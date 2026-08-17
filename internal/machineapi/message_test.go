package machineapi_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/message"
)

// grnObject adapts a GRN to authz.Object; the engine matches strings.
type grnObject struct{ g grn.GRN }

func (o grnObject) GRN() grn.GRN { return o.g }
func (o grnObject) Type() string { return o.g.ServiceType }

// engine answers the Evaluator seam with the real policy engine over
// the statements the surface collected, so what these tests exercise is
// the collection rather than a stubbed verdict. attached stands in for
// the policies an organization attached to a subject: the engine unions
// them with what the surface supplies, and a Deny from either wins
// (ADR-0007, ADR-0008).
//
// A subject here is what an attachment may name — the caller, and each
// group the request reports it belongs to — which is how the engine
// itself reads its attachments (authz.md §Policy attachment).
type engine struct {
	attached map[grn.GRN][]authz.Statement
	err      error
	asked    []machineapi.EvalInput
}

func (e *engine) Evaluate(ctx context.Context, in machineapi.EvalInput) (bool, error) {
	e.asked = append(e.asked, in)
	if e.err != nil {
		return false, e.err
	}
	var statements []authz.Statement
	for _, subject := range append([]grn.GRN{in.Subject}, in.SubjectGroups...) {
		statements = append(statements, e.attached[subject]...)
	}
	for _, s := range in.Statements {
		statements = append(statements, authz.Statement{
			Sid:      s.Sid,
			Effect:   authz.Effect(s.Effect),
			Action:   s.Action,
			Resource: s.Resource,
		})
	}
	d := authz.NewStatementEvaluator(statements).
		Evaluate(ctx, authz.Context{Object: grnObject{in.Object}, Action: in.Action})
	return d == authz.DecisionAllow, nil
}

// denySending is one attached policy statement refusing what the
// recipient pattern names.
func denySending(sid, resource string) authz.Statement {
	return authz.Statement{
		Sid:      sid,
		Effect:   authz.EffectDeny,
		Action:   []string{message.ActionSend},
		Resource: []string{resource},
	}
}

// submission is the body a sender posts to address recipient.
func submission(recipient grn.GRN, key string) string {
	return `{"recipient":"` + recipient.String() + `","idempotencyKey":"` + key + `","body":"hello"}`
}

// posted is the call the surface makes to the queue for a message it
// accepted.
func posted(sender, recipient grn.GRN, key string) messageCall {
	return messageCall{method: "post", caller: sender, recipient: recipient, key: key, body: "hello"}
}

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
	url, client := listener(t, &agentService{}, messages, &memberGroups{}, &engine{}, sender)

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
	url, client := listener(t, &agentService{}, messages, &memberGroups{}, &engine{}, caller)
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

// TestServer_SendsWhereTheSendersOwnOrganizationReaches covers what an
// agent nobody has written a policy for may send. The refusal sits
// beside the accepted message: both are the same call by the same
// sender with nothing attached to it, so what discriminates is the
// recipient's organization rather than a surface that refuses whoever
// asks.
//
// The engine is never asked about the recipient of another
// organization. That is the property rather than an optimisation: no
// answer it could give would be trusted, because a policy of the
// recipient's own organization can allow every sender at once
// (ADR-0024).
func TestServer_SendsWhereTheSendersOwnOrganizationReaches(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	stranger := agentGRN("globex", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	policies := &engine{}
	url, client := listener(t, &agentService{}, messages, &memberGroups{}, policies, sender)

	status, body := send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-1"))
	require.Equal(t, http.StatusCreated, status, body)

	status, body = send(t, client, http.MethodPost, url+"/messages", submission(stranger, "key-2"))
	assert.Equal(t, http.StatusForbidden, status, body)
	assert.JSONEq(t, `{"kind":"permission_denied","message":"recipient is in another organization"}`, body)

	// A refused message is not stored. It is not the routing failure
	// ADR-0013 keeps: garam could route this one and declined to.
	assert.Equal(t, []messageCall{posted(sender, sibling, "key-1")}, messages.calls)

	// One question was asked and it was the recipient of this
	// organization: the sender, the action and the recipient, carrying
	// what the sender's own organization grants it. The other recipient
	// was refused without a policy deciding anything.
	ownOrg := []machineapi.Statement{{
		Sid:      "machine-own-org",
		Effect:   machineapi.EffectAllow,
		Action:   []string{message.ActionSend},
		Resource: []string{"grn:acme:*"},
	}}
	assert.Equal(t, []machineapi.EvalInput{
		{Subject: sender, Object: sibling, Action: message.ActionSend, Statements: ownOrg},
	}, policies.asked)
}

// TestServer_AnAttachedPolicyDecidesWhatAnAgentSends is the whole of
// what an attachment buys: the same sender making the same call is
// refused with the policy attached and answered without it, and the
// agent beside it that holds no policy is answered throughout.
//
// Three controls sit against the one refusal, because a 403 holds as
// readily for a surface that refuses everything: the same sender
// reaching a recipient the Deny does not name, another sender reaching
// the one it does, and the same call once the attachment is gone.
func TestServer_AnAttachedPolicyDecidesWhatAnAgentSends(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	refused := agentGRN("acme", "deadbeefdeadbeef")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	policies := &engine{attached: map[grn.GRN][]authz.Statement{
		sender: {denySending("no-talking", refused.String())},
	}}
	url, client := listener(t, &agentService{}, messages, &memberGroups{}, policies, sender)

	status, body := send(t, client, http.MethodPost, url+"/messages", submission(refused, "key-1"))
	require.Equal(t, http.StatusForbidden, status, body)
	// A policy refused this one, and the recipient's organization
	// refused the one in TestServer_SendsWhereTheSendersOwnOrganizationReaches.
	// Both are 403 and they are not the same answer: one is an
	// organization's own rule and the other is a boundary no policy
	// reaches, so a caller reading the body can tell them apart.
	assert.JSONEq(t, `{"kind":"permission_denied","message":"sender may not send to this recipient"}`, body)

	status, body = send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-2"))
	require.Equal(t, http.StatusCreated, status, body)

	// The attachment names one subject, so the agent beside it reaches
	// the recipient this one may not.
	other := agentGRN("acme", "1122334455667788")
	otherURL, otherClient := listener(t, &agentService{}, messages, &memberGroups{}, policies, other)
	status, body = send(t, otherClient, http.MethodPost, otherURL+"/messages", submission(refused, "key-3"))
	require.Equal(t, http.StatusCreated, status, body)

	// Detaching the policy is what puts the refused recipient back in
	// reach, so the refusal above was the policy and nothing else.
	delete(policies.attached, sender)
	status, body = send(t, client, http.MethodPost, url+"/messages", submission(refused, "key-4"))
	require.Equal(t, http.StatusCreated, status, body)

	assert.Equal(t, []messageCall{
		posted(sender, sibling, "key-2"),
		posted(other, refused, "key-3"),
		posted(sender, refused, "key-4"),
	}, messages.calls, "the refused message reached the queue")
}

// TestServer_AGroupAttachedPolicyDecidesWhatItsMembersSend moves one
// document between attachment targets and asserts the call ends the
// same way both times: attached to the group the sender belongs to and
// attached to the sender itself. An attachment that decides nothing
// reads as a control that is not there (ADR-0008), and a group holding
// agents is a target an administrator can pick.
//
// Three controls sit against the refusal: the agent in no group making
// the same call, the same sender reaching a recipient the Deny does not
// name, and the same call once nothing is attached anywhere.
func TestServer_AGroupAttachedPolicyDecidesWhatItsMembersSend(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	outsider := agentGRN("acme", "1122334455667788")
	refused := agentGRN("acme", "deadbeefdeadbeef")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	engineering := groupGRN("acme", "engineering")
	noTalking := denySending("no-talking", refused.String())

	messages := &messageService{}
	groups := &memberGroups{byMember: map[grn.GRN][]grn.GRN{sender: {engineering}}}
	policies := &engine{attached: map[grn.GRN][]authz.Statement{engineering: {noTalking}}}
	url, client := listener(t, &agentService{}, messages, groups, policies, sender)
	outsiderURL, outsiderClient := listener(t, &agentService{}, messages, groups, policies, outsider)

	status, body := send(t, client, http.MethodPost, url+"/messages", submission(refused, "key-1"))
	require.Equal(t, http.StatusForbidden, status, body)
	assert.JSONEq(t, `{"kind":"permission_denied","message":"sender may not send to this recipient"}`, body)
	assert.Equal(t, []grn.GRN{sender}, groups.asked,
		"the groups asked for are the ones the certificate's identity belongs to")
	require.Len(t, policies.asked, 1)
	assert.Equal(t, []grn.GRN{engineering}, policies.asked[0].SubjectGroups,
		"the check carried the group the sender belongs to")

	// The Deny names one recipient, so the member reaches the agent
	// beside it.
	status, body = send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-2"))
	require.Equal(t, http.StatusCreated, status, body)

	// The agent that is in no group is answered under the same policy.
	status, body = send(t, outsiderClient, http.MethodPost, outsiderURL+"/messages", submission(refused, "key-3"))
	require.Equal(t, http.StatusCreated, status, body)

	// The same document attached to the sender itself refuses the same
	// call. This is the control the failure turned on: the two
	// attachments answered differently while a group reached nothing.
	policies.attached = map[grn.GRN][]authz.Statement{sender: {noTalking}}
	status, body = send(t, client, http.MethodPost, url+"/messages", submission(refused, "key-4"))
	require.Equal(t, http.StatusForbidden, status, body)

	// Attached to neither, the call goes through, so both refusals were
	// the attachment and not the recipient.
	policies.attached = nil
	status, body = send(t, client, http.MethodPost, url+"/messages", submission(refused, "key-5"))
	require.Equal(t, http.StatusCreated, status, body)

	assert.Equal(t, []messageCall{
		posted(sender, sibling, "key-2"),
		posted(outsider, refused, "key-3"),
		posted(sender, refused, "key-5"),
	}, messages.calls, "a refused message reached the queue")
}

// TestServer_StoresNothingWhenThePolicyEngineCannotAnswer keeps the
// surface closed on an engine that is not there. The accepted message
// sits beside it, because a surface that stored nothing at all would
// pass the refusal alone.
func TestServer_StoresNothingWhenThePolicyEngineCannotAnswer(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	policies := &engine{}
	url, client := listener(t, &agentService{}, messages, &memberGroups{}, policies, sender)

	status, body := send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-1"))
	require.Equal(t, http.StatusCreated, status, body)

	policies.err = errorx.New(errorx.KindUnavailable, "authz channel is TRANSIENT_FAILURE")
	status, body = send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-2"))
	assert.Equal(t, http.StatusInternalServerError, status, body)
	assert.Equal(t, []messageCall{posted(sender, sibling, "key-1")}, messages.calls,
		"a message nothing decided reached the queue")
}

// TestServer_StoresNothingWhenGroupMembershipCannotBeRead keeps the
// surface closed on the other half of the question. Membership that
// cannot be read is not membership that is empty: the difference is
// every Deny an organization attached to a group, so the message is
// refused rather than decided without them. The accepted message sits
// beside it, because a surface that stored nothing at all would pass
// the refusal alone.
func TestServer_StoresNothingWhenGroupMembershipCannotBeRead(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	groups := &memberGroups{}
	url, client := listener(t, &agentService{}, messages, groups, &engine{}, sender)

	status, body := send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-1"))
	require.Equal(t, http.StatusCreated, status, body)

	groups.err = errorx.New(errorx.KindUnavailable, "postgres is not accepting connections")
	status, body = send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-2"))
	assert.Equal(t, http.StatusInternalServerError, status, body)
	assert.Equal(t, []messageCall{posted(sender, sibling, "key-1")}, messages.calls,
		"a message decided without the sender's groups reached the queue")
}
