package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/bot"
	botpg "github.com/garamsh/garam/platform/internal/bot/postgres"
	"github.com/garamsh/garam/platform/internal/dpop/dpoptest"
	"github.com/garamsh/garam/platform/internal/message"
	messagepg "github.com/garamsh/garam/platform/internal/message/postgres"
)

// placedBot is a bot garam declared and placed, holding a token its
// connector exchanged and bound to its process's key.
type placedBot struct {
	g     grn.GRN
	key   *dpoptest.Key
	token string
}

// placedBots declares a Bot for each id on one connected record and
// records a token for each, bound to a key of its own, as the bot
// token exchange does. It answers them and the record's actor.
func placedBots(t *testing.T, p *pgxpool.Pool, ids ...string) ([]placedBot, actor) {
	t.Helper()
	ctx := context.Background()
	s := botpg.New(p)
	a := connectedActors(t, p, 1)[0]
	out := make([]placedBot, 0, len(ids))
	for _, id := range ids {
		_, _, err := s.Declare(ctx, a.g, id, object(t, id))
		require.NoError(t, err)
		key := dpoptest.NewKey(t)
		token, err := app.NewSecret(bot.BotTokenPrefix)
		require.NoError(t, err)
		_, err = s.RecordBotToken(ctx, a.g, id, app.HashSecret(a.token), app.HashSecret(token), key.Thumbprint(t),
			bot.BotTokenTTL)
		require.NoError(t, err)
		out = append(out, placedBot{g: bot.BotGRN(a.g.Organization, id), key: key, token: token})
	}
	return out, a
}

// mailboxPath is the path of g's mailbox, its GRN one segment.
func mailboxPath(g grn.GRN) string {
	return "/bots/" + url.PathEscape(g.String()) + "/mailbox"
}

// proofFor is a proof by key for a poll of g's mailbox at garam's
// origin, with the ath of token where it is not empty.
func proofFor(t *testing.T, key *dpoptest.Key, g grn.GRN, token string) string {
	t.Helper()
	return key.Proof(t, http.MethodPost, string(testIssuer)+mailboxPath(g), "", token)
}

// pollMailbox polls g's mailbox at base with authorization and proof,
// each where not empty, naming cursor where not empty. It answers the
// status, the delivery a 200 carries, and the challenge.
func pollMailbox(t *testing.T, base string, g grn.GRN, authorization, proof, cursor string) (int, machinegen.BotDelivery, string) {
	t.Helper()
	target := base + mailboxPath(g)
	if cursor != "" {
		target += "?cursor=" + url.QueryEscape(cursor)
	}
	req, err := http.NewRequest(http.MethodPost, target, nil)
	require.NoError(t, err)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if proof != "" {
		req.Header.Set("DPoP", proof)
	}
	client, _ := tokenClient(t)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var delivery machinegen.BotDelivery
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.Unmarshal(body, &delivery))
	}
	return resp.StatusCode, delivery, resp.Header.Get("WWW-Authenticate")
}

// poll is b's own poll of its own mailbox, as its process makes it.
func (b placedBot) poll(t *testing.T, base, cursor string) (int, machinegen.BotDelivery) {
	t.Helper()
	status, delivery, _ := pollMailbox(t, base, b.g, "DPoP "+b.token, proofFor(t, b.key, b.g, b.token), cursor)
	return status, delivery
}

// sendTo stores a message to recipient from an agent of its
// organization, as postMessage admits one, and answers it.
func sendTo(t *testing.T, p *pgxpool.Pool, recipient grn.GRN, key, body string) *message.Message {
	t.Helper()
	m, err := message.New(recipient, grnOf("agent", "sender"), key, body)
	require.NoError(t, err)
	stored, err := messagepg.New(p).Enqueue(context.Background(), m, nil)
	require.NoError(t, err)
	return stored
}

// handOuts answers how many times m was handed out and whether it was
// acknowledged.
func handOuts(t *testing.T, p *pgxpool.Pool, m *message.Message) (attempts int, acknowledged bool) {
	t.Helper()
	require.NoError(t, p.QueryRow(context.Background(),
		`SELECT attempts, completed_at IS NOT NULL FROM messages WHERE id = $1`, string(m.ID)).Scan(&attempts, &acknowledged))
	return attempts, acknowledged
}

// TestBotMailbox_ResumesFromItsCursorAfterADroppedConnection holds
// ADR-0194's cursor: a message is handed out until a poll names its
// cursor, so a process whose answer was lost with its connection
// polls again with the cursor it last held and is handed the lost
// message before any later one. A cursor whose message was handed out
// again acknowledges nothing, and an empty wait answers 204.
func TestBotMailbox_ResumesFromItsCursorAfterADroppedConnection(t *testing.T) {
	p := pool(t)
	bots, _ := placedBots(t, p, "reviewer")
	b := bots[0]
	_, sealer := keyEncryptionKey(t)
	base := machineListener(t, p, sealer)
	first := sendTo(t, p, b.g, "key-1", "first")
	second := sendTo(t, p, b.g, "key-2", "second")

	status, lost := b.poll(t, base, "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, string(first.ID), lost.Message.Id, "the oldest message comes first")
	assert.Equal(t, 1, lost.Attempt)
	assert.Equal(t, "first", lost.Message.Body)
	assert.NotEmpty(t, lost.Signature, "every hand-out is signed")

	// The answer above is lost with its connection, so the process
	// still holds no cursor and polls with none.
	status, resumed := b.poll(t, base, "")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, string(first.ID), resumed.Message.Id, "the lost message is handed out again, before the next")
	assert.Equal(t, 2, resumed.Attempt)
	require.NotEqual(t, lost.Cursor, resumed.Cursor)
	attempts, acknowledged := handOuts(t, p, first)
	assert.Equal(t, 2, attempts)
	assert.False(t, acknowledged, "no poll named a cursor yet")

	status, _ = b.poll(t, base, lost.Cursor)
	assert.Equal(t, http.StatusConflict, status, "the lost hand-out's cursor was handed out again")
	_, acknowledged = handOuts(t, p, first)
	assert.False(t, acknowledged, "a stale cursor acknowledges nothing")

	status, next := b.poll(t, base, resumed.Cursor)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, string(second.ID), next.Message.Id)
	_, acknowledged = handOuts(t, p, first)
	assert.True(t, acknowledged, "the cursor acknowledged its message")

	status, _ = b.poll(t, base, next.Cursor)
	assert.Equal(t, http.StatusNoContent, status, "an empty wait answers no message")
	_, acknowledged = handOuts(t, p, second)
	assert.True(t, acknowledged)
	status, _ = b.poll(t, base, "")
	assert.Equal(t, http.StatusNoContent, status, "an acknowledged message is not handed out again")
}

// TestBotMailbox_RefusesEveryTokenButTheBotsOwn holds ADR-0191
// decision 6 at the first route that accepts a bot's token: no
// token, a token sent as Bearer, a cluster's token, an expired token,
// a proof by another key and a replayed proof are each refused 401
// with a challenge, and another bot's token 403, and none of them
// hands the waiting message out. The control is the bot's own poll,
// which is handed it.
func TestBotMailbox_RefusesEveryTokenButTheBotsOwn(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	bots, a := placedBots(t, p, "reviewer", "other", "late")
	b, other, late := bots[0], bots[1], bots[2]
	_, sealer := keyEncryptionKey(t)
	base := machineListener(t, p, sealer)
	waiting := sendTo(t, p, b.g, "key-1", "for the reviewer")
	_, err := p.Exec(ctx, `UPDATE bot_tokens SET issued_at = issued_at - interval '2 hours',
		expires_at = expires_at - interval '2 hours' WHERE token_hash = $1`, app.HashSecret(late.token))
	require.NoError(t, err)

	refusals := []struct {
		name          string
		authorization string
		proof         string
		status        int
	}{
		{"no token", "", b.key.Proof(t, http.MethodPost, string(testIssuer)+mailboxPath(b.g), "", ""), http.StatusUnauthorized},
		{"the bound token as Bearer", "Bearer " + b.token, proofFor(t, b.key, b.g, b.token), http.StatusUnauthorized},
		{"a cluster's token", "DPoP " + a.token, proofFor(t, b.key, b.g, a.token), http.StatusUnauthorized},
		{"an expired token", "DPoP " + late.token, proofFor(t, late.key, b.g, late.token), http.StatusUnauthorized},
		{"a proof by another key", "DPoP " + b.token, proofFor(t, other.key, b.g, b.token), http.StatusUnauthorized},
		{"no proof", "DPoP " + b.token, "", http.StatusUnauthorized},
		{"another bot's token", "DPoP " + other.token, proofFor(t, other.key, b.g, other.token), http.StatusForbidden},
	}
	for _, tt := range refusals {
		status, _, challenge := pollMailbox(t, base, b.g, tt.authorization, tt.proof, "")
		assert.Equal(t, tt.status, status, tt.name)
		if tt.status == http.StatusUnauthorized {
			assert.Contains(t, challenge, `DPoP algs="ES256", error=`, tt.name)
		}
	}
	attempts, _ := handOuts(t, p, waiting)
	assert.Zero(t, attempts, "a refused poll handed the message out")

	proof := proofFor(t, b.key, b.g, b.token)
	status, delivery, _ := pollMailbox(t, base, b.g, "DPoP "+b.token, proof, "")
	require.Equal(t, http.StatusOK, status, "the control: the bot's own token and proof")
	assert.Equal(t, string(waiting.ID), delivery.Message.Id)

	status, _, challenge := pollMailbox(t, base, b.g, "DPoP "+b.token, proof, delivery.Cursor)
	assert.Equal(t, http.StatusUnauthorized, status, "a proof is spent once")
	assert.Contains(t, challenge, `error="invalid_dpop_proof"`)
	_, acknowledged := handOuts(t, p, waiting)
	assert.False(t, acknowledged, "a replayed proof acknowledged the message")
}
