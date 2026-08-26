package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// TestServer_TellsAnEngineThatIsNotThereFromADecisionThatRefused keeps
// the two refusals apart on this listener as well. A denied decision is
// permanent and an engine that cannot answer is transient, and a caller
// that retries reads which it got off the status line.
//
// The answered read sits with them as the control, because a 403 beside
// a 503 holds as readily for a surface that refuses everything.
func TestServer_TellsAnEngineThatIsNotThereFromADecisionThatRefused(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	ownerGRN := grn.New("root", "default", user.ServiceType, "owner-sub")

	// The engine answers as it does in every other test until it stops
	// being there, which is the third call below.
	var unreachable error
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in api.EvalInput) (bool, error) {
			if unreachable != nil {
				return false, unreachable
			}
			return in.Subject == ownerGRN && in.Action == org.ActionOrgRead, nil
		})
	slug := createOrg(t, f, owner, "acme")

	rec := f.do(t, "GET", "/orgs/"+slug, "", owner)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = f.do(t, "PATCH", "/orgs/"+slug, `{"name":"acme2"}`, owner)
	require.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, apigen.Error{Kind: "permission_denied", Message: "insufficient role"},
		decodeBody[apigen.Error](t, rec))

	unreachable = errorx.New(errorx.KindUnavailable, "authz channel is TRANSIENT_FAILURE")
	rec = f.do(t, "GET", "/orgs/"+slug, "", owner)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, apigen.Error{Kind: "unavailable", Message: "internal error"},
		decodeBody[apigen.Error](t, rec))
}

// TestRecoverPanics_AnswersInsteadOfDroppingTheConnection is the test
// testing.md §Failing asks this change for. Without the middleware
// net/http abandons the response, the client's own Do fails, and the
// require below never reaches a status — so the request runs through a
// real server and a real client rather than a recorder, which would
// surface the panic on the test's own goroutine instead.
//
// The body is asserted beside the status: a 500 carrying the panic's
// text would leak what a 5xx withholds on every other path.
func TestRecoverPanics_AnswersInsteadOfDroppingTheConnection(t *testing.T) {
	server := httptest.NewServer(api.RecoverPanics(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) { panic("handler defect") })))
	t.Cleanup(server.Close)

	resp, err := server.Client().Get(server.URL + "/orgs")
	require.NoError(t, err, "the client read no answer at all")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.JSONEq(t, `{"kind":"internal","message":"internal error"}`, string(body))
}

// marker is a value no message should ever repeat, distinctive enough
// that finding it in a body cannot be a coincidence.
const marker = "MARKER-abc"

// TestBindingError_NamesTheParameterAndNotTheValue is the refusal a
// parameter the router cannot bind gets: the body api/openapi.yaml
// declares, carrying a message built from the parameter name rather
// than from the binding error's own text.
//
// The absent marker is the assertion that matters. A handler that
// carries the binding error's text into the JSON message answers the
// declared schema and repeats the submitted value at once, and every
// other assertion here passes over that.
//
// The parseable value is the control. A 400 holds as readily for a
// route that does not exist or a body that was rejected later, and
// without it nothing here says the refusal tracks the binding.
func TestBindingError_NamesTheParameterAndNotTheValue(t *testing.T) {
	f := newFixture(t)

	rec := f.do(t, "GET", "/orgs?limit="+marker, "")

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, apigen.Error{
		Kind:    "invalid_argument",
		Message: "parameter limit is not in the required format",
	}, decodeBody[apigen.Error](t, rec))
	assert.NotContains(t, rec.Body.String(), marker,
		"the body repeats what the caller submitted")

	control := f.do(t, "GET", "/orgs?limit=7", "")
	assert.Equal(t, http.StatusUnauthorized, control.Code,
		"a value that parses binds, and the request reaches the session check")
}

// TestBindingError_NamesAMissingParameter covers the second refusal the
// generated router builds. It carries no submitted value to withhold —
// the parameter is the one that was not sent — so what it holds is that
// the declared body reaches this path too.
func TestBindingError_NamesAMissingParameter(t *testing.T) {
	f := newFixture(t)

	rec := f.do(t, "GET", "/auth/callback/google?state="+marker, "")

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, apigen.Error{
		Kind:    "invalid_argument",
		Message: "query parameter code is required",
	}, decodeBody[apigen.Error](t, rec))
	assert.NotContains(t, rec.Body.String(), marker)
}
