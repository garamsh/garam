package machineapi_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/pgxerr"
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

// TestServer_AConflictCarriesNoneOfTheDriversText is this surface's
// half of the disclosure regression. The table above is its control:
// the same kinds, below 500, carrying the message where this project
// wrote every part of it. Here the chain reaches Postgres instead, and
// what the caller reads must name nothing of the schema.
func TestServer_AConflictCarriesNoneOfTheDriversText(t *testing.T) {
	sender := agentGRN("acme", "9f8e7d6c5b4a3021")
	sibling := agentGRN("acme", "0a1b2c3d4e5f6071")
	policies := &engine{}
	url, client := listener(t, &agentService{}, nil, &messageService{}, consenting(), unwatched(t), &memberGroups{}, policies, sender)

	policies.err = pgxerr.Classify(&pgconn.PgError{
		Severity:       "ERROR",
		Code:           "23505",
		Message:        `duplicate key value violates unique constraint "messages_sender_grn_idempotency_key_key"`,
		ConstraintName: "messages_sender_grn_idempotency_key_key",
		TableName:      "messages",
	}, "enqueue", nil, nil)

	status, body := send(t, client, http.MethodPost, url+"/messages", submission(sibling, "duplicate"))
	require.Equal(t, http.StatusConflict, status, body)
	// The control for the search: the kind still reaches the caller, so
	// what is absent is the driver's text and not the body.
	require.Contains(t, body, `"already_exists"`)
	for _, leaked := range []string{"messages_sender_grn_idempotency_key_key", "messages", "SQLSTATE", "23505", "duplicate key", "constraint"} {
		assert.NotContains(t, body, leaked,
			"the response names something only Postgres and the schema know")
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

// TestBindingError_AnswersTheErrorTheSpecificationDeclares covers what
// a caller reads when the generated router refuses a parameter before
// any handler runs. The router's own default writes the binding error's
// text as text/plain, and that text quotes the value it could not read;
// this surface owes the Error `api/machine.yaml` declares and owes the
// caller none of its own submission back.
//
// The kind and the message are asserted beside the absence, because a
// handler writing nothing at all would carry no submitted value either.
func TestBindingError_AnswersTheErrorTheSpecificationDeclares(t *testing.T) {
	submitted := `grn:acme:default:agent:"; drop table messages`
	recorder := httptest.NewRecorder()

	machineapi.BindingError(recorder,
		httptest.NewRequest(http.MethodPost, "/agents/x/claims", nil),
		&machinegen.InvalidParamFormatError{
			ParamName: "agent",
			Err:       errors.New("parsing " + submitted + ": invalid syntax"),
		})

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"kind":"invalid_argument","message":"a request parameter could not be read"}`,
		recorder.Body.String())
	assert.NotContains(t, recorder.Body.String(), "drop table",
		"nothing the caller submitted travels back")
}
