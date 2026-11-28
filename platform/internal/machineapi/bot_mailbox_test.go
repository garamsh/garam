package machineapi_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/dpop"
	"github.com/garamsh/garam/platform/internal/dpop/dpoptest"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/operator"
)

// mailboxOrigin is the listener's external origin a bot's proof names,
// which is not the address the test dials.
const mailboxOrigin = "https://machine.garam.test"

// botValue is the bot token the tests present.
const botValue = bot.BotTokenPrefix + "live"

// mailbox is the bot's mailbox served with botValue live for one bot,
// bound to key, and what each poll handed the queue.
type mailbox struct {
	base     string
	client   *http.Client
	bots     *connectionClusters
	messages *messageService
}

// serveMailbox serves the bot's mailbox with botValue live for g,
// bound to key.
func serveMailbox(t *testing.T, g grn.GRN, key *dpoptest.Key) mailbox {
	t.Helper()
	bots := &connectionClusters{held: map[string]bot.HeldBotToken{
		botValue: {Bot: g, Place: connectedGRN, KeyThumbprint: key.Thumbprint(t), ExpiresAt: time.Now().Add(time.Hour)},
	}}
	messages := &messageService{}
	proofs := dpop.NewVerifier(dpoptest.NewStore(nil), nil)
	base, client := serveTLS(t, machineapi.NewServer(&agentService{}, nil, messages, unwatched(t),
		nil, nil, nil, nil, nil, nil, stubKeyring{}).WithInstances(nil, bots, proofs, "", mailboxOrigin), connectedGRN)
	return mailbox{base: base, client: client, bots: bots, messages: messages}
}

// mailboxOf is the path of g's mailbox.
func mailboxOf(g grn.GRN) string {
	return "/bots/" + url.PathEscape(g.String()) + "/mailbox"
}

// poll polls path presenting each of authorizations with proof, and
// answers the status, the challenge and the Retry-After.
func (m mailbox) poll(t *testing.T, path, proof string, authorizations ...string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, m.base+path, nil)
	require.NoError(t, err)
	for _, a := range authorizations {
		req.Header.Add("Authorization", a)
	}
	req.Header.Set(dpop.Header, proof)
	resp, err := m.client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("WWW-Authenticate"), resp.Header.Get("Retry-After")
}

// TestPollBotMailbox_PollsAsTheTokensBotUnderItsKeyForTheRequestedURL
// holds that the queue is asked as the bot the token names, for the
// bot the path names, under the key the token is bound to, with the
// cursor the query names, and that the proof has to name this
// request's URL at the listener's origin: one for another bot's
// mailbox is refused with a challenge, and asks the queue nothing.
func TestPollBotMailbox_PollsAsTheTokensBotUnderItsKeyForTheRequestedURL(t *testing.T) {
	g, other := bot.BotGRN("acme", "decl-a"), bot.BotGRN("acme", "decl-b")
	key := dpoptest.NewKey(t)
	m := serveMailbox(t, g, key)

	status, challenge, _ := m.poll(t, mailboxOf(g),
		key.Proof(t, http.MethodPost, mailboxOrigin+mailboxOf(other), "", botValue), "DPoP "+botValue)
	assert.Equal(t, http.StatusUnauthorized, status, "a proof for another URL")
	assert.Equal(t, `DPoP algs="ES256", error="invalid_dpop_proof"`, challenge)
	assert.Empty(t, m.messages.calls, "a refused poll reached the queue")

	status, _, _ = m.poll(t, mailboxOf(g)+"?cursor=c-1",
		key.Proof(t, http.MethodPost, mailboxOrigin+mailboxOf(g), "", botValue), "DPoP "+botValue)
	assert.Equal(t, http.StatusNoContent, status, "the control: a proof for this request")
	require.Len(t, m.messages.calls, 1)
	assert.Equal(t, messageCall{method: "poll", caller: g, recipient: g, claim: "c-1", holder: key.Thumbprint(t)},
		m.messages.calls[0])
}

// TestPollBotMailbox_RefusesATokenBesideAnotherAuthorization holds the
// listener's rule that a request presenting a token carries one
// Authorization header, which this route keeps itself. The control is
// the same token alone, which reaches the queue.
func TestPollBotMailbox_RefusesATokenBesideAnotherAuthorization(t *testing.T) {
	g := bot.BotGRN("acme", "decl-a")
	key := dpoptest.NewKey(t)
	m := serveMailbox(t, g, key)
	proof := func() string { return key.Proof(t, http.MethodPost, mailboxOrigin+mailboxOf(g), "", botValue) }

	status, challenge, _ := m.poll(t, mailboxOf(g), proof(), "DPoP "+botValue, "DPoP "+botValue)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, `DPoP algs="ES256", error="invalid_token"`, challenge)
	assert.Empty(t, m.messages.calls, "a refused poll reached the queue")

	status, _, _ = m.poll(t, mailboxOf(g), proof(), "DPoP "+botValue)
	assert.Equal(t, http.StatusNoContent, status, "the control: one Authorization header")
	assert.Len(t, m.messages.calls, 1)
}

// TestPollBotMailbox_AnswersAPollPastTheBotsBound429 holds that a poll
// the bot domain does not hold is answered 429 with its Retry-After,
// and asks the queue nothing.
func TestPollBotMailbox_AnswersAPollPastTheBotsBound429(t *testing.T) {
	g := bot.BotGRN("acme", "decl-a")
	key := dpoptest.NewKey(t)
	m := serveMailbox(t, g, key)
	m.bots.holdErr = &operator.Exceeded{RetryAfter: 30 * time.Second}

	status, _, retryAfter := m.poll(t, mailboxOf(g),
		key.Proof(t, http.MethodPost, mailboxOrigin+mailboxOf(g), "", botValue), "DPoP "+botValue)
	assert.Equal(t, http.StatusTooManyRequests, status)
	assert.Equal(t, "30", retryAfter)
	assert.Empty(t, m.messages.calls, "a poll past the bound reached the queue")
}
