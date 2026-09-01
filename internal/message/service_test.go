package message_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
)

// queue is a message.Store recording what the service asked of it. It
// hands out no claim: a claim is built by the store that read the row
// it holds, so what the service does with a message it was handed is
// the round trip in tests/e2e, against the Postgres that decides
// exclusivity, leases and the attempt bound.
type queue struct {
	mu           sync.Mutex
	stored       []*message.Message
	claims       int
	renewals     int
	completions  int
	statuses     int
	lease        time.Duration
	ceiling      time.Duration
	maxAttempts  int
	statusSender grn.GRN
	statusID     message.ID
}

func (q *queue) Enqueue(_ context.Context, m *message.Message) (*message.Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stored = append(q.stored, m)
	return m, nil
}

func (q *queue) Claim(_ context.Context, _ grn.GRN, lease time.Duration, maxAttempts int) (*message.Claim, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claims++
	q.lease, q.maxAttempts = lease, maxAttempts
	return nil, message.ErrNoMessageReady
}

func (q *queue) Renew(_ context.Context, _ grn.GRN, _ message.ClaimID, lease, ceiling time.Duration) (*message.Claim, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.renewals++
	q.lease, q.ceiling = lease, ceiling
	return nil, message.ErrStaleClaim
}

func (q *queue) Complete(_ context.Context, _ grn.GRN, _ message.ClaimID) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completions++
	return nil
}

func (q *queue) Status(_ context.Context, sender grn.GRN, id message.ID, maxAttempts int) (*message.Status, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.statuses++
	q.statusSender, q.statusID, q.maxAttempts = sender, id, maxAttempts
	return message.NewStatus(id, message.StatePending, 0, time.Now().UTC()), nil
}

// reads is how many times the service went to the queue, by any method.
func (q *queue) reads() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.claims + q.renewals + q.completions + q.statuses
}

// brief is a configuration a test can wait through. The delivery
// parameters are unchanged; only how long a poll holds is.
func brief() message.Config {
	return message.Config{Wait: 200 * time.Millisecond, PollInterval: 10 * time.Millisecond}
}

func TestNewService_RefusesAConfigurationThatDeliversNothing(t *testing.T) {
	// The default configuration is the control: what follows is refused
	// for the value it names rather than because nothing is accepted.
	_, err := message.NewService(&queue{}, message.Config{})
	require.NoError(t, err)

	tests := []struct {
		name string
		cfg  message.Config
	}{
		{name: "a lease that has already run out", cfg: message.Config{Lease: -time.Second}},
		{
			name: "a ceiling under the lease it bounds, which a renewal would shorten a claim to",
			cfg:  message.Config{Lease: time.Minute, LeaseCeiling: time.Second},
		},
		{name: "an attempt bound that hands out nothing", cfg: message.Config{Attempts: -1}},
		{
			name: "an attempt bound past what this package will hand out",
			cfg:  message.Config{Attempts: message.MaxAttempts + 1},
		},
		{name: "a poll that never re-reads the queue", cfg: message.Config{PollInterval: -time.Second}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := message.NewService(&queue{}, tt.cfg)
			assert.Nil(t, s)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

// TestService_ActsOnlyOnTheQueueTheCallerIsAddressedBy is the whole of
// the authorization on this domain. Each refusal sits beside the same
// call made by the agent the message is addressed to, so a refusal that
// held for every caller would fail here: what discriminates is the one
// comparison, and the queue is not reached at all when it refuses.
func TestService_ActsOnlyOnTheQueueTheCallerIsAddressedBy(t *testing.T) {
	ctx := context.Background()
	bot, stranger := recipientGRN(), grn.New("acme", grn.DefaultProject, "agent", "other")
	// A GRN carries its organization, so an agent of another
	// organization is refused by the same comparison.
	foreign := grn.New("globex", grn.DefaultProject, "agent", "bot")

	tests := []struct {
		name string
		// act calls one method as caller, on bot's queue.
		act func(s message.Service, caller grn.GRN) error
	}{
		{
			name: "claim",
			act: func(s message.Service, caller grn.GRN) error {
				_, err := s.Claim(ctx, caller, bot)
				return err
			},
		},
		{
			name: "renew",
			act: func(s message.Service, caller grn.GRN) error {
				_, err := s.Renew(ctx, caller, bot, "claim-1")
				return err
			},
		},
		{
			name: "complete",
			act: func(s message.Service, caller grn.GRN) error {
				return s.Complete(ctx, caller, bot, "claim-1")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &queue{}
			s, err := message.NewService(store, brief())
			require.NoError(t, err)

			for _, caller := range []grn.GRN{stranger, foreign} {
				assert.ErrorIs(t, tt.act(s, caller), message.ErrNotRecipient)
			}
			assert.Zero(t, store.reads(), "a refused caller reached the queue")

			// The recipient's own call is answered by the queue, so the
			// refusal above is the comparison rather than a method that
			// refuses everyone.
			assert.NotErrorIs(t, tt.act(s, bot), message.ErrNotRecipient)
			assert.NotZero(t, store.reads(), "the recipient's own call never reached the queue")
		})
	}
}

// TestService_ClaimWaitsForAMessageRatherThanAnsweringAnEmptyQueue
// pins the long poll: the call holds for the wait it was configured
// with, and it re-reads the queue while it holds. A message becomes
// ready when a lease runs out as well as when a sender stores one, and
// nothing announces a lease running out, so re-reading is the condition
// that catches both.
func TestService_ClaimWaitsForAMessageRatherThanAnsweringAnEmptyQueue(t *testing.T) {
	cfg := brief()
	store := &queue{}
	s, err := message.NewService(store, cfg)
	require.NoError(t, err)

	start := time.Now()
	claim, err := s.Claim(context.Background(), recipientGRN(), recipientGRN())
	waited := time.Since(start)

	assert.Nil(t, claim)
	assert.ErrorIs(t, err, message.ErrNoMessageReady)
	assert.GreaterOrEqual(t, waited, cfg.Wait, "the poll answered an empty queue before its wait ran out")
	assert.Greater(t, store.claims, 1, "the poll slept through its wait instead of re-reading the queue")
	assert.Equal(t, message.DefaultLease, store.lease, "the lease Postgres measures is the configured one")
	assert.Equal(t, message.DefaultAttempts, store.maxAttempts)
}

// TestService_ClaimStopsWaitingWhenTheCallerDoes covers the poll whose
// caller hung up: there is nobody to hand a message to, so it answers
// rather than holding a connection nobody reads.
func TestService_ClaimStopsWaitingWhenTheCallerDoes(t *testing.T) {
	cfg := brief()
	cfg.Wait = 10 * time.Second
	s, err := message.NewService(&queue{}, cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err = s.Claim(ctx, recipientGRN(), recipientGRN())
	assert.ErrorIs(t, err, message.ErrNoMessageReady)
	assert.Less(t, time.Since(start), cfg.Wait, "the poll held its wait out after the caller had gone")
}

// TestService_PostsToARecipientNothingKnowsAbout keeps ADR-0013's rule
// at the service: the recipient is an address rather than a row that
// has to exist, so a message to an agent garam has never heard of is
// stored like any other. What is refused is a message nothing could be
// done with at all.
func TestService_PostsToARecipientNothingKnowsAbout(t *testing.T) {
	ctx := context.Background()
	store := &queue{}
	s, err := message.NewService(store, brief())
	require.NoError(t, err)

	unknown := grn.New("acme", grn.DefaultProject, "agent", "nobodyhaseverseenthisone")
	stored, err := s.Post(ctx, senderGRN(), unknown, "key-1", "hello")
	require.NoError(t, err)
	assert.Equal(t, unknown, stored.Recipient())
	assert.Equal(t, senderGRN(), stored.Sender(), "the sender is the caller rather than anything the body said")
	assert.Equal(t, "hello", stored.Body())
	assert.Zero(t, stored.Attempts(), "a stored message has not been handed out")
	assert.Len(t, store.stored, 1)

	_, err = s.Post(ctx, senderGRN(), grn.GRN{}, "key-1", "hello")
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "a message addressed to nothing")
	_, err = s.Post(ctx, senderGRN(), unknown, "", "hello")
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "a message with no key to suppress a duplicate by")
	assert.Len(t, store.stored, 1, "a refused message reached the queue")
}

// TestService_RefusesABodyPastTheBoundBeforeStoringIt holds that the
// submission bound is enforced at the boundary, ahead of the queue: a
// body past it is refused and nothing reaches the store. The body at
// the bound is the control — it is stored, so the refusal is the bound
// rather than a Post that refuses every body.
func TestService_RefusesABodyPastTheBoundBeforeStoringIt(t *testing.T) {
	ctx := context.Background()
	store := &queue{}
	s, err := message.NewService(store, brief())
	require.NoError(t, err)

	past := strings.Repeat("b", message.MaxBodyLength+1)
	_, err = s.Post(ctx, senderGRN(), recipientGRN(), "key-1", past)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
	assert.Empty(t, store.stored, "a refused body reached the queue")

	at := strings.Repeat("b", message.MaxBodyLength)
	stored, err := s.Post(ctx, senderGRN(), recipientGRN(), "key-2", at)
	require.NoError(t, err)
	assert.Equal(t, at, stored.Body())
	assert.Len(t, store.stored, 1, "a body at the bound is stored")
}

// TestService_StatusReadsUnderTheCallerAsTheSender is the bound on the
// read from the sender's side: the queue is asked for the caller's own
// messages, so an identifier another identity's message carries reaches
// nothing. The attempt bound travels with the read, so what a sender is
// told and what the claim predicate would do are one number rather than
// two readings of the same columns.
func TestService_StatusReadsUnderTheCallerAsTheSender(t *testing.T) {
	ctx := context.Background()
	store := &queue{}
	s, err := message.NewService(store, brief())
	require.NoError(t, err)

	status, err := s.Status(ctx, senderGRN(), "message-1")
	require.NoError(t, err)
	assert.Equal(t, message.ID("message-1"), status.ID())
	assert.Equal(t, senderGRN(), store.statusSender, "the read named a sender the caller had not proved")
	assert.Equal(t, message.ID("message-1"), store.statusID)
	assert.Equal(t, message.DefaultAttempts, store.maxAttempts,
		"the read is bounded by the attempts the claim predicate filters on")

	// A configured bound reaches the read as readily as the default one,
	// so the state and the claim cannot be decided against different
	// numbers.
	cfg := brief()
	cfg.Attempts = message.MaxAttempts
	configured := &queue{}
	s, err = message.NewService(configured, cfg)
	require.NoError(t, err)
	_, err = s.Status(ctx, senderGRN(), "message-2")
	require.NoError(t, err)
	assert.Equal(t, message.MaxAttempts, configured.maxAttempts)
}
