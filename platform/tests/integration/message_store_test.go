package integration_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/message"
	messagepg "github.com/garamsh/garam/internal/message/postgres"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// TestMessageStore_RoundTrip drives the queue ADR-0013 rests on: a
// message is stored once per sender key, handed to one reader, and
// stays out of the queue once that reader reports it delivered. No
// agent row exists here — garam stores a message before it can route
// one, so the queue references none.
func TestMessageStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := messagepg.New(p)
	bot, elsewhere := grnOf("agent", "bot"), grnOf("agent", "elsewhere")
	ada := grnOf("user", "ada")
	// Wide enough that nothing here is refused for want of attempts;
	// what the bound does is TestMessageStore_StopsRedeliveringAnExhaustedMessage.
	const attempts = 8

	m, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	stored, err := s.Enqueue(ctx, m, nil)
	require.NoError(t, err)
	assert.Equal(t, m.ID, stored.ID)
	assert.Equal(t, "key-1", stored.IdempotencyKey, "the key the sender suppressed duplicates with comes back")

	retried, err := message.New(bot, ada, "key-1", "hello again")
	require.NoError(t, err)
	again, err := s.Enqueue(ctx, retried, nil)
	require.NoError(t, err)
	assert.Equal(t, m.ID, again.ID, "a repeated sender key returns the stored message")
	assert.Equal(t, "hello", again.Body, "and stores nothing new")

	// The queue is a table, not a connection: a store sharing no state
	// with the one that enqueued claims the message, and dropping every
	// pooled connection afterwards leaves the claim standing.
	reader := messagepg.New(p)
	claim, err := reader.Claim(ctx, bot, time.Minute, attempts, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, m.ID, claim.Message.ID)
	assert.Equal(t, "hello", claim.Message.Body)
	assert.Equal(t, ada, claim.Message.Sender)
	assert.Equal(t, 1, claim.Message.Attempts, "handing the message out is the first attempt")
	p.Reset()

	_, err = s.Claim(ctx, bot, time.Minute, attempts, nil, nil)
	assert.ErrorIs(t, err, message.ErrNoMessageReady, "a held message is nobody else's")
	_, err = s.Claim(ctx, elsewhere, time.Minute, attempts, nil, nil)
	assert.ErrorIs(t, err, message.ErrNoMessageReady, "and belongs to the recipient it is addressed to")

	// The claim is a fence rather than a secret: another recipient's
	// queue does not complete under it even where its token is known.
	assert.ErrorIs(t, s.Complete(ctx, elsewhere, claim.ID, nil), message.ErrStaleClaim)

	require.NoError(t, reader.Complete(ctx, bot, claim.ID, nil))
	// A reader retries what it could not confirm, so completing twice
	// reports the same delivery rather than failing.
	require.NoError(t, reader.Complete(ctx, bot, claim.ID, nil))
	_, err = s.Claim(ctx, bot, time.Minute, attempts, nil, nil)
	assert.ErrorIs(t, err, message.ErrNoMessageReady, "a delivered message is not handed out again")

	// The key outlives delivery, so a retry arriving after it neither
	// stores a message nor requeues the delivered one.
	late, err := message.New(bot, ada, "key-1", "hello again")
	require.NoError(t, err)
	afterDelivery, err := s.Enqueue(ctx, late, nil)
	require.NoError(t, err)
	assert.Equal(t, m.ID, afterDelivery.ID)
	_, err = s.Claim(ctx, bot, time.Minute, attempts, nil, nil)
	assert.ErrorIs(t, err, message.ErrNoMessageReady)
}

// TestMessageStore_ClaimIsExclusive puts several readers on one
// recipient's queue at once: every message is handed out, and none is
// handed to two readers.
func TestMessageStore_ClaimIsExclusive(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	const queued, readers = 8, 8
	writer := messagepg.New(p)
	for i := range queued {
		m, err := message.New(bot, ada, fmt.Sprintf("key-%d", i), "hello")
		require.NoError(t, err)
		_, err = writer.Enqueue(ctx, m, nil)
		require.NoError(t, err)
	}

	// Every reader waits for the last of them to be ready, so the claims
	// contend rather than run one after another.
	var (
		start   = make(chan struct{})
		wg      sync.WaitGroup
		mu      sync.Mutex
		claimed []message.ID
	)
	wg.Add(readers)
	for range readers {
		go func() {
			defer wg.Done()
			s := messagepg.New(p)
			<-start
			for {
				// The attempt bound is wide enough to hand every message
				// to every reader, so what keeps a message to one of them
				// is the claim and not the count.
				c, err := s.Claim(ctx, bot, time.Minute, readers, nil, nil)
				if errors.Is(err, message.ErrNoMessageReady) {
					return
				}
				if !assert.NoError(t, err) {
					return
				}
				mu.Lock()
				claimed = append(claimed, c.Message.ID)
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.Len(t, claimed, queued, "every queued message was handed out")
	held := make(map[message.ID]struct{}, len(claimed))
	for _, id := range claimed {
		held[id] = struct{}{}
	}
	assert.Len(t, held, queued, "no message was handed to two readers")
}

// TestMessageStore_LapsedClaimIsHandedOn covers the reader that claims
// a message and then dies: the lease runs out, the next reader is given
// the message, and the claim it replaced completes nothing.
func TestMessageStore_LapsedClaimIsHandedOn(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := messagepg.New(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	m, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, m, nil)
	require.NoError(t, err)

	lapsing, err := s.Claim(ctx, bot, 100*time.Millisecond, 4, nil, nil)
	require.NoError(t, err)

	// What is waited for is what is asserted: the claim that succeeds,
	// not the lease slept through. Only one attempt can succeed, because
	// it takes the message for a minute.
	var handedOn *message.Claim
	require.Eventually(t, func() bool {
		c, err := s.Claim(ctx, bot, time.Minute, 4, nil, nil)
		if err != nil {
			return false
		}
		handedOn = c
		return true
	}, 5*time.Second, 10*time.Millisecond, "a lapsed claim leaves the message claimable")

	require.NotNil(t, handedOn)
	assert.Equal(t, m.ID, handedOn.Message.ID)
	assert.NotEqual(t, lapsing.ID, handedOn.ID, "the message is handed on under a claim of its own")
	assert.Equal(t, 2, handedOn.Message.Attempts, "the hand-on is the second attempt")

	_, err = s.Renew(ctx, bot, lapsing.ID, time.Minute, time.Hour, nil)
	assert.ErrorIs(t, err, message.ErrStaleClaim,
		"the reader the message was taken from renews nothing")
	assert.ErrorIs(t, s.Complete(ctx, bot, lapsing.ID, nil), message.ErrStaleClaim,
		"the reader the message was taken from completes nothing")
	require.NoError(t, s.Complete(ctx, bot, handedOn.ID, nil))
}

// TestMessageStore_RenewalHoldsAClaimPastItsLeaseButNotPastTheCeiling
// covers the turn that outruns its lease. Renewing keeps the message
// out of every other reader's reach past the lease it was claimed
// under; the ceiling is measured from the moment the claim was taken
// and is not renewable away, so a holder that renews forever loses the
// claim all the same (ADR-0018) rather than leaving a message neither
// completed nor redelivered.
func TestMessageStore_RenewalHoldsAClaimPastItsLeaseButNotPastTheCeiling(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := messagepg.New(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")
	// The lease is what the claim would lapse under; the ceiling is how
	// far renewals may carry it. Both are wide enough that a stalled
	// machine renews late rather than failing.
	const (
		lease   = 2 * time.Second
		ceiling = 5 * time.Second
	)

	m, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, m, nil)
	require.NoError(t, err)

	held, err := s.Claim(ctx, bot, lease, 4, nil, nil)
	require.NoError(t, err)

	// Renew until the lease it was claimed under has run out, which is
	// the condition the assertion below turns on: what holds the message
	// from here is renewal and nothing else.
	renewedUntil := time.Now().Add(lease + 500*time.Millisecond)
	var renewed *message.Claim
	for time.Now().Before(renewedUntil) {
		renewed, err = s.Renew(ctx, bot, held.ID, lease, ceiling, nil)
		require.NoError(t, err, "a claim renewed inside its lease is still the holder's")
		time.Sleep(200 * time.Millisecond)
	}
	require.NotNil(t, renewed)
	assert.True(t, renewed.ExpiresAt.After(held.ExpiresAt), "renewal carried the claim past where it started")
	assert.Equal(t, held.ID, renewed.ID, "renewal keeps the hold rather than taking a new one")

	_, err = s.Claim(ctx, bot, time.Minute, 4, nil, nil)
	assert.ErrorIs(t, err, message.ErrNoMessageReady,
		"a renewed claim is nobody else's, past the lease it was claimed under")

	// The ceiling is the backstop: renewals keep being asked for and the
	// claim lapses on schedule anyway.
	var handedOn *message.Claim
	require.Eventually(t, func() bool {
		_, _ = s.Renew(ctx, bot, held.ID, lease, ceiling, nil)
		c, err := s.Claim(ctx, bot, time.Minute, 4, nil, nil)
		if err != nil {
			return false
		}
		handedOn = c
		return true
	}, 10*time.Second, 200*time.Millisecond, "the ceiling holds a renewal that would carry the claim forever")

	require.NotNil(t, handedOn)
	assert.Equal(t, m.ID, handedOn.Message.ID)
	_, err = s.Renew(ctx, bot, held.ID, lease, ceiling, nil)
	assert.ErrorIs(t, err, message.ErrStaleClaim, "a claim carried past its ceiling renews nothing")
}

// TestMessageStore_StopsRedeliveringAnExhaustedMessage covers the
// message that fails the same way every time. Handing it out is the
// attempt, since a turn that failed and a turn that never started are
// indistinguishable from here (ADR-0018), so a message that has been
// handed out its bounded number of times is handed out no more.
func TestMessageStore_StopsRedeliveringAnExhaustedMessage(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := messagepg.New(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")
	const attempts = 2
	const lease = 200 * time.Millisecond

	m, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, m, nil)
	require.NoError(t, err)

	first, err := s.Claim(ctx, bot, lease, attempts, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, first.Message.Attempts)

	// Nobody completes it, so the lease is what ends each attempt.
	var second *message.Claim
	require.Eventually(t, func() bool {
		c, err := s.Claim(ctx, bot, lease, attempts, nil, nil)
		if err != nil {
			return false
		}
		second = c
		return true
	}, 5*time.Second, 20*time.Millisecond, "a lapsed claim leaves the message claimable")
	require.NotNil(t, second)
	assert.Equal(t, 2, second.Message.Attempts)

	// Its attempts spent, the message is handed to nobody however long
	// anyone waits — and the second claim's lease runs out inside this
	// window, so what withholds it is the count rather than a lease.
	require.Never(t, func() bool {
		_, err := s.Claim(ctx, bot, time.Minute, attempts, nil, nil)
		return err == nil
	}, 2*time.Second, 50*time.Millisecond, "an exhausted message was handed out again")

	// The same message under a wider bound is claimable, so what stopped
	// it was the count and not anything else about the row.
	third, err := s.Claim(ctx, bot, time.Minute, attempts+1, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, m.ID, third.Message.ID)
	assert.Equal(t, 3, third.Message.Attempts)
}

// TestMessageService_ClaimWaitsForTheMessageThatArrives drives the long
// poll over the real queue: the claim is made before there is anything
// to claim, and it answers with the message a sender stores while it
// waits. What it waits on is a message becoming claimable, which is the
// condition the answer is read from — a poll returning early answers
// that nothing is ready, and this one does not.
func TestMessageService_ClaimWaitsForTheMessageThatArrives(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	store := messagepg.New(p)
	cfg := briefDelivery()
	s, err := message.NewService(store, cfg)
	require.NoError(t, err)
	// The claim is an agent's write, so the agent exists and was
	// activated: the execution fence admits it under that activation
	// (ADR-0080).
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	agents := agentpg.New(p)
	bot, ada := seedAgent(t, agents, orgRootGRN("acme")), grnOf("user", "ada")
	controller := scopedGRN("acme", agent.ServiceTypeOperator, "controller")
	_, err = agents.Assign(ctx, bot, controller)
	require.NoError(t, err)
	activated, _ := activatedFence(t, p, bot, controller, "r1", 1, nil)
	fence := message.Fence{Agent: bot, Serial: activated.Credential.Serial,
		NotBefore: activated.Credential.NotBefore, NotAfter: activated.Credential.NotAfter,
		TokenHash: activated.TokenBytes()}

	type answer struct {
		claim *message.Claim
		err   error
	}
	answered := make(chan answer, 1)
	start := time.Now()
	go func() {
		c, _, err := s.Claim(ctx, bot, bot, &fence, unsigned{})
		answered <- answer{claim: c, err: err}
	}()

	// Long enough that a poll answering an empty queue at once has
	// already answered, and short enough to leave the wait running.
	time.Sleep(10 * cfg.PollInterval)
	select {
	case got := <-answered:
		require.Fail(t, "the poll answered before there was a message", "%+v", got)
	default:
	}

	sent, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	_, err = store.Enqueue(ctx, sent, nil)
	require.NoError(t, err)

	select {
	case got := <-answered:
		require.NoError(t, got.err)
		require.NotNil(t, got.claim)
		assert.Equal(t, sent.ID, got.claim.Message.ID)
		assert.Equal(t, "hello", got.claim.Message.Body)
		assert.Less(t, time.Since(start), cfg.Wait,
			"the poll waited its whole wait out rather than answering with the message that arrived")
	case <-time.After(cfg.Wait + 5*time.Second):
		require.Fail(t, "the poll never answered")
	}
}

// TestMessageStore_CompletionDropsTheBodyAndKeepsTheKey is ADR-0034's
// first half in one round trip: completing a message drops its body and
// nothing else, so the row that stays still suppresses the sender's
// retry. The body is read from the table rather than through the store,
// because nothing reads a completed message back — a getter that
// rendered a dropped body as empty would answer the same for a body
// dropped and a body never read.
func TestMessageStore_CompletionDropsTheBodyAndKeepsTheKey(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := messagepg.New(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	m, err := message.New(bot, ada, "key-1", "hello")
	require.NoError(t, err)
	_, err = s.Enqueue(ctx, m, nil)
	require.NoError(t, err)

	claim, err := s.Claim(ctx, bot, time.Minute, 4, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "hello", claim.Message.Body, "the body is there to be dropped")
	require.NoError(t, s.Complete(ctx, bot, claim.ID, nil))

	assert.Equal(t, 1, countRows(t, p,
		"SELECT count(*) FROM messages WHERE id = $1 AND body IS NULL", string(m.ID)),
		"the row stays and its body is gone")

	// The key outlives the body, so a retry after delivery neither
	// stores a message nor requeues the delivered one.
	late, err := message.New(bot, ada, "key-1", "hello again")
	require.NoError(t, err)
	afterDelivery, err := s.Enqueue(ctx, late, nil)
	require.NoError(t, err)
	assert.Equal(t, m.ID, afterDelivery.ID, "a retry after completion is still suppressed")
	_, err = s.Claim(ctx, bot, time.Minute, 4, nil, nil)
	assert.ErrorIs(t, err, message.ErrNoMessageReady)
}

// TestMessageStore_EnqueueSweepsWhatOutlivedTheWindow is ADR-0034's
// second half: storing a message is the queue's one write, so the sweep
// rides it and deletes an undelivered message older than the retention
// window whole. Two controls stand beside the expired one, written and
// aged before the sweeping call: a message still inside the window,
// which a sweep reading age alone would take, and a completed one
// equally old, which a sweep reading delivery alone would take.
func TestMessageStore_EnqueueSweepsWhatOutlivedTheWindow(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := messagepg.New(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	post := func(key string) message.ID {
		m, err := message.New(bot, ada, key, "hello")
		require.NoError(t, err)
		stored, err := s.Enqueue(ctx, m, nil)
		require.NoError(t, err)
		return stored.ID
	}
	// age backdates the row, because the window is thirty days and the
	// test is not.
	age := func(id message.ID, by time.Duration) {
		_, err := p.Exec(ctx,
			"UPDATE messages SET created_at = now() - make_interval(secs => $1) WHERE id = $2",
			by.Seconds(), string(id))
		require.NoError(t, err)
	}
	present := func(id message.ID) int {
		return countRows(t, p, "SELECT count(*) FROM messages WHERE id = $1", string(id))
	}

	// The delivered one is posted first so the claim below takes it: a
	// claim hands out the oldest message on the queue.
	delivered := post("key-delivered")
	claim, err := s.Claim(ctx, bot, time.Minute, 4, nil, nil)
	require.NoError(t, err)
	require.Equal(t, delivered, claim.Message.ID, "the oldest message is the one claimed")
	require.NoError(t, s.Complete(ctx, bot, claim.ID, nil))

	expired := post("key-expired")
	standing := post("key-standing")

	age(expired, message.RetentionWindow+time.Hour)
	age(delivered, message.RetentionWindow+time.Hour)
	age(standing, message.RetentionWindow-time.Hour)
	// The aged row survives its own age: nothing has swept since it
	// crossed the window, so what removes it below is the sweep and not
	// the ageing.
	require.Equal(t, 1, present(expired))

	fresh := post("key-fresh")

	assert.Equal(t, 0, present(expired), "an undelivered message past the window was swept")
	assert.Equal(t, 1, present(standing), "a message inside the window outlives the sweep")
	assert.Equal(t, 1, present(delivered), "a completed message is kept, age or not")
	assert.Equal(t, 1, present(fresh))
}

// TestMessageStore_RetentionRunsOnTheDatabaseClock holds that the
// retention window starts on the clock the sweep ends it with (#1181).
// Each message is built a lag before it is stored, which is how a
// process clock running that far behind the database's would stamp it,
// so a stamp taken at building falls before the database's reading
// ahead of storing. Ageing then moves each stamp, not a fresh one, an
// hour either side of the window, so one sweep keeps the first message
// and deletes the second however long the round trips take.
func TestMessageStore_RetentionRunsOnTheDatabaseClock(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := messagepg.New(p)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")
	const lag, margin = time.Second, time.Hour

	dbNow := func() time.Time {
		var now time.Time
		require.NoError(t, p.QueryRow(ctx, "SELECT now()").Scan(&now))
		return now
	}
	build := func(key string) *message.Message {
		m, err := message.New(bot, ada, key, "hello")
		require.NoError(t, err)
		return m
	}
	// age moves the row's stamp back by d, which is the database's clock
	// moving d forward over it.
	age := func(id message.ID, d time.Duration) {
		_, err := p.Exec(ctx,
			"UPDATE messages SET created_at = created_at - make_interval(secs => $1) WHERE id = $2",
			d.Seconds(), string(id))
		require.NoError(t, err)
	}
	present := func(id message.ID) int {
		return countRows(t, p, "SELECT count(*) FROM messages WHERE id = $1", string(id))
	}

	inside, past := build("key-inside"), build("key-past")
	// The sleep is the clock disagreement under test, not a wait.
	time.Sleep(lag)
	before := dbNow()
	for _, m := range []*message.Message{inside, past} {
		stored, err := s.Enqueue(ctx, m, nil)
		require.NoError(t, err)
		assert.WithinRange(t, stored.CreatedAt, before, dbNow(),
			"the stamp is the database's at storing, not the process's at building")
	}

	age(inside.ID, message.RetentionWindow-margin)
	age(past.ID, message.RetentionWindow+margin)
	_, err := s.Enqueue(ctx, build("key-sweep"), nil)
	require.NoError(t, err)

	assert.Equal(t, 1, present(inside.ID), "a message inside the window on the database's clock outlives the sweep")
	assert.Equal(t, 0, present(past.ID), "a message past it is swept")
}

// TestMessage_BodyAndCompletionAreOneState covers the schema's copy of
// the lifecycle (ADR-0034): a body lives exactly while the message is
// undelivered, so the two half-states — an undelivered row without a
// body, and a completed one still holding it — are unrepresentable.
// internal/message never writes either, so only a direct statement
// reaches the constraint.
func TestMessage_BodyAndCompletionAreOneState(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	bot, ada := grnOf("agent", "bot"), grnOf("user", "ada")

	rejected := []struct {
		name string
		stmt string
	}{
		{
			name: "an undelivered message without a body",
			stmt: `INSERT INTO messages (id, recipient_grn, sender_grn, idempotency_key, body, created_at)
				VALUES ('m-nobody', $1, $2, 'key-nobody', NULL, now())`,
		},
		{
			name: "a completed message still holding its body",
			stmt: `INSERT INTO messages (id, recipient_grn, sender_grn, idempotency_key, body, created_at, claim_id, claim_expires_at, claimed_at, completed_at)
				VALUES ('m-kept', $1, $2, 'key-kept', 'hello', now(), 'c-1', now(), now(), now())`,
		},
	}
	for _, tt := range rejected {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Exec(ctx, tt.stmt, bot.String(), ada.String())
			assert.Error(t, err)
		})
	}

	// An undelivered message carrying its body is admitted, so the
	// constraint rejects a half-state rather than every row.
	m, err := message.New(bot, ada, "key-whole", "hello")
	require.NoError(t, err)
	_, err = messagepg.New(p).Enqueue(ctx, m, nil)
	require.NoError(t, err)
}
