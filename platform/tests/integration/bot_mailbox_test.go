package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

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
		out = append(out, anotherProcess(t, p, a, id))
	}
	return out, a
}

// anotherProcess is a process of the bot id placed on a's record,
// holding a token of its own bound to a key of its own, as a second
// process's exchange leaves it.
func anotherProcess(t *testing.T, p *pgxpool.Pool, a actor, id string) placedBot {
	t.Helper()
	key := dpoptest.NewKey(t)
	token, err := app.NewSecret(bot.BotTokenPrefix)
	require.NoError(t, err)
	_, err = botpg.New(p).RecordBotToken(context.Background(), a.g, id, app.HashSecret(a.token), app.HashSecret(token),
		key.Thumbprint(t), bot.BotTokenTTL)
	require.NoError(t, err)
	return placedBot{g: bot.BotGRN(a.g.Organization, id), key: key, token: token}
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
	client, _ := tokenClient(t)
	answer, err := pollOver(client, base, g, authorization, proof, cursor)
	require.NoError(t, err)
	return answer.status, answer.delivery, answer.header.Get("WWW-Authenticate")
}

// polled is one poll's answer.
type polled struct {
	status   int
	delivery machinegen.BotDelivery
	header   http.Header
}

// pollOver is pollMailbox over client, answering its failure rather
// than failing the test, so a goroutine may call it.
func pollOver(client *http.Client, base string, g grn.GRN, authorization, proof, cursor string) (polled, error) {
	target := base + mailboxPath(g)
	if cursor != "" {
		target += "?cursor=" + url.QueryEscape(cursor)
	}
	req, err := http.NewRequest(http.MethodPost, target, nil)
	if err != nil {
		return polled{}, err
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if proof != "" {
		req.Header.Set("DPoP", proof)
	}
	resp, err := client.Do(req)
	if err != nil {
		return polled{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return polled{}, err
	}
	answer := polled{status: resp.StatusCode, header: resp.Header}
	if resp.StatusCode == http.StatusOK {
		err = json.Unmarshal(body, &answer.delivery)
	}
	return answer, err
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

// TestBotMailbox_APollHandsOutAgainOnlyWhatItsOwnProcessWasHanded
// holds ADR-0194 decision 3 for two processes of one bot under two
// keys, as a rolling update leaves them: the second's poll is handed
// the next message and not the one the first holds, and the first's
// cursor still acknowledges its own.
func TestBotMailbox_APollHandsOutAgainOnlyWhatItsOwnProcessWasHanded(t *testing.T) {
	p := pool(t)
	bots, a := placedBots(t, p, "reviewer")
	first := bots[0]
	second := anotherProcess(t, p, a, "reviewer")
	_, sealer := keyEncryptionKey(t)
	base := machineListener(t, p, sealer)
	m1 := sendTo(t, p, first.g, "key-1", "first")
	m2 := sendTo(t, p, first.g, "key-2", "second")

	status, held := first.poll(t, base, "")
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, string(m1.ID), held.Message.Id)

	status, next := second.poll(t, base, "")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, string(m2.ID), next.Message.Id, "another process's poll takes no message the first holds")
	attempts, _ := handOuts(t, p, m1)
	assert.Equal(t, 1, attempts, "the first process's message was not handed out again")

	// The second's poll released none of the first's claims, so the
	// first's cursor still names the claim m1 is held under.
	status, _ = first.poll(t, base, held.Cursor)
	assert.NotEqual(t, http.StatusConflict, status, "the first process's cursor still acknowledges")
	_, acknowledged := handOuts(t, p, m1)
	assert.True(t, acknowledged)
}

// TestBotMailbox_HoldsTwoPollsABotAtOnce sends two polls of one bot at
// once, under two keys, to an empty mailbox, and a message while both
// wait: one of them is handed it and the other answers 204, and a
// third poll while both are held is answered 429 with a Retry-After.
// Once both answered, the same third poll is admitted.
func TestBotMailbox_HoldsTwoPollsABotAtOnce(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	bots, a := placedBots(t, p, "reviewer")
	first := bots[0]
	second := anotherProcess(t, p, a, "reviewer")
	_, sealer := keyEncryptionKey(t)
	base := machineListener(t, p, sealer)

	answers := make(chan polled, 2)
	for _, b := range []placedBot{first, second} {
		proof := proofFor(t, b.key, b.g, b.token)
		client, _ := tokenClient(t)
		go func() {
			answer, err := pollOver(client, base, b.g, "DPoP "+b.token, proof, "")
			assert.NoError(t, err)
			answers <- answer
		}()
	}
	// Each poll spends its proof's jti once its token resolved and
	// before it is held, so a jti spent under each key is two polls
	// past the point a third would be counted against. The keys are
	// this test's own; dpop_jtis keeps other tests' rows.
	keys := []string{first.key.Thumbprint(t), second.key.Thumbprint(t)}
	require.Eventually(t, func() bool {
		var spent int
		err := p.QueryRow(ctx, `SELECT count(*) FROM dpop_jtis WHERE jkt = ANY($1)`, keys).Scan(&spent)
		return err == nil && spent == 2
	}, 2*time.Second, 10*time.Millisecond)
	client, _ := tokenClient(t)
	third, err := pollOver(client, base, first.g, "DPoP "+first.token, proofFor(t, first.key, first.g, first.token), "")
	require.NoError(t, err)
	assert.Equal(t, http.StatusTooManyRequests, third.status, "a third poll held at once")
	assert.NotEmpty(t, third.header.Get("Retry-After"))

	// The other poll goes on waiting, and is handed the message only
	// once the first hand-out's lease lapsed, which the suite's brief
	// delivery puts inside that wait.
	sent := sendTo(t, p, first.g, "key-1", "while both wait")
	handed := <-answers
	require.Equal(t, http.StatusOK, handed.status, "one poll is handed the message")
	assert.Equal(t, 1, handed.delivery.Attempt)
	attempts, _ := handOuts(t, p, sent)
	assert.Equal(t, 1, attempts, "the other poll did not take it from the first")
	<-answers

	again, err := pollOver(client, base, first.g, "DPoP "+first.token, proofFor(t, first.key, first.g, first.token), "")
	require.NoError(t, err)
	assert.NotEqual(t, http.StatusTooManyRequests, again.status, "the control: a poll once both answered")
}

// TestBotMailbox_APollReleasesOnlyItsOwnBotsClaims holds that a poll
// lapses the claims of its own bot alone: an agent's and another
// bot's claim, taken under the very key the poll is held under, keep
// their leases. The control is the polling bot's own claim under that
// key, which the poll hands out again.
func TestBotMailbox_APollReleasesOnlyItsOwnBotsClaims(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	bots, _ := placedBots(t, p, "reviewer", "other")
	b, other := bots[0], bots[1]
	_, sealer := keyEncryptionKey(t)
	base := machineListener(t, p, sealer)
	store := messagepg.New(p)
	holder := b.key.Thumbprint(t)
	worker := grnOf("agent", "worker")

	leases := map[string]time.Time{}
	for _, recipient := range []grn.GRN{worker, other.g} {
		m := sendTo(t, p, recipient, "key-"+recipient.Identifier, "held elsewhere")
		_, err := store.ClaimHeld(ctx, recipient, holder, time.Minute, 8, nil)
		require.NoError(t, err)
		var until time.Time
		require.NoError(t, p.QueryRow(ctx, `SELECT claim_expires_at FROM messages WHERE id = $1`, string(m.ID)).Scan(&until))
		leases[string(m.ID)] = until
	}
	own := sendTo(t, p, b.g, "key-own", "the bot's own")
	_, err := store.ClaimHeld(ctx, b.g, holder, time.Minute, 8, nil)
	require.NoError(t, err)

	status, delivery := b.poll(t, base, "")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, string(own.ID), delivery.Message.Id, "the control: the bot's own claim is handed out again")
	assert.Equal(t, 2, delivery.Attempt)
	for id, until := range leases {
		var now time.Time
		require.NoError(t, p.QueryRow(ctx, `SELECT claim_expires_at FROM messages WHERE id = $1`, id).Scan(&now))
		assert.True(t, now.Equal(until), "another recipient's claim kept its lease")
	}
}

// TestBotMailbox_RefusesATokenOnceWhatItRestsOnIsWithdrawn holds that
// the mailbox refuses a token once its declaration is tombstoned, its
// connector key revoked or past its rollover, or its installation's
// hosting of bots withdrawn, each 401 invalid_token, and hands the
// waiting message to none of them. The control in each is the same
// bot's poll before the withdrawal, which is handed a message.
func TestBotMailbox_RefusesATokenOnceWhatItRestsOnIsWithdrawn(t *testing.T) {
	withdrawals := map[string]func(t *testing.T, p *pgxpool.Pool, a actor){
		"a tombstoned declaration": func(t *testing.T, p *pgxpool.Pool, a actor) {
			_, err := botpg.New(p).Tombstone(context.Background(), a.g, "reviewer")
			require.NoError(t, err)
		},
		"a revoked connector key": func(t *testing.T, p *pgxpool.Pool, a actor) {
			tag, err := p.Exec(context.Background(),
				`UPDATE instance_keys SET revoked_at = now() WHERE instance_grn = $1`, a.g.String())
			require.NoError(t, err)
			require.Equal(t, int64(1), tag.RowsAffected())
		},
		// No trigger takes a token whose key passed its rollover, so
		// the token's read is what refuses it.
		"a connector key past its rollover": func(t *testing.T, p *pgxpool.Pool, a actor) {
			tag, err := p.Exec(context.Background(),
				`UPDATE instance_keys SET retires_at = now() - interval '1 second' WHERE instance_grn = $1`, a.g.String())
			require.NoError(t, err)
			require.Equal(t, int64(1), tag.RowsAffected())
		},
		"a withdrawal of bot hosting": func(t *testing.T, p *pgxpool.Pool, a actor) {
			ctx := context.Background()
			var installation string
			require.NoError(t, p.QueryRow(ctx, `SELECT installation_grn FROM instances WHERE grn = $1`,
				a.g.String()).Scan(&installation))
			g, err := grn.Parse(installation)
			require.NoError(t, err)
			require.NoError(t, botpg.New(p).RevokeInstallationBotTokens(ctx, g))
		},
	}
	for name, withdraw := range withdrawals {
		t.Run(name, func(t *testing.T) {
			p := pool(t)
			bots, a := placedBots(t, p, "reviewer")
			b := bots[0]
			_, sealer := keyEncryptionKey(t)
			base := machineListener(t, p, sealer)
			sendTo(t, p, b.g, "key-1", "before")
			status, _ := b.poll(t, base, "")
			require.Equal(t, http.StatusOK, status, "the control: the poll before the withdrawal")

			waiting := sendTo(t, p, b.g, "key-2", "after")
			withdraw(t, p, a)
			status, _, challenge := pollMailbox(t, base, b.g, "DPoP "+b.token, proofFor(t, b.key, b.g, b.token), "")
			assert.Equal(t, http.StatusUnauthorized, status)
			assert.Equal(t, `DPoP algs="ES256", error="invalid_token"`, challenge)
			attempts, _ := handOuts(t, p, waiting)
			assert.Zero(t, attempts, "a refused poll handed the message out")
		})
	}
}
