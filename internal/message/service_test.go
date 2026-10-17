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
// the round trip in tests/integration, against the Postgres that
// decides exclusivity, leases and the attempt bound.
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
	created      []time.Time
	refusals     [][2]grn.GRN
}

func (q *queue) Enqueue(_ context.Context, m *message.Message, _ *message.Fence) (*message.Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stored = append(q.stored, m)
	return m, nil
}

func (q *queue) EnqueueWithinBudget(_ context.Context, m *message.Message, created time.Time, _ *message.Fence) (*message.Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stored = append(q.stored, m)
	q.created = append(q.created, created)
	return m, nil
}

func (q *queue) Refuse(_ context.Context, sender, recipient grn.GRN, _ *message.Fence) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.refusals = append(q.refusals, [2]grn.GRN{sender, recipient})
	return nil
}

func (q *queue) Claim(_ context.Context, _ grn.GRN, lease time.Duration, maxAttempts int, _ *message.Fence, _ func(*message.Claim) error) (*message.Claim, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.claims++
	q.lease, q.maxAttempts = lease, maxAttempts
	return nil, message.ErrNoMessageReady
}

func (q *queue) Renew(_ context.Context, _ grn.GRN, _ message.ClaimID, lease, ceiling time.Duration, _ *message.Fence) (*message.Claim, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.renewals++
	q.lease, q.ceiling = lease, ceiling
	return nil, message.ErrStaleClaim
}

func (q *queue) Complete(_ context.Context, _ grn.GRN, _ message.ClaimID, _ *message.Fence) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completions++
	return nil
}

func (q *queue) Status(_ context.Context, sender grn.GRN, id message.ID, maxAttempts int, _ *message.Fence) (*message.Status, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.statuses++
	q.statusSender, q.statusID, q.maxAttempts = sender, id, maxAttempts
	return message.NewStatus(id, message.StatePending, 0, time.Now().UTC()), nil
}

// reads is how many times the service went to the queue, by any method.
// fenceOf is the fence an agent's own request presents. The queue
// here admits it unread; the fence itself is the store's (ADR-0080).
func fenceOf(g grn.GRN) *message.Fence { return &message.Fence{Agent: g} }

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
				_, _, err := s.Claim(ctx, caller, bot, fenceOf(bot), nil)
				return err
			},
		},
		{
			name: "renew",
			act: func(s message.Service, caller grn.GRN) error {
				_, err := s.Renew(ctx, caller, bot, "claim-1", fenceOf(bot))
				return err
			},
		},
		{
			name: "complete",
			act: func(s message.Service, caller grn.GRN) error {
				return s.Complete(ctx, caller, bot, "claim-1", fenceOf(bot))
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

	// A deadline far above the configured wait and far below the default
	// one, so a poll that ran on the default rather than on what this
	// test set is told apart from one that ran its wait out: the first
	// leaves this context expired and the second does not.
	ctx, cancel := context.WithTimeout(context.Background(), message.DefaultWait/10)
	defer cancel()

	start := time.Now()
	claim, _, err := s.Claim(ctx, recipientGRN(), recipientGRN(), fenceOf(recipientGRN()), nil)
	waited := time.Since(start)

	assert.Nil(t, claim)
	assert.ErrorIs(t, err, message.ErrNoMessageReady)
	assert.NoError(t, ctx.Err(), "the poll ran on a wait this test did not configure")
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
	_, _, err = s.Claim(ctx, recipientGRN(), recipientGRN(), fenceOf(recipientGRN()), nil)
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
	stored, err := s.Post(ctx, senderGRN(), unknown, "key-1", "hello", message.AuthorSender, nil)
	require.NoError(t, err)
	assert.Equal(t, unknown, stored.Recipient())
	assert.Equal(t, senderGRN(), stored.Sender(), "the sender is the caller rather than anything the body said")
	assert.Equal(t, "hello", stored.Body())
	assert.Zero(t, stored.Attempts(), "a stored message has not been handed out")
	assert.Len(t, store.stored, 1)

	_, err = s.Post(ctx, senderGRN(), grn.GRN{}, "key-1", "hello", message.AuthorSender, nil)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "a message addressed to nothing")
	_, err = s.Post(ctx, senderGRN(), unknown, "", "hello", message.AuthorSender, nil)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "a message with no key to suppress a duplicate by")
	assert.Len(t, store.stored, 1, "a refused message reached the queue")
}

// TestService_PostWithinBudgetLeavesTheBudgetToTheStore holds that a
// durable submission reaches the store with the creation instant it
// carries, however old, because the store judges the budget when it
// inserts (ADR-0083). A message nothing could be done with is refused
// first, beside a valid one that is stored.
func TestService_PostWithinBudgetLeavesTheBudgetToTheStore(t *testing.T) {
	ctx := context.Background()
	store := &queue{}
	s, err := message.NewService(store, brief())
	require.NoError(t, err)

	long := time.Now().Add(-2 * message.SubmissionBudget)
	stored, err := s.PostWithinBudget(ctx, senderGRN(), recipientGRN(), "entry-1", "hello", message.AuthorSender, long, nil)
	require.NoError(t, err)
	assert.Equal(t, senderGRN(), stored.Sender())
	assert.Equal(t, "entry-1", stored.IdempotencyKey())
	assert.Equal(t, []time.Time{long}, store.created, "the creation instant reached the store unjudged")

	_, err = s.PostWithinBudget(ctx, senderGRN(), recipientGRN(), "", "hello", message.AuthorSender, long, nil)
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
	_, err = s.Post(ctx, senderGRN(), recipientGRN(), "key-1", past, message.AuthorSender, nil)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
	assert.Empty(t, store.stored, "a refused body reached the queue")

	at := strings.Repeat("b", message.MaxBodyLength)
	stored, err := s.Post(ctx, senderGRN(), recipientGRN(), "key-2", at, message.AuthorSender, nil)
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

	status, err := s.Status(ctx, senderGRN(), "message-1", nil)
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
	_, err = s.Status(ctx, senderGRN(), "message-2", nil)
	require.NoError(t, err)
	assert.Equal(t, message.MaxAttempts, configured.maxAttempts)
}

// answering is a queue whose claim answers err instead of the package's
// own sentinel, so what the wait reads can be a copy carrying the kind
// rather than the sentinel itself.
type answering struct {
	*queue
	err error
}

func (a *answering) Claim(ctx context.Context, recipient grn.GRN, lease time.Duration, maxAttempts int, fence *message.Fence, handOut func(*message.Claim) error) (*message.Claim, error) {
	_, _ = a.queue.Claim(ctx, recipient, lease, maxAttempts, fence, handOut)
	return nil, a.err
}

// TestService_ClaimWaitsOnTheKindTheStoreAnswersWith holds the wait
// against the shape the store's answer arrives in. `stack-go.md` §4
// does not admit errors.Is beside errorx, and the reason is here: a
// sentinel matched by identity stops matching the moment anything
// replaces it with a kind-carrying copy, and the wait would then hand
// that copy to a caller as the failure it is not.
//
// The control is a failure of another kind, which must be handed back
// at once: without it the wait would hold as readily for a service that
// waits out every failure the store reports.
func TestService_ClaimWaitsOnTheKindTheStoreAnswersWith(t *testing.T) {
	cfg := brief()
	store := &answering{queue: &queue{}, err: errorx.New(errorx.KindNotFound, "no message ready")}
	s, err := message.NewService(store, cfg)
	require.NoError(t, err)

	claim, _, err := s.Claim(context.Background(), recipientGRN(), recipientGRN(), fenceOf(recipientGRN()), nil)

	assert.Nil(t, claim)
	assert.ErrorIs(t, err, message.ErrNoMessageReady, "an empty queue is answered as an empty queue")
	assert.Greater(t, store.queue.claims, 1, "the wait re-read the queue rather than answering the first read")

	unreachable := errorx.New(errorx.KindUnavailable, "queue unreachable")
	failing := &answering{queue: &queue{}, err: unreachable}
	s, err = message.NewService(failing, cfg)
	require.NoError(t, err)

	_, _, err = s.Claim(context.Background(), recipientGRN(), recipientGRN(), fenceOf(recipientGRN()), nil)

	assert.ErrorIs(t, err, unreachable, "the control: another kind reaches the caller")
	assert.Equal(t, 1, failing.queue.claims, "and is not waited on")
}

// TestService_RefuseRecordsOnlyASendBetweenTwoIdentities passes a
// denied send to the store as its two parties, and refuses one whose
// party is no identity before the store is reached. The valid send is
// the control: the store is reachable, so the refusal is the check.
func TestService_RefuseRecordsOnlyASendBetweenTwoIdentities(t *testing.T) {
	q := &queue{}
	s, err := message.NewService(q, message.Config{})
	require.NoError(t, err)

	require.NoError(t, s.Refuse(context.Background(), senderGRN(), recipientGRN(), fenceOf(senderGRN())))
	assert.Equal(t, [][2]grn.GRN{{senderGRN(), recipientGRN()}}, q.refusals)

	err = s.Refuse(context.Background(), senderGRN(), grn.GRN{}, nil)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%v", err)
	assert.Len(t, q.refusals, 1, "a send to no identity reached the store")
}
