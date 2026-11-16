package machineapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// submission is the body a sender posts to address recipient.
func submission(recipient grn.GRN, key string) string {
	return `{"recipient":"` + recipient.String() + `","idempotencyKey":"` + key + `","body":"hello"}`
}

// posted is the call the surface makes to the queue for a message it
// accepted.
func posted(sender, recipient grn.GRN, key string) messageCall {
	return messageCall{method: "post", caller: sender, recipient: recipient, key: key, body: "hello", author: message.AuthorSender}
}

// messageCall is one thing the surface asked the message service for.
type messageCall struct {
	method    string
	caller    grn.GRN
	recipient grn.GRN
	claim     message.ClaimID
	id        message.ID
	key       string
	body      string
	author    message.Author
	created   time.Time
}

// storedAt is when the queue stored the message this double answers a
// status for. It is fixed so the wire form is an equality rather than a
// window.
var storedAt = time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)

// messageService is a message.Service recording what the surface passed
// it. It hands back no claim: one is built by the store that read the
// row it holds, so what a claimed message looks like on the wire is the
// round trip in tests/integration, over the real queue.
type messageService struct {
	err   error
	calls []messageCall
	// fences is what each claim was handed to judge its caller by.
	fences []*message.Fence
	// refusals is each send a policy denied, as the surface recorded it.
	refusals []messageCall
	// refuseErr is what recording a refusal answers.
	refuseErr error
}

func (s *messageService) Post(_ context.Context, sender, recipient grn.GRN, key, body string, author message.Author, _ *message.Fence) (*message.Message, error) {
	s.calls = append(s.calls, messageCall{method: "post", caller: sender, recipient: recipient, key: key, body: body, author: author})
	if s.err != nil {
		return nil, s.err
	}
	return message.New(recipient, sender, key, body)
}

func (s *messageService) PostWithinBudget(_ context.Context, sender, recipient grn.GRN, key, body string, author message.Author, created time.Time, _ *message.Fence) (*message.Message, error) {
	s.calls = append(s.calls, messageCall{method: "post within budget", caller: sender, recipient: recipient, key: key, body: body, author: author, created: created})
	if s.err != nil {
		return nil, s.err
	}
	return message.New(recipient, sender, key, body)
}

func (s *messageService) Refuse(_ context.Context, sender, recipient grn.GRN, _ *message.Fence) error {
	s.refusals = append(s.refusals, messageCall{method: "refuse", caller: sender, recipient: recipient})
	return s.refuseErr
}

func (s *messageService) Claim(_ context.Context, caller, recipient grn.GRN, fence *message.Fence, _ message.Signer) (*message.Claim, message.Signed, error) {
	s.calls = append(s.calls, messageCall{method: "claim", caller: caller, recipient: recipient})
	s.fences = append(s.fences, fence)
	if s.err != nil {
		return nil, message.Signed{}, s.err
	}
	return nil, message.Signed{}, message.ErrNoMessageReady
}

func (s *messageService) Renew(_ context.Context, caller, recipient grn.GRN, id message.ClaimID, _ *message.Fence) (*message.Claim, error) {
	s.calls = append(s.calls, messageCall{method: "renew", caller: caller, recipient: recipient, claim: id})
	if s.err != nil {
		return nil, s.err
	}
	return nil, message.ErrStaleClaim
}

func (s *messageService) Complete(_ context.Context, caller, recipient grn.GRN, id message.ClaimID, _ *message.Fence) error {
	s.calls = append(s.calls, messageCall{method: "complete", caller: caller, recipient: recipient, claim: id})
	return s.err
}

func (s *messageService) Status(_ context.Context, caller grn.GRN, id message.ID, _ *message.Fence) (*message.Status, error) {
	s.calls = append(s.calls, messageCall{method: "status", caller: caller, id: id})
	if s.err != nil {
		return nil, s.err
	}
	return &message.Status{ID: id, State: message.StateExhausted, Attempts: 2, CreatedAt: storedAt}, nil
}

// send performs one request carrying a JSON body and returns the status
// and the body that came back.
func send(t *testing.T, client *http.Client, method, url, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	// Every route but the contracts' ignores it; on those it moves the
	// request past the router's own refusal to the handler under test.
	version := operator.TokenAuthorityContractVersion
	switch {
	case executionPath(req.URL.Path):
		version = agent.TokenExecutionContractVersion
	case enrollmentPath(req.URL.Path):
		version = agent.TokenEnrollmentContractVersion
	case standalonePath(req.URL.Path):
		version = agent.CredentialStandaloneContractVersion
	case strings.HasPrefix(req.URL.Path, "/agent-enrollment-approvals"):
		version = agent.AdmissionContractVersion
	case strings.HasSuffix(req.URL.Path, "/outbox-messages"):
		version = message.TokenDeliveryContractVersion
	case strings.HasSuffix(req.URL.Path, "/tokens"):
		version = operator.TokenContractVersion
	}
	req.Header.Set(operator.ContractVersionHeader, version)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(got)
}

// executionPath reports whether path is one of the execution routes,
// which serve execution-fence.v2.
func executionPath(path string) bool {
	for _, suffix := range []string{"/activations", "/deactivation", "/execution/introspection", "/credential-recovery"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

// enrollmentPath reports whether path is the managed creation route,
// which serves managed-enrollment.v2.
func enrollmentPath(path string) bool {
	return strings.HasSuffix(path, "/managed-agents")
}

// standalonePath reports whether path is the standalone enrollment
// route, whose contract is standalone-enrollment.v2.
func standalonePath(path string) bool {
	return path == "/agent-enrollments"
}

// TestServer_PostsUnderTheSenderTheCertificateNames covers what reaches
// the queue and what comes back. The sender is never read off the body:
// a caller could name anyone there, and the certificate names one
// identity.
func TestServer_PostsUnderTheSenderTheCertificateNames(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	recipient := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t), &decider{}, &roster{}, sender)

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
		author:    message.AuthorSender,
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
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t), &decider{}, &roster{}, caller)
	claims := url + "/agents/" + neturl.PathEscape(caller.String()) + "/claims"

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

// refusalBody is what garam stores in answer to a send it will not
// route: the same words whichever question refused it, naming neither
// consent nor the recipient's absence (ADR-0168).
func refusalBody(recipient grn.GRN) string {
	return "garam could not deliver your message to " + recipient.String() +
		": a message to this recipient is not permitted"
}

// refused is the call the surface makes to the queue to store the
// refusal of sender's post to recipient.
func refused(sender, recipient grn.GRN, key string) messageCall {
	return messageCall{
		method:    "post",
		caller:    recipient,
		recipient: sender,
		key:       "crossing-refused:" + sender.String() + ":" + key,
		body:      refusalBody(recipient),
		author:    message.AuthorGaram,
	}
}

// TestServer_AdmitsARecipientNoAgentAnswersToWithoutAskingTheService
// covers the recipients inside the sender's organization the policy
// service is never asked about (ADR-0168): an agent id garam holds no
// row for and a GRN of another type, each stored as ADR-0013's routing
// failure. The agent garam holds is the control: it is asked, so the
// silence is the recipient's and not the surface's.
func TestServer_AdmitsARecipientNoAgentAnswersToWithoutAskingTheService(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	ghost := agentGRN("acme", "deadbeefdeadbeef")
	group := groupGRN("acme", "engineering")
	messages := &messageService{}
	decisions := allowing([2]grn.GRN{sender, sibling})
	held := holding(sibling)
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t), decisions, held, sender)

	for i, recipient := range []grn.GRN{sibling, ghost, group} {
		status, body := send(t, client, http.MethodPost, url+"/messages", submission(recipient, "key-"+strconv.Itoa(i)))
		require.Equal(t, http.StatusCreated, status, body)
		assert.Contains(t, body, `"recipient":"`+recipient.String()+`"`, "the message itself was stored")
	}

	assert.Equal(t, []messageCall{
		posted(sender, sibling, "key-0"),
		posted(sender, ghost, "key-1"),
		posted(sender, group, "key-2"),
	}, messages.calls)
	assert.Equal(t, []policydecision.Question{
		{Subject: sender, Action: message.ActionSend, Resource: sibling},
	}, decisions.asked, "the service was asked about a recipient no agent answers to")
	assert.Equal(t, []grn.GRN{sibling, ghost}, held.asked,
		"a recipient of another type needs no read of the agents")
}

// TestServer_RefusesAcrossTheBoundaryARecipientThatIsNoAgentUnasked
// covers the recipients the service cannot be asked about across the
// boundary (ADR-0168): an organization and a group of another
// organization. Each is answered 201 with garam's refusal stored. The
// allowed agent is the control, so the refusal is the recipient's type
// and not a surface that refuses every crossing.
func TestServer_RefusesAcrossTheBoundaryARecipientThatIsNoAgentUnasked(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	foreign := agentGRN("globex", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	decisions := allowing([2]grn.GRN{sender, foreign})
	held := holding()
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t), decisions, held, sender)

	status, body := send(t, client, http.MethodPost, url+"/messages", submission(foreign, "key-1"))
	require.Equal(t, http.StatusCreated, status, body)
	for i, recipient := range []grn.GRN{organization("globex"), groupGRN("globex", "engineering")} {
		status, body = send(t, client, http.MethodPost, url+"/messages", submission(recipient, "key-"+strconv.Itoa(i+2)))
		require.Equal(t, http.StatusCreated, status, body)
		assert.Contains(t, body, `"recipient":"`+sender.String()+`"`, "the refusal came back to the sender")
	}

	assert.Equal(t, []messageCall{
		posted(sender, foreign, "key-1"),
		refused(sender, organization("globex"), "key-2"),
		refused(sender, groupGRN("globex", "engineering"), "key-3"),
	}, messages.calls)
	assert.Equal(t, []policydecision.Question{
		{Subject: sender, Action: message.ActionSend, Resource: foreign},
	}, decisions.asked, "the service was asked about a recipient it can only refuse")
	assert.Empty(t, held.asked, "the agents of another organization were read")
}

// TestServer_TheServiceDecidesASendAcrossTheBoundary covers every agent
// recipient of another organization: the service's allow is stored,
// its deny is answered 201 with garam's refusal, and an agent garam
// holds no row for is that deny too, in the same words, so the sender
// learns nothing of the other organization (ADR-0168). The agents of
// the other organization are not read.
func TestServer_TheServiceDecidesASendAcrossTheBoundary(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	consenting := agentGRN("globex", "0a1b2c3d4e5f6071")
	refusing := agentGRN("globex", "1122334455667788")
	ghost := agentGRN("initech", "deadbeefdeadbeef")
	messages := &messageService{}
	decisions := allowing([2]grn.GRN{sender, consenting})
	held := holding()
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t), decisions, held, sender)

	for i, recipient := range []grn.GRN{consenting, refusing, ghost} {
		status, body := send(t, client, http.MethodPost, url+"/messages", submission(recipient, "key-"+strconv.Itoa(i)))
		require.Equal(t, http.StatusCreated, status, body)
	}

	assert.Equal(t, []messageCall{
		posted(sender, consenting, "key-0"),
		refused(sender, refusing, "key-1"),
		refused(sender, ghost, "key-2"),
	}, messages.calls, "exactly the allowed pair's message crossed")
	assert.NotContains(t, refusalBody(ghost), "consent")
	assert.Equal(t, []policydecision.Question{
		{Subject: sender, Action: message.ActionSend, Resource: consenting},
		{Subject: sender, Action: message.ActionSend, Resource: refusing},
		{Subject: sender, Action: message.ActionSend, Resource: ghost},
	}, decisions.asked)
	assert.Empty(t, held.asked, "the agents of another organization were read")
}

// TestServer_RecordsASameOrganizationDenialBeforeAnsweringIt holds the
// service's denial of a send inside the organization to the record of
// it: the send reaches the queue as a refused send and nothing else
// (ADR-0106), and is answered 403 (ADR-0168). The admitted send beside
// it records no refusal, so the record is the denial's. A record that
// cannot be written is the answer, so no refusal goes unrecorded.
func TestServer_RecordsASameOrganizationDenialBeforeAnsweringIt(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	denied := agentGRN("acme", "deadbeefdeadbeef")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t),
		allowing([2]grn.GRN{sender, sibling}), holding(denied, sibling), sender)

	status, body := send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-1"))
	require.Equal(t, http.StatusCreated, status, body)
	assert.Empty(t, messages.refusals, "an admitted send was recorded as refused")

	status, body = send(t, client, http.MethodPost, url+"/messages", submission(denied, "key-2"))
	require.Equal(t, http.StatusForbidden, status, body)
	assert.JSONEq(t, `{"kind":"permission_denied","message":"sender may not send to this recipient"}`, body)
	assert.Equal(t, []messageCall{{method: "refuse", caller: sender, recipient: denied}}, messages.refusals)

	messages.refuseErr = errorx.New(errorx.KindUnavailable, "postgres is not accepting connections")
	status, body = send(t, client, http.MethodPost, url+"/messages", submission(denied, "key-3"))
	assert.Equal(t, http.StatusServiceUnavailable, status, body)
	assert.Equal(t, []messageCall{posted(sender, sibling, "key-1")}, messages.calls,
		"a denied message reached the queue")
}

// TestServer_StoresNothingWhenThePolicyServiceCannotDecide keeps the
// surface closed on a service that is not there, for the question
// inside the organization and the one across it, and on a surface
// holding no service at all (ADR-0168). The accepted message sits
// beside each, because a surface that stored nothing at all would pass
// the refusal alone.
func TestServer_StoresNothingWhenThePolicyServiceCannotDecide(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	foreign := agentGRN("globex", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	decisions := allowing([2]grn.GRN{sender, sibling}, [2]grn.GRN{sender, foreign})
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t), decisions, holding(sibling), sender)

	for _, recipient := range []grn.GRN{sibling, foreign} {
		status, body := send(t, client, http.MethodPost, url+"/messages", submission(recipient, "ok-"+recipient.Organization))
		require.Equal(t, http.StatusCreated, status, body)
	}

	decisions.err = errorx.New(errorx.KindUnavailable, "policy channel is TRANSIENT_FAILURE")
	for _, recipient := range []grn.GRN{sibling, foreign} {
		status, body := send(t, client, http.MethodPost, url+"/messages", submission(recipient, "down-"+recipient.Organization))
		assert.Equal(t, http.StatusServiceUnavailable, status, body)
	}

	bare, bareClient := listener(t, &agentService{}, nil, messages, unwatched(t), nil, holding(sibling), sender)
	for _, recipient := range []grn.GRN{sibling, foreign} {
		status, body := send(t, bareClient, http.MethodPost, bare+"/messages", submission(recipient, "bare-"+recipient.Organization))
		assert.Equal(t, http.StatusServiceUnavailable, status, body)
	}

	assert.Equal(t, []messageCall{
		posted(sender, sibling, "ok-acme"),
		posted(sender, foreign, "ok-globex"),
	}, messages.calls, "a message nothing decided reached the queue")
}

// TestServer_StoresNothingWhenTheSendersAgentsCannotBeRead keeps the
// surface closed on the question that chooses the question. An agent
// that cannot be read is not an agent that is absent: absent is
// admitted as a routing failure, and an unreadable one is not known to
// be absent. The accepted message sits beside it.
func TestServer_StoresNothingWhenTheSendersAgentsCannotBeRead(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	held := holding(sibling)
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t),
		allowing([2]grn.GRN{sender, sibling}), held, sender)

	status, body := send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-1"))
	require.Equal(t, http.StatusCreated, status, body)

	held.err = errorx.New(errorx.KindUnavailable, "postgres is not accepting connections")
	status, body = send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-2"))
	assert.Equal(t, http.StatusServiceUnavailable, status, body)
	assert.Equal(t, []messageCall{posted(sender, sibling, "key-1")}, messages.calls,
		"a message decided without reading the recipient reached the queue")
}

// TestServer_ReadsAMessageStatusUnderTheSenderTheCertificateNames pins
// what the surface does with a status read: the sender is the identity
// the connection proved and the path carries the identifier alone, so
// nothing a caller writes can name another sender's message. The answer
// carries no body — a completed message no longer has one (ADR-0034) —
// so the field set is asserted rather than the one field's absence.
func TestServer_ReadsAMessageStatusUnderTheSenderTheCertificateNames(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	messages := &messageService{}
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t), &decider{}, &roster{}, sender)

	status, body := send(t, client, http.MethodGet, url+"/messages/0a1b2c3d", "")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []messageCall{{method: "status", caller: sender, id: "0a1b2c3d"}}, messages.calls,
		"the read named a sender the certificate had not")

	var answered map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &answered))
	assert.Equal(t, map[string]any{
		"id":        "0a1b2c3d",
		"state":     string(message.StateExhausted),
		"attempts":  float64(2),
		"createdAt": storedAt.Format(time.RFC3339),
	}, answered)
}

// TestServer_AnswersAMessageItHasNoneOfAsNotFound covers the refusal
// the bound produces: a message the caller did not send, one that never
// existed and one the sweep deleted reach the surface as the same
// domain error and leave as the same status. The read the queue answers
// is the control beside it — the 404 is the error travelling rather
// than a route that refuses whoever asks.
func TestServer_AnswersAMessageItHasNoneOfAsNotFound(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	messages := &messageService{}
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t), &decider{}, &roster{}, sender)

	status, body := send(t, client, http.MethodGet, url+"/messages/0a1b2c3d", "")
	require.Equal(t, http.StatusOK, status, body)

	messages.err = message.ErrNoSuchMessage
	status, body = send(t, client, http.MethodGet, url+"/messages/0a1b2c3d", "")
	assert.Equal(t, http.StatusNotFound, status)
	assert.Contains(t, body, `"kind":"`+errorx.KindNotFound.String()+`"`)
}

// TestServer_ReadsAnEmptyPollFromTheKindNotTheSentinel holds the answer
// to a poll that found nothing against the shape the queue's answer
// reaches this surface in. `stack-go.md` §4 does not admit errors.Is
// beside errorx, and the reason is here: a sentinel matched by identity
// stops matching the moment anything wraps it into a kind-carrying copy
// — and an empty poll that stops reading as one is answered 404, which
// the adapter treats as a failure rather than as the loop's ordinary
// turn.
//
// The control is a failure of another kind, which must not become an
// empty answer: without it a 204 would hold as readily for a surface
// that answers every failure that way.
func TestServer_ReadsAnEmptyPollFromTheKindNotTheSentinel(t *testing.T) {
	caller := agentGRN("acme", "0a1b2c3d4e5f6071")
	claims := "/agents/" + neturl.PathEscape(caller.String()) + "/claims"

	empty := &messageService{err: errorx.New(errorx.KindNotFound, "no message ready")}
	url, client := listener(t, nil, nil, empty, nil, nil, nil, caller)
	status, body := send(t, client, http.MethodPost, url+claims, "")
	assert.Equal(t, http.StatusNoContent, status)
	assert.Empty(t, body)

	unreachable := &messageService{err: errorx.New(errorx.KindUnavailable, "queue unreachable")}
	url, client = listener(t, nil, nil, unreachable, nil, nil, nil, caller)
	status, body = send(t, client, http.MethodPost, url+claims, "")
	assert.Equal(t, http.StatusServiceUnavailable, status, "the control: another kind is not an empty poll")
	assert.Contains(t, body, `"kind":"unavailable"`)
}

// outboxSend posts body to the durable submission route under version,
// sending no version header at all where version is empty, and returns
// the status, the version the answer carries and the body.
func outboxSend(t *testing.T, client *http.Client, url, version, body string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/outbox-messages", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if version != "" {
		req.Header.Set(operator.ContractVersionHeader, version)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get(operator.ContractVersionHeader), string(got)
}

// outboxEntry is the body a sender posts for one outbox entry.
func outboxEntry(recipient grn.GRN, key, createdAt string) string {
	return `{"recipient":"` + recipient.String() + `","idempotencyKey":"` + key +
		`","body":"hello","createdAt":"` + createdAt + `"}`
}

// TestServer_RefusesAnOutboxMessageBeforeTheQueueWithoutItsContractOrCreationInstant
// holds the refusals the surface makes itself (ADR-0083): a request
// carrying no contract version, or another one, and an entry whose
// creation instant is missing or does not parse never reach the queue.
// The entry the queue is asked for is the control, sent first under
// the same version and recipient: it reaches the queue with the
// instant it named, so each refusal is its own field rather than a
// route that refuses everything. Every answer names the contract it
// follows.
func TestServer_RefusesAnOutboxMessageBeforeTheQueueWithoutItsContractOrCreationInstant(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	recipient := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t), &decider{}, &roster{}, sender)
	const v2 = message.TokenDeliveryContractVersion

	status, version, body := outboxSend(t, client, url, v2, outboxEntry(recipient, "entry-1", "2026-10-04T18:07:52.1Z"))
	require.Equal(t, http.StatusCreated, status, body)
	assert.Equal(t, v2, version)
	assert.Contains(t, body, `"sender":"`+sender.String()+`"`)
	assert.Equal(t, []messageCall{{
		method: "post within budget", caller: sender, recipient: recipient, key: "entry-1", body: "hello",
		author: message.AuthorSender, created: time.Date(2026, time.October, 4, 18, 7, 52, 100000000, time.UTC),
	}}, messages.calls)

	tests := []struct {
		name, version, body, names string
	}{
		{name: "no contract version", body: outboxEntry(recipient, "entry-2", "2026-10-04T18:07:52Z"), names: v2},
		{name: "another contract version", version: "durable-delivery.v1", body: outboxEntry(recipient, "entry-2", "2026-10-04T18:07:52Z"), names: v2},
		{name: "no creation instant", version: v2, body: `{"recipient":"` + recipient.String() + `","idempotencyKey":"entry-2","body":"hello"}`, names: "createdAt"},
		{name: "a creation instant that is not RFC 3339", version: v2, body: outboxEntry(recipient, "entry-2", "2026-10-04 18:07:52"), names: "createdAt"},
		{name: "a creation instant with no zone", version: v2, body: outboxEntry(recipient, "entry-2", "2026-10-04T18:07:52"), names: "createdAt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := len(messages.calls)
			status, version, body := outboxSend(t, client, url, tt.version, tt.body)
			assert.Equal(t, http.StatusBadRequest, status, body)
			assert.Equal(t, v2, version, "the refusal names the contract the route serves")
			assert.Contains(t, body, `"kind":"invalid_argument"`)
			assert.Contains(t, body, tt.names)
			assert.Len(t, messages.calls, before, "a refused entry reached the queue")
		})
	}
}

// TestServer_AnswersEachBudgetRefusalWithItsOwnStatus pins the table
// the adapter branches on (ADR-0083). An expired entry is 410 because
// the sentinel is what picks it: the control is a failure of the same
// kind that is not the sentinel, which keeps the shared 504, so a
// timeout never reads as expiry.
func TestServer_AnswersEachBudgetRefusalWithItsOwnStatus(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	recipient := agentGRN("acme", "0a1b2c3d4e5f6071")

	tests := []struct {
		name   string
		err    error
		status int
		kind   errorx.Kind
	}{
		{name: "expired", err: message.ErrSubmissionExpired, status: http.StatusGone, kind: errorx.KindDeadlineExceeded},
		{name: "created later than garam's clock", err: message.ErrCreatedLater, status: http.StatusBadRequest, kind: errorx.KindOutOfRange},
		{name: "a key holding another message", err: message.ErrKeyHoldsAnotherMessage, status: http.StatusConflict, kind: errorx.KindAlreadyExists},
		{
			name: "control: a timeout of the expiry's kind", err: errorx.New(errorx.KindDeadlineExceeded, "query timed out"),
			status: http.StatusGatewayTimeout, kind: errorx.KindDeadlineExceeded,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messages := &messageService{err: tt.err}
			url, client := listener(t, &agentService{}, nil, messages, unwatched(t), &decider{}, &roster{}, sender)
			status, version, body := outboxSend(t, client, url, message.TokenDeliveryContractVersion, outboxEntry(recipient, "entry-1", "2026-10-04T18:07:52Z"))
			assert.Equal(t, tt.status, status, body)
			assert.Equal(t, message.TokenDeliveryContractVersion, version)
			if tt.status < http.StatusInternalServerError {
				assert.Contains(t, body, `"kind":"`+tt.kind.String()+`"`)
			}
		})
	}
}

// TestServer_RefusesAnOutboxMessageAcrossTheBoundaryUnderItsBudget
// holds that the cross-organization refusal of an outbox entry is
// written through the same budgeted write, carrying the entry's
// creation instant, so an expired entry stores no refusal either. The
// allowed pair is the control: its entry is stored as submitted.
func TestServer_RefusesAnOutboxMessageAcrossTheBoundaryUnderItsBudget(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	foreign := agentGRN("globex", "0a1b2c3d4e5f6071")
	stranger := agentGRN("initech", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	url, client := listener(t, &agentService{}, nil, messages, unwatched(t),
		allowing([2]grn.GRN{sender, foreign}), holding(), sender)
	created := time.Date(2026, time.October, 4, 18, 7, 52, 0, time.UTC)

	status, _, body := outboxSend(t, client, url, message.TokenDeliveryContractVersion, outboxEntry(foreign, "entry-1", created.Format(time.RFC3339)))
	require.Equal(t, http.StatusCreated, status, body)
	status, _, body = outboxSend(t, client, url, message.TokenDeliveryContractVersion, outboxEntry(stranger, "entry-2", created.Format(time.RFC3339)))
	require.Equal(t, http.StatusCreated, status, body)
	assert.Contains(t, body, `"recipient":"`+sender.String()+`"`)
	assert.Contains(t, body, `"sender":"`+stranger.String()+`"`)

	assert.Equal(t, []messageCall{
		{method: "post within budget", caller: sender, recipient: foreign, key: "entry-1", body: "hello", author: message.AuthorSender, created: created},
		{
			method: "post within budget", caller: stranger, recipient: sender,
			key:     "crossing-refused:" + sender.String() + ":entry-2",
			body:    refusalBody(stranger),
			author:  message.AuthorGaram,
			created: created,
		},
	}, messages.calls)
}

// unopenableKeyring is a keyring whose key does not open, as under a
// key-encryption key other than the one it was sealed with.
type unopenableKeyring struct{ stubKeyring }

func (unopenableKeyring) Signer(context.Context) (message.Signer, error) {
	return nil, errorx.New(errorx.KindInternal, "message: open signing key k-1")
}

// TestServer_AClaimGaramCannotSignSpendsNoAttempt holds the order the
// claim route keeps: the signing key is opened before the queue is
// asked, so a key that does not open answers 500 with nothing claimed,
// rather than spending an attempt on an answer the caller never gets
// (ADR-0101). The same request against a key that opens is the control:
// it reaches the queue.
func TestServer_AClaimGaramCannotSignSpendsNoAttempt(t *testing.T) {
	caller := agentGRN("acme", "0a1b2c3d4e5f6071")
	claims := "/agents/" + neturl.PathEscape(caller.String()) + "/claims"

	unsigned := &messageService{}
	url, client := serveTLS(t, machineapi.NewServer(&agentService{}, nil, unsigned, unwatched(t),
		nil, nil, nil, nil, nil, nil, unopenableKeyring{}), caller)
	status, body := send(t, client, http.MethodPost, url+claims, "")
	assert.Equal(t, http.StatusInternalServerError, status, body)
	assert.Empty(t, unsigned.calls, "the queue was asked for a claim garam could not sign")

	signed := &messageService{}
	url, client = serveTLS(t, machineapi.NewServer(&agentService{}, nil, signed, unwatched(t),
		nil, nil, nil, nil, nil, nil, stubKeyring{}), caller)
	status, body = send(t, client, http.MethodPost, url+claims, "")
	assert.Equal(t, http.StatusNoContent, status, body)
	assert.Equal(t, []messageCall{{method: "claim", caller: caller, recipient: caller}}, signed.calls)
}
