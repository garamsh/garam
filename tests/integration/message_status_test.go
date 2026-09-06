package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/org"
)

// statusOf reads how far a message has got, as its sender.
func statusOf(t *testing.T, sender *http.Client, url string) machinegen.MessageStatus {
	t.Helper()
	status, body := machineSend(t, sender, http.MethodGet, url, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var read machinegen.MessageStatus
	require.NoError(t, json.Unmarshal(body, &read))
	return read
}

// fieldsOf is what the answer carries, by name. The field set is what
// says the body is not among them: a completed message no longer has
// one (ADR-0034), so a field for it would be present in some states and
// absent in others, and an absent field is not what an assertion on one
// value would catch.
func fieldsOf(t *testing.T, body []byte) []string {
	t.Helper()
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &fields))
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	return names
}

// TestMessageStatus_ASenderReadsItsOwnMessageThroughEveryStateItReaches
// carries ADR-0055 over the listener a machine identity authenticates
// on: a sender posts, and from the identifier the post answered with it
// reads the message through waiting, being held, being delivered, and —
// for a second message nobody completes — being given up on. garam
// announces none of it; every answer here is one the sender asked for.
//
// Each read sits beside the same read made by the agent the message was
// addressed to. That agent authenticates, holds a certificate this
// listener verifies, and is the only identity that has actually handled
// the message — and it did not send it, so it reads nothing. What
// refuses is the sender comparison rather than a route that refuses
// whoever asks.
func TestMessageStatus_ASenderReadsItsOwnMessageThroughEveryStateItReaches(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)

	_, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)
	operator := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "one")))
	_, sender := provisioned(t, url, operator)
	recipientGRN, recipient := provisioned(t, url, operator)
	queue := url + "/agents/" + recipientGRN.String() + "/claims"

	status, body := machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-1"))
	require.Equal(t, http.StatusCreated, status, string(body))
	var stored machinegen.Message
	require.NoError(t, json.Unmarshal(body, &stored))
	read := url + "/messages/" + stored.Id

	// Nothing has looked at it yet, and what comes back is what garam
	// knows rather than what the sender submitted.
	status, body = machineSend(t, sender, http.MethodGet, read, "")
	require.Equal(t, http.StatusOK, status, string(body))
	assert.ElementsMatch(t, []string{"id", "state", "attempts", "createdAt"}, fieldsOf(t, body))
	var waiting machinegen.MessageStatus
	require.NoError(t, json.Unmarshal(body, &waiting))
	assert.Equal(t, machinegen.MessageStatePending, waiting.State)
	assert.Zero(t, waiting.Attempts, "a message nothing has been handed carries an attempt")
	assert.True(t, waiting.CreatedAt.Equal(stored.CreatedAt), "the read stamped the message again")

	// The recipient did not send it, so there is no such message for it
	// to read — and neither is there for the sender under an identifier
	// the queue never minted. One answer for both, so asking teaches a
	// caller nothing about the queue outside its own messages.
	status, body = machineSend(t, recipient, http.MethodGet, read, "")
	assert.Equal(t, http.StatusNotFound, status, string(body))
	status, body = machineSend(t, sender, http.MethodGet, url+"/messages/0123456789abcdef", "")
	assert.Equal(t, http.StatusNotFound, status, string(body))

	// A reader takes it. The sender can now tell a message being worked
	// on from one nothing has looked at.
	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	require.Equal(t, http.StatusOK, status, string(body))
	var claim machinegen.Claim
	require.NoError(t, json.Unmarshal(body, &claim))
	require.Equal(t, stored.Id, claim.Message.Id)
	held := statusOf(t, sender, read)
	assert.Equal(t, machinegen.MessageStateClaimed, held.State)
	assert.Equal(t, 1, held.Attempts)

	// The reader reports it delivered, which is the answer the sender
	// was posting for.
	completeOne(t, recipient, url, recipientGRN, claim)
	delivered := statusOf(t, sender, read)
	assert.Equal(t, machinegen.MessageStateCompleted, delivered.State)
	assert.Equal(t, 1, delivered.Attempts)

	// A second message, which the reader takes and never completes. The
	// queue hands one out a bounded number of times (ADR-0018), and the
	// sender watches the count rise.
	status, body = machineSend(t, sender, http.MethodPost, url+"/messages", submissionTo(recipientGRN, "key-2"))
	require.Equal(t, http.StatusCreated, status, string(body))
	var abandoned machinegen.Message
	require.NoError(t, json.Unmarshal(body, &abandoned))
	abandonedRead := url + "/messages/" + abandoned.Id

	for attempt := 1; attempt <= briefDelivery().Attempts; attempt++ {
		// The poll is the wait: a lapsed lease is a moment passing that
		// nothing announces, so the call that finds the message ready
		// again is what says the previous hold is over.
		status, body = machineSend(t, recipient, http.MethodPost, queue, "")
		require.Equal(t, http.StatusOK, status, string(body))
		var handed machinegen.Claim
		require.NoError(t, json.Unmarshal(body, &handed))
		require.Equal(t, abandoned.Id, handed.Message.Id)

		holding := statusOf(t, sender, abandonedRead)
		assert.Equal(t, machinegen.MessageStateClaimed, holding.State)
		assert.Equal(t, attempt, holding.Attempts)
	}

	// The queue is done with it: this poll waits out the last lease and
	// answers that nothing is ready, which is the condition the state
	// below rests on rather than a duration a test guessed at.
	status, body = machineSend(t, recipient, http.MethodPost, queue, "")
	require.Equal(t, http.StatusNoContent, status, string(body))
	spent := statusOf(t, sender, abandonedRead)
	assert.Equal(t, machinegen.MessageStateExhausted, spent.State,
		"a message the queue hands out no further still read as one waiting for a reader")
	assert.Equal(t, briefDelivery().Attempts, spent.Attempts)
}
