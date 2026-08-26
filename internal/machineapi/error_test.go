package machineapi_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machineapi"
)

// engineWords is what the engine's own error says. A caller must not
// read it back on a 5xx, and every status below one carries it.
const engineWords = "authz channel is TRANSIENT_FAILURE"

// TestServer_TellsAnEngineThatIsNotThereFromAPolicyThatRefused is the
// pair an adapter holding a durable outbound queue has to separate: a
// policy refusal is permanent and retrying it is a defect, an engine
// that cannot answer is transient and retrying it is the point. Both
// store nothing, so the status line is what separates them.
//
// The accepted send sits with them as the control, because a 403 beside
// a 503 holds as readily for a surface that refuses everything.
func TestServer_TellsAnEngineThatIsNotThereFromAPolicyThatRefused(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	refused := agentGRN("acme", "deadbeefdeadbeef")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	policies := &engine{attached: map[grn.GRN][]machineapi.Statement{
		sender: {denySending("no-talking", refused.String())},
	}}
	url, client := listener(t, &agentService{}, nil, messages, consenting(), unwatched(t), &memberGroups{}, policies, sender)

	status, body := send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-1"))
	require.Equal(t, http.StatusCreated, status, body)

	status, body = send(t, client, http.MethodPost, url+"/messages", submission(refused, "key-2"))
	require.Equal(t, http.StatusForbidden, status, body)
	assert.JSONEq(t, `{"kind":"permission_denied","message":"sender may not send to this recipient"}`, body)

	policies.err = errorx.New(errorx.KindUnavailable, engineWords)
	status, body = send(t, client, http.MethodPost, url+"/messages", submission(sibling, "key-3"))
	require.Equal(t, http.StatusServiceUnavailable, status, body)
	assert.JSONEq(t, `{"kind":"unavailable","message":"internal error"}`, body)

	assert.Equal(t, []messageCall{posted(sender, sibling, "key-1")}, messages.calls,
		"a message no policy allowed reached the queue")
}

// TestServer_AnswersEachErrorKindWithItsOwnStatus covers the whole
// mapping through the surface that serves it. Every kind is here, so a
// kind that means something specific cannot answer 500 unnoticed, and
// the rows are read against each other: a surface answering one status
// for everything fails all but one of them.
//
// The engine is a fake and each row sets the kind on it, so a row
// proves the mapping renders that kind and never that a real engine
// produces it.
//
// The message is the second half of each row. Internals stay out of a
// 5xx body whatever the kind, and every status below one carries the
// error the engine produced.
func TestServer_AnswersEachErrorKindWithItsOwnStatus(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	policies := &engine{}
	url, client := listener(t, &agentService{}, nil, &messageService{}, consenting(), unwatched(t), &memberGroups{}, policies, sender)

	tests := []struct {
		kind    errorx.Kind
		status  int
		message string
	}{
		{errorx.KindUnknown, http.StatusInternalServerError, "internal error"},
		{errorx.KindInternal, http.StatusInternalServerError, "internal error"},
		{errorx.KindCanceled, http.StatusServiceUnavailable, "internal error"},
		{errorx.KindInvalidArgument, http.StatusBadRequest, engineWords},
		{errorx.KindDeadlineExceeded, http.StatusGatewayTimeout, "internal error"},
		{errorx.KindNotFound, http.StatusNotFound, engineWords},
		{errorx.KindAlreadyExists, http.StatusConflict, engineWords},
		{errorx.KindPermissionDenied, http.StatusForbidden, engineWords},
		{errorx.KindResourceExhausted, http.StatusTooManyRequests, engineWords},
		{errorx.KindFailedPrecondition, http.StatusConflict, engineWords},
		{errorx.KindAborted, http.StatusConflict, engineWords},
		{errorx.KindOutOfRange, http.StatusBadRequest, engineWords},
		{errorx.KindUnimplemented, http.StatusNotImplemented, "internal error"},
		{errorx.KindUnavailable, http.StatusServiceUnavailable, "internal error"},
		{errorx.KindDataLoss, http.StatusInternalServerError, "internal error"},
		{errorx.KindUnauthenticated, http.StatusUnauthorized, engineWords},
	}
	for _, tc := range tests {
		t.Run(tc.kind.String(), func(t *testing.T) {
			policies.err = errorx.New(tc.kind, engineWords)
			status, body := send(t, client, http.MethodPost, url+"/messages", submission(sibling, tc.kind.String()))
			require.Equal(t, tc.status, status, body)
			assert.JSONEq(t, `{"kind":"`+tc.kind.String()+`","message":"`+tc.message+`"}`, body)
		})
	}
}

// TestRecoverPanics_AnswersInsteadOfDroppingTheConnection is this
// surface's half of the test testing.md §Failing asks the change for.
// Without the middleware net/http abandons the response, the client's
// own Do fails, and the require below never reaches a status. Plain
// HTTP is enough for it: the middleware answers before the client
// certificate this listener requires decides anything, and the answer
// is what is under test.
//
// The body is asserted beside the status: a 500 carrying the panic's
// text would leak what a 5xx withholds on every other path.
func TestRecoverPanics_AnswersInsteadOfDroppingTheConnection(t *testing.T) {
	server := httptest.NewServer(machineapi.RecoverPanics(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { panic("handler defect") })))
	t.Cleanup(server.Close)

	resp, err := server.Client().Get(server.URL + "/messages")
	require.NoError(t, err, "the caller read no answer at all")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.JSONEq(t, `{"kind":"internal","message":"internal error"}`, string(body))
}
