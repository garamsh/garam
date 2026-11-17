package api_test

import (
	"context"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policydecision"
	"github.com/garamsh/garam/platform/internal/policyrelay"
)

// storedPolicies is the policy relay as far as an explanation reads it:
// the policies it holds by identifier, and how often it was read. Any
// other method is not one the explanation calls and panics on the nil
// interface.
type storedPolicies struct {
	policyrelay.Service
	held  map[string]policyrelay.Detail
	reads atomic.Int32
	// listed is what List answers, and listedFor the organizations it
	// was asked of.
	listed    []policyrelay.Policy
	listedFor []grn.GRN
}

func (s *storedPolicies) List(_ context.Context, _, orgGRN grn.GRN) ([]policyrelay.Policy, error) {
	s.listedFor = append(s.listedFor, orgGRN)
	return s.listed, nil
}

func (s *storedPolicies) Get(_ context.Context, w policyrelay.Write) (policyrelay.Detail, error) {
	s.reads.Add(1)
	detail, ok := s.held[w.ID]
	if !ok {
		return policyrelay.Detail{}, errorx.New(errorx.KindNotFound, "policy not found")
	}
	return detail, nil
}

// stored is a policy of kind holding one default version.
func stored(kind policyrelay.Kind, name string, body policyrelay.Body) policyrelay.Detail {
	body.Kind = kind
	return policyrelay.Detail{
		Policy:   policyrelay.Policy{Name: name, Kind: kind, DefaultVersion: 1},
		Versions: []policyrelay.Version{{Number: 1, Body: body}},
	}
}

// explains stubs the policy service to decide the pair as the test says
// and to answer the read of a policy as mayRead says, and returns the
// explanation of action on the resource the query names.
func (f *fixture) explains(
	t *testing.T, owner *http.Cookie, slug, query string, allowed, mayRead bool, reasons ...policydecision.Reason,
) *httpResult {
	t.Helper()
	set := policydecision.Reasons{}
	for _, reason := range reasons {
		set[reason] = struct{}{}
	}
	f.decisions.EXPECT().Evaluate(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, q policydecision.Question) (policydecision.Decision, error) {
			if q.Action == org.ActionPolicyRead {
				return policydecision.Decision{Allowed: mayRead}, nil
			}
			return policydecision.Decision{Allowed: allowed, Reasons: set}, nil
		})
	rec := f.do(t, "GET", "/orgs/"+slug+"/permission-explanation?"+query, "", owner)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return &httpResult{decodeBody[apigen.PermissionExplanation](t, rec)}
}

// httpResult is a decoded explanation.
type httpResult struct{ apigen.PermissionExplanation }

// one is the only ground the explanation names.
func (r *httpResult) one(t *testing.T) apigen.ExplainedGround {
	t.Helper()
	require.Len(t, r.Grounds, 1)
	return r.Grounds[0]
}

func queryFor(action string, kind apigen.PermissionResourceType, id string) string {
	q := url.Values{"action": {action}}
	if id != "" {
		q.Set("resourceType", string(kind))
		q.Set("resourceId", id)
	}
	return q.Encode()
}

const linkHash = "057a36867e8b5d54bdfba284ee2f3a02"

func policyReason(slug, id string) policydecision.Reason {
	return policydecision.Reason{PolicyID: grn.New(slug, "policy", id).String()}
}

// TestExplainPermission_NamesARoleLinkByItsReservedIdentifier holds
// the three paths a reserved identifier reaches the caller by, and that
// the path is the question's own: a team's link is the team's where the
// question is asked of a team, and carries no path asked of anything
// else. No read is made, so a caller who may not read policies is told.
func TestExplainPermission_NamesARoleLinkByItsReservedIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		query    string
		wantPath *apigen.ExplainedGroundPath
		wantTeam string
		wantProj string
	}{
		{"organization link", "link-owner-" + linkHash, queryFor(org.ActionOrgRead, "", ""),
			ptr(apigen.GroundPathOrganization), "", ""},
		{"team link on its team", "link-scoped-admin-" + linkHash,
			queryFor(org.ActionGroupUpdate, apigen.PermissionResourceTypeGroup, "eng"),
			ptr(apigen.GroundPathTeam), "eng", ""},
		{"project link on its project", "link-scoped-member-" + linkHash,
			queryFor(org.ActionProjectUpdate, apigen.PermissionResourceTypeProject, "web"),
			ptr(apigen.GroundPathProject), "", "web"},
		{"team link asked of the organization", "link-scoped-admin-" + linkHash,
			queryFor(org.ActionOrgRead, "", ""), nil, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			relay := &storedPolicies{}
			f.server.WithPolicyRelay(relay)
			owner := f.sessionCookieFor(t, "owner-sub", "Owner")
			slug := createOrg(t, f, owner, "acme")

			got := f.explains(t, owner, slug, tt.query, true, false, policyReason(slug, tt.id)).one(t)

			assert.Equal(t, apigen.GroundRoleLink, got.Kind)
			require.NotNil(t, got.Role)
			assert.Equal(t, tt.id, *got.Policy)
			assert.Equal(t, tt.wantPath, got.Path)
			assert.Equal(t, tt.wantTeam, deref(got.Team))
			assert.Equal(t, tt.wantProj, deref(got.Project))
			assert.Zero(t, relay.reads.Load(), "a reserved identifier is told without a read")
		})
	}
}

// TestExplainPermission_ReadsTheKindOfAStoredPolicyWhereTheCallerMayRead
// holds that the kind of a policy the platform does not reserve the
// identifier of is the policy service's, whatever the decision is: a
// role link a console wrote under a `p-` identifier is a role link that
// allows, a guardrail one that forbids, and a permit one that allows.
func TestExplainPermission_ReadsTheKindOfAStoredPolicyWhereTheCallerMayRead(t *testing.T) {
	text := "permit(principal, action, resource);"
	team := grn.New("acme", org.ServiceTypeGroup, "eng")
	project := grn.New("acme", org.ServiceTypeProject, "web")
	user := grn.New("root", "user", "ada")
	tests := []struct {
		name    string
		allowed bool
		policy  policyrelay.Detail
		check   func(t *testing.T, got apigen.ExplainedGround)
	}{
		{"role link on the organization", true,
			stored(policyrelay.KindRoleLink, "admins", policyrelay.Body{Link: &policyrelay.RoleLink{
				Role: org.RoleAdmin, Scope: policyrelay.ScopeOrganization, Principal: team, Resource: grn.Org("acme")}}),
			func(t *testing.T, got apigen.ExplainedGround) {
				assert.Equal(t, apigen.GroundRoleLink, got.Kind)
				assert.Equal(t, apigen.RoleAdmin, deref(got.Role))
				assert.Equal(t, apigen.GroundPathOrganization, deref(got.Path))
				assert.Nil(t, got.Text)
			}},
		{"role link on a team", true,
			stored(policyrelay.KindRoleLink, "leads", policyrelay.Body{Link: &policyrelay.RoleLink{
				Role: org.RoleMember, Scope: policyrelay.ScopeTeamOrProject, Principal: user, Resource: team}}),
			func(t *testing.T, got apigen.ExplainedGround) {
				assert.Equal(t, apigen.GroundRoleLink, got.Kind)
				assert.Equal(t, apigen.GroundPathTeam, deref(got.Path))
				assert.Equal(t, "eng", deref(got.Team))
			}},
		{"role link on a project", true,
			stored(policyrelay.KindRoleLink, "builders", policyrelay.Body{Link: &policyrelay.RoleLink{
				Role: org.RoleMember, Scope: policyrelay.ScopeTeamOrProject, Principal: user, Resource: project}}),
			func(t *testing.T, got apigen.ExplainedGround) {
				assert.Equal(t, apigen.GroundPathProject, deref(got.Path))
				assert.Equal(t, "web", deref(got.Project))
			}},
		{"permit that allows", true,
			stored(policyrelay.KindPermit, "open", policyrelay.Body{Text: text}),
			func(t *testing.T, got apigen.ExplainedGround) {
				assert.Equal(t, apigen.GroundPermit, got.Kind)
				assert.Equal(t, "open", deref(got.Name))
				assert.Equal(t, text, deref(got.Text))
				assert.Nil(t, got.Role)
			}},
		{"guardrail that forbids", false,
			stored(policyrelay.KindGuardrail, "closed", policyrelay.Body{Text: text}),
			func(t *testing.T, got apigen.ExplainedGround) {
				assert.Equal(t, apigen.GroundGuardrail, got.Kind)
				assert.Equal(t, "closed", deref(got.Name))
				assert.Equal(t, text, deref(got.Text))
			}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			relay := &storedPolicies{held: map[string]policyrelay.Detail{"p-1": tt.policy}}
			f.server.WithPolicyRelay(relay)
			owner := f.sessionCookieFor(t, "owner-sub", "Owner")
			slug := createOrg(t, f, owner, "acme")

			got := f.explains(t, owner, slug, queryFor(org.ActionOrgRead, "", ""), tt.allowed, true,
				policyReason(slug, "p-1")).one(t)

			assert.Equal(t, "p-1", deref(got.Policy))
			tt.check(t, got)
			assert.Equal(t, int32(1), relay.reads.Load())
		})
	}
}

// TestExplainPermission_WithholdsTheKindOfAStoredPolicyItDoesNotShow
// holds that a stored policy the caller may not read, or the service no
// longer shows, is named as one whose kind is withheld, not as the kind
// the decision would suggest, and that a read the caller may make of the
// same policy is the control.
func TestExplainPermission_WithholdsTheKindOfAStoredPolicyItDoesNotShow(t *testing.T) {
	held := map[string]policyrelay.Detail{
		"p-1": stored(policyrelay.KindPermit, "open", policyrelay.Body{Text: "permit(principal, action, resource);"}),
		// Reads as a role link but for the name: a short identifier is
		// no reserved one.
		"link-admin": stored(policyrelay.KindPermit, "near", policyrelay.Body{Text: "permit(principal, action, resource);"}),
	}
	query := queryFor(org.ActionOrgRead, "", "")

	t.Run("the caller may not read policies", func(t *testing.T) {
		f := newFixture(t)
		relay := &storedPolicies{held: held}
		f.server.WithPolicyRelay(relay)
		owner := f.sessionCookieFor(t, "owner-sub", "Owner")
		slug := createOrg(t, f, owner, "acme")

		got := f.explains(t, owner, slug, query, true, false, policyReason(slug, "p-1")).one(t)

		assert.Equal(t, apigen.GroundStoredPolicy, got.Kind)
		assert.Equal(t, "p-1", deref(got.Policy))
		assert.Nil(t, got.Name)
		assert.Nil(t, got.Text)
		assert.Zero(t, relay.reads.Load(), "the service is not read for a caller who may not read")
	})
	t.Run("the service does not show it", func(t *testing.T) {
		f := newFixture(t)
		f.server.WithPolicyRelay(&storedPolicies{held: held})
		owner := f.sessionCookieFor(t, "owner-sub", "Owner")
		slug := createOrg(t, f, owner, "acme")

		got := f.explains(t, owner, slug, query, true, true, policyReason(slug, "p-gone")).one(t)

		assert.Equal(t, apigen.GroundStoredPolicy, got.Kind)
		assert.Nil(t, got.Name)
	})
	t.Run("an identifier that only starts like a link is no link", func(t *testing.T) {
		f := newFixture(t)
		f.server.WithPolicyRelay(&storedPolicies{held: held})
		owner := f.sessionCookieFor(t, "owner-sub", "Owner")
		slug := createOrg(t, f, owner, "acme")

		got := f.explains(t, owner, slug, query, true, true, policyReason(slug, "link-admin")).one(t)

		assert.Equal(t, apigen.GroundPermit, got.Kind, "read as what the service holds")
	})
	t.Run("control: the same policy to a caller who may read", func(t *testing.T) {
		f := newFixture(t)
		f.server.WithPolicyRelay(&storedPolicies{held: held})
		owner := f.sessionCookieFor(t, "owner-sub", "Owner")
		slug := createOrg(t, f, owner, "acme")

		got := f.explains(t, owner, slug, query, true, true, policyReason(slug, "p-1")).one(t)

		assert.Equal(t, apigen.GroundPermit, got.Kind)
		assert.Equal(t, "open", deref(got.Name))
	})
}

// TestExplainPermission_NamesWhatIsNotAStoredPolicyOfTheOrganization
// holds the grounds that are no policy the caller reads: a system policy
// by its file name, a reason with an error, a refused question, and a
// policy of another organization, none of which is read.
func TestExplainPermission_NamesWhatIsNotAStoredPolicyOfTheOrganization(t *testing.T) {
	query := queryFor(org.ActionOrgRead, "", "")
	f := newFixture(t)
	relay := &storedPolicies{}
	f.server.WithPolicyRelay(relay)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")

	byKind := func(r *httpResult) map[apigen.ExplainedGroundKind][]apigen.ExplainedGround {
		out := map[apigen.ExplainedGroundKind][]apigen.ExplainedGround{}
		for _, g := range r.Grounds {
			out[g.Kind] = append(out[g.Kind], g)
		}
		return out
	}
	got := byKind(f.explains(t, owner, slug, query, false, true,
		policydecision.Reason{PolicyID: "organization.cedar"},
		policydecision.Reason{PolicyID: grn.New(slug, "policy", "p-bad").String(), Error: "type error"},
		policydecision.Reason{Error: "refused"},
		policydecision.Reason{PolicyID: grn.New("other", "policy", "p-1").String()},
	))

	require.Len(t, got[apigen.GroundSystemPolicy], 1)
	assert.Equal(t, "organization.cedar", deref(got[apigen.GroundSystemPolicy][0].Policy))
	require.Len(t, got[apigen.GroundError], 2)
	assert.ElementsMatch(t, []string{"type error", "refused"},
		[]string{deref(got[apigen.GroundError][0].Error), deref(got[apigen.GroundError][1].Error)})
	require.Len(t, got[apigen.GroundStoredPolicy], 1)
	assert.Nil(t, got[apigen.GroundStoredPolicy][0].Policy, "another organization's policy is not named")
	assert.Zero(t, relay.reads.Load(), "none of these is read")
}

// TestExplainPermission_RefusesWith503WhenThePolicyServiceDoesNotAnswer
// holds that a question the service cannot answer is an unavailable
// answer and not a deny with no grounds. The same pair answered is the
// control.
func TestExplainPermission_RefusesWith503WhenThePolicyServiceDoesNotAnswer(t *testing.T) {
	f := newFixture(t)
	owner := f.sessionCookieFor(t, "owner-sub", "Owner")
	slug := createOrg(t, f, owner, "acme")
	f.decisions.EXPECT().Evaluate(mock.Anything, mock.Anything).
		Return(policydecision.Decision{}, errorx.New(errorx.KindUnavailable, "policy service is down")).Once()
	path := "/orgs/" + slug + "/permission-explanation?" + queryFor(org.ActionOrgRead, "", "")

	rec := f.do(t, "GET", path, "", owner)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())

	f.decisions.EXPECT().Evaluate(mock.Anything, mock.Anything).
		Return(policydecision.Decision{Allowed: true}, nil).Once()
	assert.Equal(t, http.StatusOK, f.do(t, "GET", path, "", owner).Code, "control: an answer is a 200")
}

func ptr[T any](v T) *T { return &v }

func deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
