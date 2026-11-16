package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// What a role grants, and where, is the policy service's decision and
// is shown against the real service in tests/integration
// (policy_enforcement_test.go). What this listener owns, and what is
// shown here, is the question it asks and what it does with the answer.

// TestEnforcement_AsksTheServiceWhatTheHandlerActsOn holds the question
// a handler's own enforcement asks: the signed-in user, the action of
// the operation and the resource it addresses, asked as an attempt
// (not a probe) and past no cache (not current), since the delivered
// state suffices for a decision that acts (ADR-0171).
//
// The allowed and the refused answer each go through the same route,
// so a handler that ignored the service would show in one of the two.
func TestEnforcement_AsksTheServiceWhatTheHandlerActsOn(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	alice := f.sessionCookieFor(t, "alice-sub", "Alice")
	slug := createOrg(t, f, owner, "acme")
	aliceGRN := userGRNOf("alice-sub")
	require.NoError(t, f.orgs.AddMember(testCtx(t), org.NewMembership(orgGRNOf(slug), aliceGRN, org.RoleMember)))
	eng := grn.New(slug, org.ServiceTypeGroup, "eng")
	require.NoError(t, f.orgs.CreateGroup(testCtx(t), org.NewGroup(eng, "eng", orgGRNOf(slug))))

	var asked []policydecision.Question
	allow := true
	f.answers(func(q policydecision.Question) (bool, error) {
		asked = append(asked, q)
		return allow, nil
	})
	want := policydecision.Question{Subject: aliceGRN, Action: org.ActionGroupUpdate, Resource: eng}

	rec := f.do(t, "PATCH", "/orgs/"+slug+"/groups/eng", `{"name":"renamed"}`, alice)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, []policydecision.Question{want}, asked)

	allow, asked = false, nil
	rec = f.do(t, "PATCH", "/orgs/"+slug+"/groups/eng", `{"name":"again"}`, alice)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Equal(t, []policydecision.Question{want}, asked)
}

// TestEnforcement_AServiceThatFailsRefusesAsUnavailable holds that an
// error of the service is a 503 on enforcement, never an allow and
// never the 403 of a forbid (ADR-0168 item 7): the retry a caller makes
// of the one is wrong for the other. The forbid and the allow beside it
// are the controls, so the 503 is the error and not the route.
func TestEnforcement_AServiceThatFailsRefusesAsUnavailable(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	eng := grn.New(slug, org.ServiceTypeGroup, "eng")
	require.NoError(t, f.orgs.CreateGroup(testCtx(t), org.NewGroup(eng, "eng", orgGRNOf(slug))))

	var answer error
	allow := true
	f.answers(func(policydecision.Question) (bool, error) { return allow, answer })
	update := func() int {
		return f.do(t, "PATCH", "/orgs/"+slug+"/groups/eng", `{"name":"renamed"}`, owner).Code
	}

	assert.Equal(t, http.StatusOK, update(), "the service allows")
	allow = false
	assert.Equal(t, http.StatusForbidden, update(), "the service forbids")
	allow, answer = true, errorx.New(errorx.KindUnavailable, "policy service unreachable")
	rec := f.do(t, "PATCH", "/orgs/"+slug+"/groups/eng", `{"name":"again"}`, owner)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, "an allow beside the error is not read")
	assert.Equal(t, "unavailable", decodeBody[apigen.Error](t, rec).Kind)
	assert.NotContains(t, rec.Body.String(), "unreachable", "the error's text is not the caller's")
}

// TestEnforcement_AServiceThatFailsRefusesAnyErrorKindAsUnavailable
// holds that the kind of the error does not choose the status: an error
// that names a denial is no more a forbid than one that names nothing.
func TestEnforcement_AServiceThatFailsRefusesAnyErrorKindAsUnavailable(t *testing.T) {
	for name, err := range map[string]error{
		"a denial kind":   errorx.New(errorx.KindPermissionDenied, "the service said no"),
		"an internal one": errorx.New(errorx.KindInternal, "decode"),
		"a plain error":   assert.AnError,
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			owner := f.sessionCookieFor(t, "owner-sub", "Owner")
			slug := createOrg(t, f, owner, "acme")
			f.answers(func(policydecision.Question) (bool, error) { return false, err })

			rec := f.do(t, "GET", "/orgs/"+slug, "", owner)

			assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		})
	}
}

// TestEnforcement_MalformedSlugStopsBeforeTheService pins where a path
// segment stops being inert: it is validated before the service is
// asked, so '*' and '?' never reach a question. The well-formed slug is
// the control — it is answered on the same route, so what the rows
// below refuse is the segment rather than the request.
func TestEnforcement_MalformedSlugStopsBeforeTheService(t *testing.T) {
	t.Run("a well-formed slug is answered", func(t *testing.T) {
		f := newFixture(t)
		owner := f.sessionCookieFor(t, "owner-sub", "Owner")
		slug := createOrg(t, f, owner, "acme")
		f.permit(permission{userGRNOf("owner-sub"), org.ActionOrgRead})

		assert.Equal(t, http.StatusOK, f.do(t, "GET", "/orgs/"+slug, "", owner).Code)
	})

	// The wildcards are percent-encoded: a raw '?' opens the query
	// string instead of reaching the path segment.
	malformed := map[string]string{
		"a wildcard":                   "%2A",
		"a single-character wildcard":  "ac%3Fe",
		"an uppercase slug":            "ACME",
		"a slug past the length bound": strings.Repeat("a", 65),
	}
	for name, slug := range malformed {
		t.Run(name+" is refused", func(t *testing.T) {
			// The service carries no expectations, so a request that
			// asks it fails the test.
			f := newFixture(t)
			owner := f.sessionCookieFor(t, "owner-sub", "Owner")

			assert.Equal(t, http.StatusBadRequest, f.do(t, "GET", "/orgs/"+slug, "", owner).Code)
		})
	}
}

// TestEnforcement_ANonMemberIsRefusedWithoutAskingTheService holds that
// membership stays the precondition the platform checks itself: the
// service is not asked about a user who is not in the org, and the
// member beside is the control.
func TestEnforcement_ANonMemberIsRefusedWithoutAskingTheService(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	stranger := f.sessionCookieFor(t, "stranger-sub", "Stranger")
	slug := createOrg(t, f, owner, "acme")
	asked := map[grn.GRN]bool{}
	f.answers(func(q policydecision.Question) (bool, error) {
		asked[q.Subject] = true
		return true, nil
	})

	assert.Equal(t, http.StatusOK, f.do(t, "GET", "/orgs/"+slug, "", owner).Code)
	assert.Equal(t, http.StatusForbidden, f.do(t, "GET", "/orgs/"+slug, "", stranger).Code)

	assert.True(t, asked[userGRNOf("owner-sub")], "the member is asked about")
	assert.False(t, asked[userGRNOf("stranger-sub")], "the stranger is not")
}
