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
)

// mailboxOrigin is the listener's external origin a bot's proof names,
// which is not the address the test dials.
const mailboxOrigin = "https://machine.garam.test"

// botValue is the bot token the tests present.
const botValue = bot.BotTokenPrefix + "live"

// serveMailbox serves the bot's mailbox with botValue live for bot,
// bound to key, and answers the listener and what each poll handed
// the queue.
func serveMailbox(t *testing.T, g grn.GRN, key *dpoptest.Key) (string, *http.Client, *messageService) {
	t.Helper()
	bots := &connectionClusters{held: map[string]bot.HeldBotToken{
		botValue: {Bot: g, Place: connectedGRN, KeyThumbprint: key.Thumbprint(t), ExpiresAt: time.Now().Add(time.Hour)},
	}}
	messages := &messageService{}
	proofs := dpop.NewVerifier(dpoptest.NewStore(nil), nil)
	base, client := serveTLS(t, machineapi.NewServer(&agentService{}, nil, messages, unwatched(t),
		nil, nil, nil, nil, nil, nil, stubKeyring{}).WithInstances(nil, bots, proofs, "", mailboxOrigin), connectedGRN)
	return base, client, messages
}

// pollAs polls path at base presenting botValue as DPoP with proof.
func pollAs(t *testing.T, client *http.Client, base, path, proof string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "DPoP "+botValue)
	req.Header.Set(dpop.Header, proof)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("WWW-Authenticate")
}

// TestPollBotMailbox_PollsAsTheBotTheTokenNamesForTheRequestedURL
// holds that the queue is asked as the bot the token names, for the
// bot the path names, with the cursor the query names, and that the
// proof has to name this request's URL at the listener's origin: one
// for another bot's mailbox is refused with a challenge, and asks the
// queue nothing.
func TestPollBotMailbox_PollsAsTheBotTheTokenNamesForTheRequestedURL(t *testing.T) {
	g, other := bot.BotGRN("acme", "decl-a"), bot.BotGRN("acme", "decl-b")
	key := dpoptest.NewKey(t)
	base, client, messages := serveMailbox(t, g, key)
	path := "/bots/" + url.PathEscape(g.String()) + "/mailbox"
	otherPath := "/bots/" + url.PathEscape(other.String()) + "/mailbox"

	status, challenge := pollAs(t, client, base, path,
		key.Proof(t, http.MethodPost, mailboxOrigin+otherPath, "", botValue))
	assert.Equal(t, http.StatusUnauthorized, status, "a proof for another URL")
	assert.Equal(t, `DPoP algs="ES256", error="invalid_dpop_proof"`, challenge)
	assert.Empty(t, messages.calls, "a refused poll reached the queue")

	status, _ = pollAs(t, client, base, path+"?cursor=c-1",
		key.Proof(t, http.MethodPost, mailboxOrigin+path, "", botValue))
	assert.Equal(t, http.StatusNoContent, status, "the control: a proof for this request")
	require.Len(t, messages.calls, 1)
	assert.Equal(t, messageCall{method: "poll", caller: g, recipient: g, claim: "c-1"}, messages.calls[0])
}
