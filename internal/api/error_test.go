package api_test

import (
	"context"
	"net/http"
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
