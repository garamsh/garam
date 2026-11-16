//go:build policymatrix

package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/policyrelay"
	relaygrpc "github.com/garamsh/garam/platform/internal/policyrelay/grpc"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// The cases here hold the platform's relay of an organization's role
// links, guardrails and permits against the stack's real policy service
// (ADR-0146 item 3, ADR-0169): the service decides the writer, the
// platform stores no body, and a write the service refuses stores
// nothing.

const (
	relayGuardrail = `forbid (principal, action == Iam::Action::"agent.activate", resource);`
	relayPermit    = `permit (principal, action == Iam::Action::"agent.read", resource in Iam::Org::"grn:root/iam/org/acme");`
)

// relayStack is the browser listener over the real policy service, with
// an owner, an administrator and a member of the organization acme.
type relayStack struct {
	url                 string
	rp                  *realPolicy
	acme                grn.GRN
	owner, admin, other *http.Cookie
	ownerGRN            grn.GRN
	made                []string
}

// newRelayStack composes the listener with relay as the policy relay,
// the real service's unless the case gives another.
func newRelayStack(t *testing.T, relay func(*realPolicy) policyrelay.Service) *relayStack {
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
	s := &relayStack{rp: rp, acme: acme}
	s.owner, s.ownerGRN = session("ada", org.RoleOwner)
	s.admin, _ = session("bea", org.RoleAdmin)
	s.other, _ = session("bob", org.RoleMember)
	rp.deliver(t)

	srv := api.NewServer(users, orgs, nil, nil, nil, nil, authnpg.NewSessionStore(p),
		nil, nil, nil, nil, nil, nil, nil, nil, nil, api.Config{SessionTTL: time.Hour}).
		WithPolicyRelay(relay(rp))
	browser := httptest.NewServer(apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError}))
	t.Cleanup(browser.Close)
	s.url = browser.URL + "/orgs/acme/iam-policies"

	// The service outlives the test, so what a case made is removed.
	t.Cleanup(func() {
		for _, id := range s.made {
			_, _ = rp.client.DeletePolicy(ctx, &policyv1.DeletePolicyRequest{
				Writer: &policyv1.Subject{Type: "Iam::User", Id: s.ownerGRN.String()},
				Org:    acme.String(), Policy: id,
			})
		}
	})
	return s
}

func realRelay(rp *realPolicy) policyrelay.Service {
	return relaygrpc.New(rp.client)
}

// send makes one request as cookie and answers the status and body.
func (s *relayStack) send(t *testing.T, method, path string, cookie *http.Cookie, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, s.url+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(got)
}

// create writes a policy as cookie, answers its status and body, and
// removes a policy it made when the case ends.
func (s *relayStack) create(t *testing.T, cookie *http.Cookie, body string) (int, string) {
	t.Helper()
	status, got := s.send(t, http.MethodPost, "", cookie, `{"name":"n","body":`+body+`}`)
	if status == http.StatusCreated {
		var made apigen.IamPolicy
		require.NoError(t, json.Unmarshal([]byte(got), &made))
		s.made = append(s.made, made.Id)
	}
	return status, got
}

// ids lists the organization's policies as the owner and answers their
// identifiers.
func (s *relayStack) ids(t *testing.T) []string {
	t.Helper()
	status, got := s.send(t, http.MethodGet, "", s.owner, "")
	require.Equal(t, http.StatusOK, status, got)
	var listed []apigen.IamPolicy
	require.NoError(t, json.Unmarshal([]byte(got), &listed))
	out := make([]string, len(listed))
	for i, p := range listed {
		out[i] = p.Id
	}
	return out
}

func textBody(kind, text string) string {
	raw, _ := json.Marshal(map[string]string{"kind": kind, "text": text})
	return string(raw)
}

func linkBody(principal, resource, scope string) string {
	raw, _ := json.Marshal(map[string]any{"kind": "role-link", "roleLink": map[string]string{
		"role": "member", "scope": scope, "principal": principal, "resource": resource,
	}})
	return string(raw)
}

func policyID(t *testing.T, got string) string {
	t.Helper()
	var made apigen.IamPolicy
	require.NoError(t, json.Unmarshal([]byte(got), &made))
	return made.Id
}

// TestPolicyRelay_AnOwnerWritesEachKindThroughItsLifecycle creates,
// versions, rolls back, activates, reads, lists and deletes a
// guardrail, a permit and a role link, each as the owner.
func TestPolicyRelay_AnOwnerWritesEachKindThroughItsLifecycle(t *testing.T) {
	s := newRelayStack(t, realRelay)
	agentLink := linkBody(grn.New("acme", "agent", "bot").String(), s.acme.String(), "organization")
	for name, body := range map[string][2]string{
		"guardrail": {textBody("guardrail", relayGuardrail), textBody("guardrail", relayGuardrail+" ")},
		"permit":    {textBody("permit", relayPermit), textBody("permit", relayPermit+" ")},
		"role-link": {agentLink, agentLink},
	} {
		t.Run(name, func(t *testing.T) {
			status, got := s.create(t, s.owner, body[0])
			require.Equal(t, http.StatusCreated, status, got)
			id := policyID(t, got)
			assert.Contains(t, s.ids(t), id, "the service lists what was written")

			status, got = s.send(t, http.MethodPost, "/"+id+"/versions", s.owner, `{"body":`+body[1]+`}`)
			require.Equal(t, http.StatusCreated, status, got)
			assert.JSONEq(t, `{"version":2}`, got)
			status, got = s.send(t, http.MethodPost, "/"+id+"/rollback", s.owner, `{"version":2}`)
			require.Equal(t, http.StatusNoContent, status, got)
			status, got = s.send(t, http.MethodPost, "/"+id+"/activate", s.owner, "")
			require.Equal(t, http.StatusNoContent, status, got)

			status, got = s.send(t, http.MethodGet, "/"+id, s.owner, "")
			require.Equal(t, http.StatusOK, status, got)
			var detail apigen.IamPolicyDetail
			require.NoError(t, json.Unmarshal([]byte(got), &detail))
			assert.Equal(t, apigen.Active, detail.Policy.Status)
			assert.Equal(t, int32(2), detail.Policy.DefaultVersion)
			assert.Equal(t, apigen.IamPolicyKind(name), detail.Policy.Kind)
			require.Len(t, detail.Versions, 2, "versions, oldest first")
			assert.Equal(t, int32(1), detail.Versions[0].Version)
			assert.Equal(t, s.ownerGRN.String(), detail.Versions[0].CreatedBy, "the caller wrote it")

			status, got = s.send(t, http.MethodDelete, "/"+id, s.owner, "")
			require.Equal(t, http.StatusNoContent, status, got)
			status, _ = s.send(t, http.MethodGet, "/"+id, s.owner, "")
			assert.Equal(t, http.StatusNotFound, status, "a deleted policy is gone")
			assert.NotContains(t, s.ids(t), id)
		})
	}
}

// TestPolicyRelay_AWriteTheServiceRefusesStoresNothing shows each
// refusal as the service made it, and that the policy list is as it
// was. An owner's write of the same guardrail is the control: it is
// the writer, and not the route, that is refused.
func TestPolicyRelay_AWriteTheServiceRefusesStoresNothing(t *testing.T) {
	s := newRelayStack(t, realRelay)
	before := s.ids(t)
	guardrail := textBody("guardrail", relayGuardrail)

	status, got := s.create(t, s.other, guardrail)
	assert.Equal(t, http.StatusForbidden, status, "a member may not write: %s", got)
	status, got = s.create(t, s.owner, textBody("guardrail", "forbid (principal"))
	assert.Equal(t, http.StatusBadRequest, status, "the service rejects the body: %s", got)
	assert.Contains(t, got, `"invalid_argument"`)
	assert.Equal(t, before, s.ids(t), "neither write stored anything")

	status, got = s.create(t, s.owner, guardrail)
	require.Equal(t, http.StatusCreated, status, "control: the same body by an owner is stored: %s", got)
	assert.Len(t, s.ids(t), len(before)+1)
}

// TestPolicyRelay_ARefusedVersionIsNotStored shows a version a member
// writes and a body the service rejects leave the policy's versions as
// they were.
func TestPolicyRelay_ARefusedVersionIsNotStored(t *testing.T) {
	s := newRelayStack(t, realRelay)
	status, got := s.create(t, s.owner, textBody("guardrail", relayGuardrail))
	require.Equal(t, http.StatusCreated, status, got)
	id := policyID(t, got)

	status, got = s.send(t, http.MethodPost, "/"+id+"/versions", s.other, `{"body":`+textBody("guardrail", relayGuardrail)+`}`)
	assert.Equal(t, http.StatusForbidden, status, got)
	status, got = s.send(t, http.MethodPost, "/"+id+"/versions", s.owner, `{"body":`+textBody("guardrail", "forbid (")+`}`)
	assert.Equal(t, http.StatusBadRequest, status, got)
	status, got = s.send(t, http.MethodDelete, "/"+id, s.other, "")
	assert.Equal(t, http.StatusForbidden, status, got)

	status, got = s.send(t, http.MethodGet, "/"+id, s.owner, "")
	require.Equal(t, http.StatusOK, status, "the policy still stands: %s", got)
	var detail apigen.IamPolicyDetail
	require.NoError(t, json.Unmarshal([]byte(got), &detail))
	assert.Len(t, detail.Versions, 1, "no refused version was stored")

	status, got = s.send(t, http.MethodPost, "/"+id+"/versions", s.owner, `{"body":`+textBody("guardrail", relayGuardrail)+`}`)
	assert.Equal(t, http.StatusCreated, status, "control: the owner's valid version is stored: %s", got)
	status, got = s.send(t, http.MethodDelete, "/"+id, s.owner, "")
	assert.Equal(t, http.StatusNoContent, status, "control: the owner deletes it: %s", got)
}

// TestPolicyRelay_TheServiceBeingDownIsUnavailable points the relay at
// an address nothing listens on. The same request to the real service
// is the control.
func TestPolicyRelay_TheServiceBeingDownIsUnavailable(t *testing.T) {
	down := newRelayStack(t, func(*realPolicy) policyrelay.Service {
		conn, err := grpc.NewClient("127.0.0.1:1", grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		return relaygrpc.New(policyv1.NewPolicyServiceClient(conn))
	})
	status, got := down.create(t, down.owner, textBody("guardrail", relayGuardrail))
	assert.Equal(t, http.StatusServiceUnavailable, status, got)
	status, got = down.send(t, http.MethodGet, "", down.owner, "")
	assert.Equal(t, http.StatusServiceUnavailable, status, got)

	up := newRelayStack(t, realRelay)
	status, got = up.create(t, up.owner, textBody("guardrail", relayGuardrail))
	assert.Equal(t, http.StatusCreated, status, "control: the service answers: %s", got)
}

// TestPolicyRelay_OnlyAnOwnerWritesAPermit shows an administrator, who
// writes a guardrail, refused a permit, and the owner who writes it.
func TestPolicyRelay_OnlyAnOwnerWritesAPermit(t *testing.T) {
	s := newRelayStack(t, realRelay)
	permit := textBody("permit", relayPermit)

	status, got := s.create(t, s.admin, textBody("guardrail", relayGuardrail))
	require.Equal(t, http.StatusCreated, status, "control: an administrator writes a guardrail: %s", got)

	before := s.ids(t)
	status, got = s.create(t, s.admin, permit)
	assert.Equal(t, http.StatusForbidden, status, "an administrator is not an owner: %s", got)
	assert.Equal(t, before, s.ids(t), "the refused permit stored nothing")

	status, got = s.create(t, s.owner, permit)
	assert.Equal(t, http.StatusCreated, status, "the owner writes it: %s", got)
}

// TestPolicyRelay_AMembershipsRoleLinkIsNotWrittenHere shows the two
// shapes the platform derives from a membership refused, and a link a
// writer owns (an agent's) written.
func TestPolicyRelay_AMembershipsRoleLinkIsNotWrittenHere(t *testing.T) {
	s := newRelayStack(t, realRelay)
	before := s.ids(t)
	org := s.acme.String()
	team := grn.New("acme", "group", "eng").String()

	status, got := s.create(t, s.owner, linkBody(s.ownerGRN.String(), org, "organization"))
	assert.Equal(t, http.StatusBadRequest, status, "a user's organization role is its membership's: %s", got)
	status, got = s.create(t, s.owner, linkBody(team, team, "team-or-project"))
	assert.Equal(t, http.StatusBadRequest, status, "a team's own scoped link is derived: %s", got)
	assert.Equal(t, before, s.ids(t), "neither was stored")

	status, got = s.create(t, s.owner, linkBody(grn.New("acme", "agent", "bot").String(), org, "organization"))
	assert.Equal(t, http.StatusCreated, status, "control: an agent's link is a writer's: %s", got)
	status, got = s.create(t, s.owner, linkBody(s.ownerGRN.String(), team, "team-or-project"))
	assert.Equal(t, http.StatusCreated, status, "control: a user's link to a team is a writer's: %s", got)
}
