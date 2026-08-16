package machineapi_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
)

// messageCall is one thing the surface asked the message service for.
type messageCall struct {
	method    string
	caller    grn.GRN
	recipient grn.GRN
	claim     message.ClaimID
	key       string
	body      string
}

// messageService is a message.Service recording what the surface passed
// it. It hands back no claim: one is built by the store that read the
// row it holds, so what a claimed message looks like on the wire is the
// round trip in tests/e2e, over the real queue.
type messageService struct {
	err   error
	calls []messageCall
}

func (s *messageService) Post(_ context.Context, sender, recipient grn.GRN, key, body string) (*message.Message, error) {
	s.calls = append(s.calls, messageCall{method: "post", caller: sender, recipient: recipient, key: key, body: body})
	if s.err != nil {
		return nil, s.err
	}
	return message.New(recipient, sender, key, body)
}

func (s *messageService) Claim(_ context.Context, caller, recipient grn.GRN) (*message.Claim, error) {
	s.calls = append(s.calls, messageCall{method: "claim", caller: caller, recipient: recipient})
	if s.err != nil {
		return nil, s.err
	}
	return nil, message.ErrNoMessageReady
}

func (s *messageService) Renew(_ context.Context, caller, recipient grn.GRN, id message.ClaimID) (*message.Claim, error) {
	s.calls = append(s.calls, messageCall{method: "renew", caller: caller, recipient: recipient, claim: id})
	if s.err != nil {
		return nil, s.err
	}
	return nil, message.ErrStaleClaim
}

func (s *messageService) Complete(_ context.Context, caller, recipient grn.GRN, id message.ClaimID) error {
	s.calls = append(s.calls, messageCall{method: "complete", caller: caller, recipient: recipient, claim: id})
	return s.err
}

// send performs one request carrying a JSON body and returns the status
// and the body that came back.
func send(t *testing.T, client *http.Client, method, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(got)
}

// TestServer_PostsUnderTheSenderTheCertificateNames covers what reaches
// the queue and what comes back. The sender is never read off the body:
// a caller could name anyone there, and the certificate names one
// identity.
func TestServer_PostsUnderTheSenderTheCertificateNames(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	recipient := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	url, client := listener(t, &agentService{}, messages, sender)

	status, body := send(t, client, http.MethodPost, url+"/messages", `{
		"recipient": "`+recipient.String()+`",
		"idempotencyKey": "key-1",
		"body": "hello",
		"sender": "`+agentGRN("acme", "deadbeefdeadbeef").String()+`"
	}`)
	require.Equal(t, http.StatusCreated, status, body)
	assert.Contains(t, body, `"sender":"`+sender.String()+`"`)
	assert.Contains(t, body, `"recipient":"`+recipient.String()+`"`)
	assert.Contains(t, body, `"body":"hello"`)
	assert.Equal(t, []messageCall{{
		method:    "post",
		caller:    sender,
		recipient: recipient,
		key:       "key-1",
		body:      "hello",
	}}, messages.calls, "the sender is the caller and the recipient is what the body named")

	// A recipient that is not a GRN addresses nothing, and the queue is
	// never asked to store it.
	before := len(messages.calls)
	status, body = send(t, client, http.MethodPost, url+"/messages", `{"recipient":"bot-1","idempotencyKey":"key-2","body":"hello"}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, body, `"kind":"invalid_argument"`)
	status, body = send(t, client, http.MethodPost, url+"/messages", `not json`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, body, `"kind":"invalid_argument"`)
	assert.Len(t, messages.calls, before, "an unaddressed message reached the queue")
}

// TestServer_ActsOnTheQueueThePathNames covers the caller and the
// recipient reaching the service, and the kinds it answers with
// becoming statuses. Which of the two may act on the other's queue is
// internal/message's decision, so what is asserted here is that both
// arrive and that a refusal is not rendered as an answer.
func TestServer_ActsOnTheQueueThePathNames(t *testing.T) {
	caller := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	url, client := listener(t, &agentService{}, messages, caller)
	claims := url + "/agents/" + caller.String() + "/claims"

	// A poll that found nothing is the ordinary empty answer rather
	// than an error the caller has to tell apart.
	status, body := send(t, client, http.MethodPost, claims, "")
	assert.Equal(t, http.StatusNoContent, status)
	assert.Empty(t, body)

	// A claim the reader no longer holds is refused, and it is refused
	// as a conflict rather than as a missing route.
	status, body = send(t, client, http.MethodPost, claims+"/claim-1/renewal", "")
	assert.Equal(t, http.StatusConflict, status)
	assert.Contains(t, body, `"kind":"failed_precondition"`)

	status, body = send(t, client, http.MethodPost, claims+"/claim-1/completion", "")
	assert.Equal(t, http.StatusNoContent, status, body)

	assert.Equal(t, []messageCall{
		{method: "claim", caller: caller, recipient: caller},
		{method: "renew", caller: caller, recipient: caller, claim: "claim-1"},
		{method: "complete", caller: caller, recipient: caller, claim: "claim-1"},
	}, messages.calls, "the caller is what the certificate named and the queue is what the path named")

	// What the service refuses is a refusal here, carrying its kind
	// rather than a status invented at the boundary.
	messages.err = message.ErrNotRecipient
	for _, path := range []string{claims, claims + "/claim-1/renewal", claims + "/claim-1/completion"} {
		status, body := send(t, client, http.MethodPost, path, "")
		assert.Equal(t, http.StatusForbidden, status)
		assert.JSONEq(t, `{"kind":"permission_denied","message":"`+message.ErrNotRecipient.Error()+`"}`, body)
	}

	// A path segment that is not a GRN names no queue, and the service
	// is never asked about one.
	before := len(messages.calls)
	status, body = send(t, client, http.MethodPost, url+"/agents/bot-1/claims", "")
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, body, `"kind":"invalid_argument"`)
	assert.Len(t, messages.calls, before, "an unparsed recipient reached the service")
}
