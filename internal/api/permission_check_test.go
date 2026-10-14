package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// checksBody is a request asking about the given pairs.
func checksBody(t *testing.T, checks ...apigen.PermissionCheck) string {
	t.Helper()
	raw, err := json.Marshal(apigen.PermissionCheckRequest{Checks: checks})
	require.NoError(t, err)
	return string(raw)
}

// TestCheckPermissions_AnswersEachPairInOrder asks two actions the
// engine decides differently and reads both answers back in the order
// asked, each carrying the pair it answers.
func TestCheckPermissions_AnswersEachPairInOrder(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit(permission{grn.New("root", "default", user.ServiceType, "owner-sub"), org.ActionOperatorRead})

	rec := f.do(t, "POST", "/orgs/"+slug+"/permission-checks", checksBody(t,
		apigen.PermissionCheck{Action: org.ActionOperatorRegister},
		apigen.PermissionCheck{Action: org.ActionOperatorRead},
	), owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []apigen.PermissionCheckResult{
		{Action: org.ActionOperatorRegister, Allowed: false},
		{Action: org.ActionOperatorRead, Allowed: true},
	}, decodeBody[apigen.PermissionCheckAnswer](t, rec).Results)
}

// TestCheckPermissions_DeniesAnActionGaramDoesNotDefine holds that a
// name no operation checks is answered false even where the engine
// would allow it, as a role's `org:*` would. The engine here allows
// both names; the defined one is the control, answered true, so what
// denies the other is the vocabulary rather than the engine.
func TestCheckPermissions_DeniesAnActionGaramDoesNotDefine(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	subject := grn.New("root", "default", user.ServiceType, "owner-sub")
	f.permit(permission{subject, "org:frobnicate"}, permission{subject, org.ActionOrgUpdate})

	rec := f.do(t, "POST", "/orgs/"+slug+"/permission-checks", checksBody(t,
		apigen.PermissionCheck{Action: "org:frobnicate"},
		apigen.PermissionCheck{Action: org.ActionOrgUpdate},
	), owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	results := decodeBody[apigen.PermissionCheckAnswer](t, rec).Results
	require.Len(t, results, 2)
	assert.False(t, results[0].Allowed, "an undefined action is denied")
	assert.True(t, results[1].Allowed, "a defined action the engine allows is allowed")
}

// TestCheckPermissions_AsksAboutTheNamedResource holds that a resource
// is checked as the GRN its own operation checks: the engine here
// allows one group and not another, and the same action on each is
// answered apart.
func TestCheckPermissions_AsksAboutTheNamedResource(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	allowedGroup := grn.New(slug, grn.DefaultProject, org.ServiceTypeGroup, "eng")
	f.evaluator.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, in api.EvalInput) (bool, error) {
			return in.Object == allowedGroup, nil
		})

	rec := f.do(t, "POST", "/orgs/"+slug+"/permission-checks", checksBody(t,
		apigen.PermissionCheck{Action: org.ActionGroupUpdate,
			Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeGroup, Id: "eng"}},
		apigen.PermissionCheck{Action: org.ActionGroupUpdate,
			Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypeGroup, Id: "ops"}},
	), owner)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	results := decodeBody[apigen.PermissionCheckAnswer](t, rec).Results
	require.Len(t, results, 2)
	assert.True(t, results[0].Allowed)
	assert.Equal(t, "eng", results[0].Resource.Id, "an answer carries the resource it answers")
	assert.False(t, results[1].Allowed)
}

// TestCheckPermissions_BoundsTheRequest accepts fifty pairs and
// refuses fifty-one and none. Fifty is the control: the same pairs,
// one fewer, are answered, so the refusal is the bound's.
func TestCheckPermissions_BoundsTheRequest(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit()
	pairs := func(n int) []apigen.PermissionCheck {
		out := make([]apigen.PermissionCheck, n)
		for i := range out {
			out[i] = apigen.PermissionCheck{Action: org.ActionOrgRead}
		}
		return out
	}
	path := "/orgs/" + slug + "/permission-checks"

	rec := f.do(t, "POST", path, checksBody(t, pairs(50)...), owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, decodeBody[apigen.PermissionCheckAnswer](t, rec).Results, 50)

	rec = f.do(t, "POST", path, checksBody(t, pairs(51)...), owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	rec = f.do(t, "POST", path, `{"checks":[]}`, owner)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestCheckPermissions_RefusesAMalformedResource refuses a resource
// type the operation does not name and an identifier no GRN can hold.
// The well-formed pair is the control.
func TestCheckPermissions_RefusesAMalformedResource(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.permit()
	path := "/orgs/" + slug + "/permission-checks"

	rec := f.do(t, "POST", path, `{"checks":[{"action":"group:read","resource":{"type":"group","id":"eng"}}]}`, owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	for _, body := range []string{
		`{"checks":[{"action":"group:read","resource":{"type":"team","id":"eng"}}]}`,
		`{"checks":[{"action":"group:read","resource":{"type":"group","id":"*"}}]}`,
	} {
		rec = f.do(t, "POST", path, body, owner)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}

// TestCheckPermissions_RefusesANonMember holds that membership is the
// precondition here as on every operation inside an org. The member
// asking the same question is the control.
func TestCheckPermissions_RefusesANonMember(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	outsider := f.sessionCookieFor(t, "outsider-sub", "Outsider")
	slug := createOrg(t, f, owner, "acme")
	f.permit()
	body := checksBody(t, apigen.PermissionCheck{Action: org.ActionOrgRead})
	path := "/orgs/" + slug + "/permission-checks"

	assert.Equal(t, http.StatusOK, f.do(t, "POST", path, body, owner).Code)
	rec := f.do(t, "POST", path, body, outsider)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.False(t, strings.Contains(rec.Body.String(), "results"))
}
