package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/org"
)

// outbox is a message store with what the tests below read it by: the
// database's clock, and the rows a key holds.
type outbox struct {
	p *pgxpool.Pool
	s *message.PostgresStore
}

func newOutbox(t *testing.T, p *pgxpool.Pool) outbox {
	t.Helper()
	return outbox{p: p, s: message.NewPostgresStore(p)}
}

// clock reads the database's clock, the one the budget is judged on.
func (o outbox) clock(t *testing.T) time.Time {
	t.Helper()
	var now time.Time
	require.NoError(t, o.p.QueryRow(context.Background(), "SELECT clock_timestamp()").Scan(&now))
	return now
}

// submit posts one entry of sender's under key, created at created.
func (o outbox) submit(t *testing.T, sender, recipient grn.GRN, key, body string, created time.Time, fence *message.Fence) (*message.Message, error) {
	t.Helper()
	m, err := message.New(recipient, sender, key, body)
	require.NoError(t, err)
	return o.s.EnqueueWithinBudget(context.Background(), m, created, fence)
}

// stored counts the rows sender's key holds.
func (o outbox) stored(t *testing.T, sender grn.GRN, key string) int {
	t.Helper()
	return countRows(t, o.p, "SELECT count(*) FROM messages WHERE sender_grn = $1 AND idempotency_key = $2", sender.String(), key)
}

// age moves a row's receipt back by d, which is the database's clock
// moving d forward over it; the entry's own creation instant moves
// back with it in the caller.
func (o outbox) age(t *testing.T, id message.ID, d time.Duration) {
	t.Helper()
	_, err := o.p.Exec(context.Background(),
		"UPDATE messages SET created_at = created_at - make_interval(secs => $1) WHERE id = $2", d.Seconds(), string(id))
	require.NoError(t, err)
}

// TestOutbox_AnEntryInsideItsBudgetIsStoredOnceAndEveryRetryIsAnsweredWithIt
// covers the first send and a retry after a lost answer (ADR-0083).
// The receipt is the database's decision instant and not the entry's
// creation, and a retry of the same entry is answered with the message
// the first stored. The retry is the control for the two refusals
// beside it: the same key carrying another recipient or another body
// conflicts, so what answers the retry is the comparison rather than a
// key that answers whatever it is sent.
func TestOutbox_AnEntryInsideItsBudgetIsStoredOnceAndEveryRetryIsAnsweredWithIt(t *testing.T) {
	o := newOutbox(t, pool(t))
	bot, ada, eve := grnOf("agent", "bot"), grnOf("agent", "ada"), grnOf("agent", "eve")

	before := o.clock(t)
	created := before.Add(-time.Minute)
	first, err := o.submit(t, bot, ada, "entry-1", "hello", created, nil)
	require.NoError(t, err)
	assert.WithinRange(t, first.CreatedAt(), before, o.clock(t), "the receipt is the decision instant, not createdAt")
	assert.Equal(t, bot, first.Sender())
	assert.Equal(t, ada, first.Recipient())
	assert.Equal(t, "entry-1", first.IdempotencyKey())

	retried, err := o.submit(t, bot, ada, "entry-1", "hello", created, nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID(), retried.ID(), "the retry is answered with the message the first attempt stored")
	assert.True(t, first.CreatedAt().Equal(retried.CreatedAt()))

	_, err = o.submit(t, bot, eve, "entry-1", "hello", created, nil)
	assert.ErrorIs(t, err, message.ErrKeyHoldsAnotherMessage, "another recipient under the key")
	_, err = o.submit(t, bot, ada, "entry-1", "hello again", created, nil)
	assert.ErrorIs(t, err, message.ErrKeyHoldsAnotherMessage, "another body under the key")
	assert.Equal(t, 1, o.stored(t, bot, "entry-1"))
}

// TestOutbox_TheBudgetIsJudgedOnTheDatabaseClockWhicheverWayTheHostIsSkewed
// stands in for a producer host whose clock disagrees with the
// database's (ADR-0083). Ahead, its entry is refused as later than
// garam's clock and never receipted at the instant it names; behind,
// it is stored while inside the budget, receipted on the database's
// clock so retention runs from there, and refused once past it. The
// entry inside the budget is the control for both refusals.
func TestOutbox_TheBudgetIsJudgedOnTheDatabaseClockWhicheverWayTheHostIsSkewed(t *testing.T) {
	o := newOutbox(t, pool(t))
	bot, ada := grnOf("agent", "bot"), grnOf("agent", "ada")

	now := o.clock(t)
	_, err := o.submit(t, bot, ada, "ahead", "hello", now.Add(time.Minute), nil)
	assert.ErrorIs(t, err, message.ErrCreatedLater)

	inside, err := o.submit(t, bot, ada, "behind-inside", "hello", now.Add(-message.SubmissionBudget+time.Minute), nil)
	require.NoError(t, err, "control: a host behind by less than the budget is stored")
	assert.False(t, inside.CreatedAt().Before(now), "the receipt is the database's clock, not the host's")

	_, err = o.submit(t, bot, ada, "behind-past", "hello", now.Add(-message.SubmissionBudget-time.Second), nil)
	assert.ErrorIs(t, err, message.ErrSubmissionExpired)

	assert.Equal(t, 0, o.stored(t, bot, "ahead"))
	assert.Equal(t, 1, o.stored(t, bot, "behind-inside"))
	assert.Equal(t, 0, o.stored(t, bot, "behind-past"))
}

// TestOutbox_AStoredResultIsAnsweredAfterTheBudget covers a sender
// whose answer was lost and whose entry is now past its budget
// (ADR-0083): the message its key stores is answered, waiting or
// completed with its body dropped (ADR-0034). A cleared body cannot be
// compared, so a retry bound to the completed message by key and
// recipient is answered whatever body it carries, and another
// recipient still conflicts. Another key created at the same instant
// is the control: it stores nothing, so the answers come from the key
// and not from a budget that was not applied.
func TestOutbox_AStoredResultIsAnsweredAfterTheBudget(t *testing.T) {
	ctx := context.Background()
	o := newOutbox(t, pool(t))
	bot, ada, eve := grnOf("agent", "bot"), grnOf("agent", "ada"), grnOf("agent", "eve")
	const elapsed = message.SubmissionBudget + time.Hour

	created := o.clock(t).Add(-time.Minute)
	delivered, err := o.submit(t, bot, ada, "delivered", "hello", created, nil)
	require.NoError(t, err)
	claim, err := o.s.Claim(ctx, ada, time.Minute, 3, nil)
	require.NoError(t, err)
	require.Equal(t, delivered.ID(), claim.Message().ID())
	require.NoError(t, o.s.Complete(ctx, ada, claim.ID(), nil))
	waiting, err := o.submit(t, bot, ada, "waiting", "hello", created, nil)
	require.NoError(t, err)

	o.age(t, waiting.ID(), elapsed)
	o.age(t, delivered.ID(), elapsed)
	created = created.Add(-elapsed)

	_, err = o.submit(t, bot, ada, "never-stored", "hello", created, nil)
	require.ErrorIs(t, err, message.ErrSubmissionExpired, "control: the budget has run out for a key that stores nothing")

	answered, err := o.submit(t, bot, ada, "waiting", "hello", created, nil)
	require.NoError(t, err)
	assert.Equal(t, waiting.ID(), answered.ID())
	assert.Equal(t, "hello", answered.Body())

	answered, err = o.submit(t, bot, ada, "delivered", "hello", created, nil)
	require.NoError(t, err)
	assert.Equal(t, delivered.ID(), answered.ID())
	assert.Empty(t, answered.Body(), "a completed message has dropped its body")
	answered, err = o.submit(t, bot, ada, "delivered", "a body the completed message no longer holds", created, nil)
	require.NoError(t, err, "a cleared body binds the retry by key and recipient")
	assert.Equal(t, delivered.ID(), answered.ID())
	_, err = o.submit(t, bot, eve, "delivered", "hello", created, nil)
	assert.ErrorIs(t, err, message.ErrKeyHoldsAnotherMessage)

	assert.Equal(t, 0, o.stored(t, bot, "never-stored"))
}

// TestOutbox_AKeyTheSweepEmptiedStoresNothingAgain carries an entry
// whose first message was stored and never delivered past the
// thirty-day retention (ADR-0034): the retry's own sweep deletes it,
// and the budget, long run out, stores nothing in its place — which
// is what the budget sitting inside the retention buys (ADR-0083). A
// fresh entry stored by a retry in the same state is the control.
func TestOutbox_AKeyTheSweepEmptiedStoresNothingAgain(t *testing.T) {
	o := newOutbox(t, pool(t))
	bot, ada := grnOf("agent", "bot"), grnOf("agent", "ada")
	const elapsed = message.RetentionWindow + time.Hour

	created := o.clock(t).Add(-time.Minute)
	first, err := o.submit(t, bot, ada, "entry-1", "hello", created, nil)
	require.NoError(t, err)
	o.age(t, first.ID(), elapsed)
	created = created.Add(-elapsed)
	require.Equal(t, 1, o.stored(t, bot, "entry-1"))

	_, err = o.submit(t, bot, ada, "entry-1", "hello", created, nil)
	assert.ErrorIs(t, err, message.ErrSubmissionExpired)
	assert.Equal(t, 0, o.stored(t, bot, "entry-1"), "the sweep removed the message and nothing replaced it")

	_, err = o.submit(t, bot, ada, "entry-2", "hello", o.clock(t).Add(-time.Minute), nil)
	require.NoError(t, err, "control: an entry inside its budget is stored after the sweep")
	assert.Equal(t, 1, o.stored(t, bot, "entry-2"))
}

// TestOutbox_ARequestDelayedAtTheWriteBoundaryIsJudgedWhenItWrites
// holds the agent's execution row, which a fenced write waits on
// first (ADR-0080), while the entry's budget runs out. The write is
// judged when it inserts and not when its transaction began, so it is
// refused; the same wait for an entry with time to spare is stored
// and receipted after the wait (ADR-0083).
func TestOutbox_ARequestDelayedAtTheWriteBoundaryIsJudgedWhenItWrites(t *testing.T) {
	ctx := context.Background()
	f := newFence(t)
	_, token := f.activate(t, "r1", generation(1), nil, valid("1"))
	c := valid("1")
	hash := token.Hash()
	fence := &message.Fence{Agent: f.agent, Serial: c.Serial, NotBefore: c.NotBefore, NotAfter: c.NotAfter, TokenHash: hash[:]}
	o := newOutbox(t, f.p)
	ada := scopedGRN("acme", "agent", "ada")
	const delay = 900 * time.Millisecond

	for _, tc := range []struct {
		name string
		// left is how much budget the entry has when the write starts.
		left time.Duration
		want error
	}{
		{name: "the budget runs out while it waits", left: delay / 3, want: message.ErrSubmissionExpired},
		{name: "control: budget to spare after the same wait", left: time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hold, err := f.p.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = hold.Rollback(ctx) }()
			_, err = hold.Exec(ctx, `SELECT 1 FROM agent_executions WHERE agent_grn = $1 FOR UPDATE`, f.agent.String())
			require.NoError(t, err)

			start := o.clock(t)
			created := start.Add(tc.left - message.SubmissionBudget)
			type answer struct {
				m   *message.Message
				err error
			}
			done := make(chan answer, 1)
			go func() {
				m, err := message.New(ada, f.agent, "entry-"+tc.name, "hello")
				if err == nil {
					m, err = o.s.EnqueueWithinBudget(ctx, m, created, fence)
				}
				done <- answer{m, err}
			}()
			awaitLockWaiter(t, f.p)
			released := start.Add(delay)
			require.Eventually(t, func() bool { return !o.clock(t).Before(released) }, 5*time.Second, 10*time.Millisecond)
			require.NoError(t, hold.Rollback(ctx))

			got := <-done
			if tc.want != nil {
				assert.ErrorIs(t, got.err, tc.want)
				assert.Equal(t, 0, o.stored(t, f.agent, "entry-"+tc.name))
				return
			}
			require.NoError(t, got.err)
			assert.False(t, got.m.CreatedAt().Before(released), "the receipt is the instant it wrote, after the wait")
		})
	}
}

// outboxPost sends body to the durable submission route as client,
// under version, or under no version where it is empty.
func outboxPost(t *testing.T, client *http.Client, url, version, body string) (int, string, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/outbox-messages", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if version != "" {
		req.Header.Set("Garam-Contract-Version", version)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get("Garam-Contract-Version"), answer
}

// entryTo is the body a sender posts for one outbox entry.
func entryTo(recipient grn.GRN, key, createdAt string) string {
	return `{"recipient":"` + recipient.String() + `","idempotencyKey":"` + key +
		`","body":"hello","createdAt":"` + createdAt + `"}`
}

// TestOutbox_TheRouteRefusesWithoutItsContractOrCreationInstantBeforeAnyWrite
// drives the listener an agent posts on. A queue holding a message
// past its retention is the witness: the sweep rides every write, so
// a refusal that left it standing and stored nothing performed no
// mutation at all. The entry under the contract with its creation
// instant is the control, and its write sweeps that message (ADR-0083).
// Its answer is the message shape a sender verifies; a retry is
// answered with the same message, and a post across the boundary no
// consent carries is answered 201 with the refusal, whose sender and
// recipient are the swap of the submission's.
func TestOutbox_TheRouteRefusesWithoutItsContractOrCreationInstantBeforeAnyWrite(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	operator := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	senderGRN, sender := provisioned(t, url, operator)
	recipientGRN, _ := provisioned(t, url, operator)
	o := newOutbox(t, p)

	witness, err := message.New(recipientGRN, grnOf("agent", "witness"), "witness", "old")
	require.NoError(t, err)
	witness, err = o.s.Enqueue(ctx, witness, nil)
	require.NoError(t, err)
	o.age(t, witness.ID(), message.RetentionWindow+time.Hour)
	witnessed := func() int {
		return countRows(t, p, "SELECT count(*) FROM messages WHERE id = $1", string(witness.ID()))
	}

	created := o.clock(t).Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	const v1 = message.DeliveryContractVersion
	for _, tc := range []struct{ name, version, body string }{
		{name: "no contract version", body: entryTo(recipientGRN, "entry-1", created)},
		{name: "a version this route does not serve", version: "durable-delivery.v2", body: entryTo(recipientGRN, "entry-1", created)},
		{name: "no creation instant", version: v1, body: `{"recipient":"` + recipientGRN.String() + `","idempotencyKey":"entry-1","body":"hello"}`},
		{name: "a creation instant that does not parse", version: v1, body: entryTo(recipientGRN, "entry-1", "yesterday")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, version, body := outboxPost(t, sender, url, tc.version, tc.body)
			assert.Equal(t, http.StatusBadRequest, status, string(body))
			assert.Equal(t, v1, version)
			assert.Contains(t, string(body), `"kind":"invalid_argument"`)
			assert.Equal(t, 1, witnessed(), "the refusal swept the queue")
			assert.Equal(t, 0, o.stored(t, senderGRN, "entry-1"), "the refusal stored the entry")
		})
	}

	status, version, body := outboxPost(t, sender, url, v1, entryTo(recipientGRN, "entry-1", created))
	require.Equal(t, http.StatusCreated, status, string(body))
	assert.Equal(t, v1, version)
	var stored machinegen.Message
	require.NoError(t, json.Unmarshal(body, &stored))
	assert.NotEmpty(t, stored.Id)
	assert.Equal(t, senderGRN.String(), stored.Sender)
	assert.Equal(t, recipientGRN.String(), stored.Recipient)
	assert.Equal(t, "entry-1", stored.IdempotencyKey)
	assert.Equal(t, 0, witnessed(), "control: the admitted write swept the queue")

	status, _, body = outboxPost(t, sender, url, v1, entryTo(recipientGRN, "entry-1", created))
	require.Equal(t, http.StatusCreated, status, string(body))
	var replayed machinegen.Message
	require.NoError(t, json.Unmarshal(body, &replayed))
	assert.Equal(t, stored.Id, replayed.Id, "a retry is answered with the stored message")

	foreign := scopedGRN("globex", agent.ServiceType, "0a1b2c3d4e5f6071")
	status, _, body = outboxPost(t, sender, url, v1, entryTo(foreign, "entry-2", created))
	require.Equal(t, http.StatusCreated, status, string(body))
	var refusal machinegen.Message
	require.NoError(t, json.Unmarshal(body, &refusal))
	assert.Equal(t, foreign.String(), refusal.Sender, "the refusal is sent under the recipient that could not be reached")
	assert.Equal(t, senderGRN.String(), refusal.Recipient)
	assert.NotEqual(t, "entry-2", refusal.IdempotencyKey)
}
