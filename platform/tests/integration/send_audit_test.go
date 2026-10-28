package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/wire"
)

// sendEvent is one row of the send record as a test reads it back.
type sendEvent struct {
	sender, recipient, decision string
	messageID                   *string
	messageCreatedAt            *time.Time
	recordedAt                  time.Time
}

// sendEvents reads every send event sender wrote, oldest first.
func sendEvents(t *testing.T, p *pgxpool.Pool, sender grn.GRN) []sendEvent {
	t.Helper()
	rows, err := p.Query(context.Background(), `SELECT sender_grn, recipient_grn, decision,
		message_id, message_created_at, recorded_at
		FROM audit_send_events WHERE sender_grn = $1 ORDER BY id`, sender.String())
	require.NoError(t, err)
	defer rows.Close()
	var events []sendEvent
	for rows.Next() {
		var e sendEvent
		require.NoError(t, rows.Scan(&e.sender, &e.recipient, &e.decision,
			&e.messageID, &e.messageCreatedAt, &e.recordedAt))
		events = append(events, e)
	}
	require.NoError(t, rows.Err())
	return events
}

// sendStack is a machine listener over real Postgres and the real
// policy engine, with acme and globex created and no consent between
// them.
type sendStack struct {
	p        *pgxpool.Pool
	url      string
	policies *authz.PolicyGRPCServer
	operator *http.Client
}

func newSendStack(t *testing.T) sendStack {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	orgs := orgpg.New(p)
	for _, name := range []string{"acme", "globex"} {
		require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN(name), name)))
	}
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	policies, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	operator := operatorClient(t, p, url, scopedGRN("acme", agent.ServiceTypeOperator, "one"))
	return sendStack{p: p, url: url, policies: policies, operator: operator}
}

// post sends body to recipient under key as client and answers the
// status.
func (s sendStack) post(t *testing.T, client *http.Client, recipient grn.GRN, key, body string) int {
	t.Helper()
	status, answer := machineSend(t, client, http.MethodPost, s.url+"/messages",
		`{"recipient":"`+recipient.String()+`","idempotencyKey":"`+key+`","body":"`+body+`"}`)
	require.Contains(t, []int{http.StatusCreated, http.StatusForbidden}, status, string(answer))
	return status
}

// distinctBody is a body no other row of the database holds, so a
// search for it finds this test's copy and nothing else.
func distinctBody(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return "body-" + hex.EncodeToString(b)
}

// TestSendAudit_ARefusedCrossingLeavesARefusedEventAndNoMessage covers
// #1157 scenario 3's refusal as the record holds it (ADR-0106): the
// post across a boundary no consent carries leaves one refused event
// naming the intended recipient and no message, and the refusal garam
// writes back to the sender stays. The send inside the organization is
// the control: the same sender's admitted post leaves an authorized
// event naming the message it stored, so what refuses the crossing is
// the missing consent and not a record that writes only refusals.
func TestSendAudit_ARefusedCrossingLeavesARefusedEventAndNoMessage(t *testing.T) {
	s := newSendStack(t)
	senderGRN, sender := provisioned(t, s.url, s.operator)
	siblingGRN, _ := provisioned(t, s.url, s.operator)
	foreign := scopedGRN("globex", agent.ServiceType, "0a1b2c3d4e5f6071")

	require.Equal(t, http.StatusCreated, s.post(t, sender, siblingGRN, "inside", "hello"))
	require.Equal(t, http.StatusCreated, s.post(t, sender, foreign, "across", "hello"))

	events := sendEvents(t, s.p, senderGRN)
	require.Len(t, events, 2)

	admitted := events[0]
	assert.Equal(t, siblingGRN.String(), admitted.recipient)
	assert.Equal(t, "authorized", admitted.decision)
	require.NotNil(t, admitted.messageID, "an authorized event names the message")
	var storedID string
	var storedAt time.Time
	require.NoError(t, s.p.QueryRow(context.Background(),
		"SELECT id, created_at FROM messages WHERE sender_grn = $1 AND idempotency_key = 'inside'",
		senderGRN.String()).Scan(&storedID, &storedAt))
	assert.Equal(t, storedID, *admitted.messageID)
	require.NotNil(t, admitted.messageCreatedAt)
	assert.True(t, storedAt.Equal(*admitted.messageCreatedAt))
	assert.False(t, admitted.recordedAt.Before(storedAt), "the event is stamped no earlier than its message")

	refused := events[1]
	assert.Equal(t, foreign.String(), refused.recipient)
	assert.Equal(t, "refused", refused.decision)
	assert.Nil(t, refused.messageID, "a refused event names no message")
	assert.Nil(t, refused.messageCreatedAt)
	assert.Equal(t, 0, countRows(t, s.p, "SELECT count(*) FROM messages WHERE recipient_grn = $1", foreign.String()),
		"the refused message was stored for its recipient")
	assert.Equal(t, 1, countRows(t, s.p,
		"SELECT count(*) FROM messages WHERE recipient_grn = $1 AND sender_grn = $2 AND author = 'garam'",
		senderGRN.String(), foreign.String()), "the refusal written back to the sender")

	// A retry is a send of its own, decided again, and names the
	// message its key already stores.
	require.Equal(t, http.StatusCreated, s.post(t, sender, siblingGRN, "inside", "hello"))
	events = sendEvents(t, s.p, senderGRN)
	require.Len(t, events, 3)
	assert.Equal(t, "authorized", events[2].decision)
	require.NotNil(t, events[2].messageID)
	assert.Equal(t, storedID, *events[2].messageID)
}

// TestSendAudit_NoEventCarriesABody sends one body three ways — stored,
// refused across the boundary, denied by a policy — and finds it in no
// event, by column and by value (ADR-0106). The messages table is the
// control for the value search: the same search finds the stored copy
// there, so an absence in the events is the record's and not a search
// that matches nothing. The denied post also leaves its refused event
// and no message.
func TestSendAudit_NoEventCarriesABody(t *testing.T) {
	ctx := context.Background()
	s := newSendStack(t)
	senderGRN, sender := provisioned(t, s.url, s.operator)
	siblingGRN, _ := provisioned(t, s.url, s.operator)
	deniedGRN, _ := provisioned(t, s.url, s.operator)
	foreign := scopedGRN("globex", agent.ServiceType, "0a1b2c3d4e5f6071")

	denied := activePolicy(t, s.policies, orgRootGRN("acme"), "no-talking",
		statementDoc("no-talking", wire.EffectDeny, message.ActionSend, deniedGRN.String()))
	attachPolicy(t, s.policies, denied, senderGRN)

	body := distinctBody(t)
	require.Equal(t, http.StatusCreated, s.post(t, sender, siblingGRN, "stored", body))
	require.Equal(t, http.StatusCreated, s.post(t, sender, foreign, "crossing", body))
	require.Equal(t, http.StatusForbidden, s.post(t, sender, deniedGRN, "denied", body))

	events := sendEvents(t, s.p, senderGRN)
	require.Len(t, events, 3, "every send writes one event")
	assert.Equal(t, []string{"authorized", "refused", "refused"},
		[]string{events[0].decision, events[1].decision, events[2].decision})
	assert.Equal(t, deniedGRN.String(), events[2].recipient)
	assert.Equal(t, 0, countRows(t, s.p,
		"SELECT count(*) FROM messages WHERE sender_grn = $1 AND idempotency_key = 'denied'", senderGRN.String()),
		"the denied post stored a message")

	rows, err := s.p.Query(ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_name = 'audit_send_events' ORDER BY ordinal_position`)
	require.NoError(t, err)
	var columns []string
	for rows.Next() {
		var c string
		require.NoError(t, rows.Scan(&c))
		columns = append(columns, c)
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, []string{"id", "sender_grn", "recipient_grn", "sender_org", "recipient_org",
		"decision", "message_id", "message_created_at", "recorded_at"}, columns,
		"the record holds no column a body could be written to")

	assert.Equal(t, 1, countRows(t, s.p,
		"SELECT count(*) FROM messages t WHERE strpos(row_to_json(t)::text, $1) > 0", body),
		"control: the search finds the stored body")
	assert.Equal(t, 0, countRows(t, s.p,
		"SELECT count(*) FROM audit_send_events t WHERE strpos(row_to_json(t)::text, $1) > 0", body),
		"an event carries the body")
	assert.Equal(t, 0, countRows(t, s.p,
		"SELECT count(*) FROM audit_send_events t WHERE strpos(row_to_json(t)::text, 'garam could not deliver') > 0"),
		"an event carries the refusal's body")
}

// TestSendAudit_DeletingAnOrganizationTakesTheEventsNamingIt holds the
// record to its tenants (ADR-0106): deleting the recipient's
// organization removes the crossing event naming it, and the event
// between two parties of the organization that stays is the control.
// The event naming an organization garam never held is written all the
// same, referencing none.
func TestSendAudit_DeletingAnOrganizationTakesTheEventsNamingIt(t *testing.T) {
	ctx := context.Background()
	s := newSendStack(t)
	senderGRN, sender := provisioned(t, s.url, s.operator)
	siblingGRN, _ := provisioned(t, s.url, s.operator)
	foreign := scopedGRN("globex", agent.ServiceType, "0a1b2c3d4e5f6071")
	nowhere := scopedGRN("initech", agent.ServiceType, "0a1b2c3d4e5f6071")

	require.Equal(t, http.StatusCreated, s.post(t, sender, siblingGRN, "inside", "hello"))
	require.Equal(t, http.StatusCreated, s.post(t, sender, foreign, "across", "hello"))
	require.Equal(t, http.StatusCreated, s.post(t, sender, nowhere, "nowhere", "hello"))

	naming := func(recipient grn.GRN) int {
		return countRows(t, s.p, "SELECT count(*) FROM audit_send_events WHERE recipient_grn = $1", recipient.String())
	}
	require.Equal(t, 1, naming(foreign), "setup: the crossing was recorded")
	assert.Equal(t, 1, countRows(t, s.p,
		"SELECT count(*) FROM audit_send_events WHERE recipient_grn = $1 AND recipient_org IS NULL", nowhere.String()),
		"an organization garam does not hold is recorded and referenced by nothing")

	require.NoError(t, orgpg.New(s.p).DeleteOrg(ctx, orgRootGRN("globex")))

	assert.Equal(t, 0, naming(foreign), "the crossing event outlived its recipient's organization")
	assert.Equal(t, 1, naming(siblingGRN), "control: an event naming neither deleted party stays")
	assert.Len(t, sendEvents(t, s.p, senderGRN), 2)
}

// TestSendAudit_ARefusedSubmissionWritesNoEvent holds the event to the
// outcome's transaction (ADR-0106): a durable submission the budget
// refuses, or whose key holds another message, stores nothing and
// records nothing. The submission inside the budget is the control: it
// stores its message and records it.
func TestSendAudit_ARefusedSubmissionWritesNoEvent(t *testing.T) {
	o := newOutbox(t, pool(t))
	bot, ada, eve := grnOf("agent", "bot"), grnOf("agent", "ada"), grnOf("agent", "eve")
	now := o.clock(t)

	stored, err := o.submit(t, bot, ada, "inside", "hello", now.Add(-time.Minute), nil)
	require.NoError(t, err)
	_, err = o.submit(t, bot, ada, "expired", "hello", now.Add(-message.SubmissionBudget-time.Second), nil)
	require.ErrorIs(t, err, message.ErrSubmissionExpired)
	_, err = o.submit(t, bot, ada, "later", "hello", now.Add(time.Minute), nil)
	require.ErrorIs(t, err, message.ErrCreatedLater)
	_, err = o.submit(t, bot, eve, "inside", "hello", now.Add(-time.Minute), nil)
	require.ErrorIs(t, err, message.ErrKeyHoldsAnotherMessage)

	events := sendEvents(t, o.p, bot)
	require.Len(t, events, 1, "a refused submission recorded a send")
	assert.Equal(t, "authorized", events[0].decision)
	require.NotNil(t, events[0].messageID)
	assert.Equal(t, string(stored.ID), *events[0].messageID)
}
