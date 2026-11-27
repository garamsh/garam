package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policydecision"
	"github.com/garamsh/garam/platform/internal/policyrelay"
)

// foundPolicies runs the search for q as the owner and returns the
// policies it found, by identifier, and whether the kind was offered.
func foundPolicies(
	t *testing.T, f *fixture, owner *http.Cookie, slug, q string,
) (names map[string]string, offered bool) {
	t.Helper()
	rec := f.do(t, "GET", "/orgs/"+slug+"/search?q="+q, "", owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	names = map[string]string{}
	for _, kind := range decodeBody[apigen.OrgSearch](t, rec).Found {
		if kind.Kind != apigen.FoundPolicy {
			continue
		}
		offered = true
		for _, o := range kind.Objects {
			names[o.Grn] = deref(o.Name)
		}
	}
	return names, offered
}

// TestSearch_FindsPoliciesThroughTheRelay holds that the policy kind
// reads the organization's policies from the policy service and
// matches the needle on a name or an identifier, whatever its case.
// The policy that matches neither is the control: a search that
// returned the whole list would show it.
func TestSearch_FindsPoliciesThroughTheRelay(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	relay := &storedPolicies{listed: []policyrelay.Policy{
		{ID: "p-b", Name: "Closed Office"},
		{ID: "p-a", Name: "readers"},
		{ID: "p-office", Name: "other"},
		{ID: "p-z", Name: "unrelated"},
	}}
	f.server.WithPolicyRelay(relay)
	f.answers(func(q policydecision.Question) (bool, error) { return q.Action == org.ActionPolicyRead, nil })

	got, offered := foundPolicies(t, f, owner, slug, "oFFice")

	require.True(t, offered, "the caller may read policies")
	assert.Equal(t, map[string]string{
		grn.New(slug, "policy", "p-b").String():      "Closed Office",
		grn.New(slug, "policy", "p-office").String(): "other",
	}, got, "a name and an identifier match, whatever the case, and the rest do not")
	require.Len(t, relay.listedFor, 1)
	assert.Equal(t, orgGRNOf(slug), relay.listedFor[0])
}

// TestSearch_PoliciesAreLeftOutWhereTheCallerMayNotRead holds that the
// service's refusal of iam:policy.read leaves the kind out, and that a
// policy service the platform does not reach is an unavailable search
// rather than an empty one. The permitted search is the control for
// the first, and the same query with a relay is the control for the
// second.
func TestSearch_PoliciesAreLeftOutWhereTheCallerMayNotRead(t *testing.T) {
	listed := []policyrelay.Policy{{ID: "p-1", Name: "readers"}}

	t.Run("denied", func(t *testing.T) {
		f := newFixture(t)
		owner := f.sessionCookieFor(t, "owner-sub", "Owner")
		slug := createOrg(t, f, owner, "acme")
		relay := &storedPolicies{listed: listed}
		f.server.WithPolicyRelay(relay)
		f.answers(func(policydecision.Question) (bool, error) { return false, nil })

		_, offered := foundPolicies(t, f, owner, slug, "readers")

		assert.False(t, offered)
		assert.Empty(t, relay.listedFor, "a kind the caller may not list is not read")
	})
	t.Run("permitted", func(t *testing.T) {
		f := newFixture(t)
		owner := f.sessionCookieFor(t, "owner-sub", "Owner")
		slug := createOrg(t, f, owner, "acme")
		f.server.WithPolicyRelay(&storedPolicies{listed: listed})
		f.answers(func(q policydecision.Question) (bool, error) { return q.Action == org.ActionPolicyRead, nil })

		got, offered := foundPolicies(t, f, owner, slug, "readers")

		assert.True(t, offered)
		assert.Len(t, got, 1)
	})
	t.Run("no policy service", func(t *testing.T) {
		f := newFixture(t)
		f.server.WithPolicyRelay(nil)
		owner := f.sessionCookieFor(t, "owner-sub", "Owner")
		slug := createOrg(t, f, owner, "acme")
		f.answers(func(q policydecision.Question) (bool, error) { return q.Action == org.ActionPolicyRead, nil })

		rec := f.do(t, "GET", "/orgs/"+slug+"/search?q=readers", "", owner)

		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})
}
