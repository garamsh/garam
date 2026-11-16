//go:build policymatrix

package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/policydecision"
	decisiongrpc "github.com/garamsh/garam/platform/internal/policydecision/grpc"
	relaygrpc "github.com/garamsh/garam/platform/internal/policyrelay/grpc"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
	"github.com/garamsh/garam/platform/internal/wire"
)

// The cases here hold the browser listener's enforcement, its
// permission checks and its holders list against the stack's real
// policy service (ADR-0171): the service decides what a role carries
// and what a guardrail forbids, and an unreachable service is a 503,
// never an allow.

// guardrailName is the name the guardrail these cases write is given.
const guardrailName = "no-invites"

const enforcementGuardrail = `forbid (principal, action == Iam::Action::"org.invite", resource);`

type enforcementStack struct {
	url                 string
	rp                  *realPolicy
	owner, member       *http.Cookie
	ownerGRN, memberGRN grn.GRN
	acme                grn.GRN
	policies            *authz.PolicyGRPCServer
}

// newEnforcementStack composes the listener with decisions as the
// policy service the enforcement asks and the real service as the
// relay.
func newEnforcementStack(t *testing.T, decide func(*realPolicy) policydecision.Service) *enforcementStack {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	rp := startRealPolicy(t, p)
	orgs, users := orgpg.New(p), userpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	session := func(id string, role org.Role) (*http.Cookie, grn.GRN) {
		g := grn.New(grn.RootOwner, user.ServiceType, id)
		require.NoError(t, users.Create(ctx, user.New(g, id)))
		require.NoError(t, orgs.AddMember(ctx, org.NewMembership(acme, g, role)))
		token, err := authn.NewToken()
		require.NoError(t, err)
		require.NoError(t, authnpg.NewSessionStore(p).Create(ctx, token, authn.NewSession(g, time.Hour)))
		return &http.Cookie{Name: "garam_session", Value: token}, g
	}
	s := &enforcementStack{rp: rp, acme: acme}
	s.owner, s.ownerGRN = session("ada", org.RoleOwner)
	s.member, s.memberGRN = session("bob", org.RoleMember)
	rp.deliver(t)
	// The service outlives the test: a guardrail an earlier run left
	// in it would deny this run's controls, so none is left before or
	// after.
	writer := &policyv1.Subject{Type: "Iam::User", Id: s.ownerGRN.String()}
	dropGuardrails := func() {
		listed, err := rp.client.ListPolicies(ctx, &policyv1.ListPoliciesRequest{Writer: writer, Org: acme.String()})
		if err != nil {
			return
		}
		for _, summary := range listed.GetPolicies() {
			if summary.GetName() == guardrailName {
				_, _ = rp.client.DeletePolicy(ctx, &policyv1.DeletePolicyRequest{Writer: writer, Org: acme.String(), Policy: summary.GetPolicy()})
			}
		}
	}
	dropGuardrails()
	t.Cleanup(dropGuardrails)

	policies, engine := policyEngine(t, p)
	s.policies = policies
	srv := api.NewServer(users, orgs, nil, nil, nil, nil, authnpg.NewSessionStore(p),
		nil, nil, nil, nil, api.NewGRPCEvaluator(engine), decide(rp), api.NewGRPCPolicyManager(policyService(t, policies)),
		nil, nil, nil, api.Config{SessionTTL: time.Hour}).
		WithPolicyRelay(relaygrpc.New(rp.client))
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	s.url = browser.URL
	return s
}

func realDecisions(rp *realPolicy) policydecision.Service { return rp.asked }

func (s *enforcementStack) check(t *testing.T, cookie *http.Cookie, action string) apigen.PermissionDecision {
	t.Helper()
	return permissionDecisions(t, s.url, cookie, action)[0]
}

func (s *enforcementStack) get(t *testing.T, path string, cookie *http.Cookie) (int, string) {
	t.Helper()
	return browse(t, http.MethodGet, s.url+path, cookie, "")
}

// TestEnforcement_TheServiceDecidesWhatARoleCarries shows an owner
// allowed what its role carries and a member refused what its role does
// not, in the permission check and in the route that acts on it. The
// owner's allowance is each refusal's control.
func TestEnforcement_TheServiceDecidesWhatARoleCarries(t *testing.T) {
	s := newEnforcementStack(t, realDecisions)

	assert.Equal(t, apigen.DecisionAllow, s.check(t, s.owner, org.ActionOrgInvite), "an owner's role carries org.invite")
	assert.Equal(t, apigen.DecisionDeny, s.check(t, s.member, org.ActionOrgInvite), "a member's role does not")
	assert.Equal(t, apigen.DecisionAllow, s.check(t, s.member, org.ActionOrgRead), "control: a member reads the organization")

	status, body := s.get(t, "/orgs/acme", s.member)
	assert.Equal(t, http.StatusOK, status, "control: the member's read is allowed: %s", body)
	status, body = browse(t, http.MethodPost, s.url+"/orgs/acme/invitations", s.member, `{"email":"x@example.com","role":"member"}`)
	assert.Equal(t, http.StatusForbidden, status, "the member's invite is refused: %s", body)
	status, body = browse(t, http.MethodPost, s.url+"/orgs/acme/invitations", s.owner, `{"email":"x@example.com","role":"member"}`)
	assert.Equal(t, http.StatusCreated, status, "control: the owner's invite is allowed: %s", body)
}

// TestEnforcement_AGuardrailWrittenThroughTheRelayForbidsWhatTheRoleAllows
// writes a guardrail through the relay (#1971) that forbids an action
// the owner's role carries. The same pair before the guardrail is the
// control, and the member's unforbidden read after it shows the
// guardrail refuses the action and not the route.
func TestEnforcement_AGuardrailWrittenThroughTheRelayForbidsWhatTheRoleAllows(t *testing.T) {
	s := newEnforcementStack(t, realDecisions)
	require.Equal(t, apigen.DecisionAllow, s.check(t, s.owner, org.ActionOrgInvite), "control: allowed before the guardrail")

	body := textBody("guardrail", enforcementGuardrail)
	status, got := browse(t, http.MethodPost, s.url+"/orgs/acme/iam-policies", s.owner, `{"name":"`+guardrailName+`","body":`+body+`}`)
	require.Equal(t, http.StatusCreated, status, got)
	id := policyID(t, got)
	status, got = browse(t, http.MethodPost, s.url+"/orgs/acme/iam-policies/"+id+"/activate", s.owner, "")
	require.Equal(t, http.StatusNoContent, status, got)

	assert.Equal(t, apigen.DecisionDeny, s.check(t, s.owner, org.ActionOrgInvite), "the guardrail forbids what the role allows")
	status, got = browse(t, http.MethodPost, s.url+"/orgs/acme/invitations", s.owner, `{"email":"y@example.com","role":"member"}`)
	assert.Equal(t, http.StatusForbidden, status, "and the route refuses it: %s", got)
	assert.Equal(t, apigen.DecisionAllow, s.check(t, s.owner, org.ActionOrgRead), "control: another action is not forbidden")
}

// TestEnforcement_AnUnreachableServiceIsUnavailableNotForbidden points
// enforcement at an address nothing listens on: a read the real service
// allows is a 503 'unavailable', and a forbid by the real service is a
// 403, so the two are told apart.
func TestEnforcement_AnUnreachableServiceIsUnavailableNotForbidden(t *testing.T) {
	down := newEnforcementStack(t, func(*realPolicy) policydecision.Service {
		conn, err := grpc.NewClient("127.0.0.1:1", grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		return decisiongrpc.New(policyv1.NewPolicyServiceClient(conn))
	})
	status, got := down.get(t, "/orgs/acme", down.owner)
	assert.Equal(t, http.StatusServiceUnavailable, status, got)
	assert.Contains(t, got, `"unavailable"`)

	up := newEnforcementStack(t, realDecisions)
	status, got = up.get(t, "/orgs/acme", up.owner)
	assert.Equal(t, http.StatusOK, status, "control: the service answers: %s", got)
	status, got = browse(t, http.MethodPost, up.url+"/orgs/acme/invitations", up.member, `{"email":"z@example.com","role":"member"}`)
	assert.Equal(t, http.StatusForbidden, status, "a forbid is a 403: %s", got)
}

// TestEnforcement_AQuestionOnAPolicyIsAskedOnItsOrganization shows what
// the service decides about a policy of the platform's own store. The
// service holds no entity for one, so a question put on the policy
// itself is denied to everyone; the platform asks it on the policy's
// organization, as the service decides a write to a policy, and the
// owner is allowed what the member is refused (ADR-0171 item 7). The
// member's refusal is the owner's control.
func TestEnforcement_AQuestionOnAPolicyIsAskedOnItsOrganization(t *testing.T) {
	s := newEnforcementStack(t, realDecisions)
	policy, err := grn.Parse(activePolicy(t, s.policies, s.acme, "held",
		statementDoc("held", wire.EffectAllow, org.ActionOrgRead, s.acme.String())))
	require.NoError(t, err)
	onPolicy := apigen.PermissionCheck{Action: org.ActionPolicyUpdate,
		Resource: &apigen.PermissionResource{Type: apigen.PermissionResourceTypePolicy, Id: policy.Identifier}}

	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionAllow},
		checkedDecisions(t, s.url, s.owner, onPolicy), "an owner's role carries iam:policy.update")
	assert.Equal(t, []apigen.PermissionDecision{apigen.DecisionDeny},
		checkedDecisions(t, s.url, s.member, onPolicy), "a member's role does not")

	versions := "/orgs/acme/policies/" + policy.Identifier + "/versions"
	status, body := s.get(t, versions, s.owner)
	assert.Equal(t, http.StatusOK, status, "an owner reads the policy's versions: %s", body)
	status, body = s.get(t, versions, s.member)
	assert.Equal(t, http.StatusForbidden, status, "a member does not: %s", body)
}
