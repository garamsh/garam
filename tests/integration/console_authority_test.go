package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/api"
	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/wire"
)

// consoleOperations is every operation a console read or publication is
// minted for (#1170), granted beside create.
var consoleOperations = []string{
	"agent:create", "agent-template:read", "agent-template:publish",
	"execution-profile:read", "agent:execution-read",
}

// mintTarget asks for an authority binding requestTarget, or none where
// it is empty, and answers the status and body.
func (s *authorityStack) mintTarget(t *testing.T, cookie *http.Cookie, operation, target, requestID, requestTarget string) (int, string) {
	t.Helper()
	body := map[string]string{
		"audience": s.hosted.String(), "operation": operation, "target": target,
		"requestId": requestID, "bodySha256": emptyBodySHA256,
	}
	if requestTarget != "" {
		body["requestTarget"] = requestTarget
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", cookie, string(raw))
}

// emptyBodySHA256 is the SHA-256 of the empty body every GET sends.
const emptyBodySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func bindingOf(t *testing.T, body string) machinegen.OperationBinding {
	t.Helper()
	var b machinegen.OperationBinding
	require.NoError(t, json.Unmarshal([]byte(body), &b))
	return b
}

// TestConsoleAuthority_ReadsAndPublicationBindTheirRequestTarget holds
// the #1170 matrix at garam's boundary: each read and the publication
// binds the exact request target it is minted for, and introspection
// answers it byte for byte. Each refusal sits beside the accepted call
// it differs from in one field.
func TestConsoleAuthority_ReadsAndPublicationBindTheirRequestTarget(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	s.grant(t, "g-1", nil, consoleOperations...)
	orgTarget := s.acme.String()
	const templates = "/prefix/v1/orgs/acme/templates?after=web%20agent"

	status, got := s.mintTarget(t, s.member, "agent-template:read", orgTarget, "read-1", templates)
	require.Equal(t, http.StatusCreated, status, got)
	read := authorityOf(t, got)
	status, got = s.introspect(t, s.hostedTLS, read.Authority)
	require.Equal(t, http.StatusOK, status, got)
	b := bindingOf(t, got)
	require.NotNil(t, b.RequestTarget)
	assert.Equal(t, templates, *b.RequestTarget, "bound byte for byte, escapes and query kept")
	assert.Equal(t, machinegen.OperationBindingOperation("agent-template:read"), b.Operation)
	assert.Equal(t, orgTarget, b.TargetGrn)
	assert.Equal(t, emptyBodySHA256, b.BodySha256)
	assert.Nil(t, b.Assignment, "an organization operation binds no placement")

	status, got = s.mintTarget(t, s.member, "agent-template:read", orgTarget, "read-2", "")
	assert.Equal(t, http.StatusBadRequest, status, "a read without its request target: %s", got)
	status, got = s.mintTarget(t, s.member, "agent:create", s.controller.String(), "create-1", templates)
	assert.Equal(t, http.StatusBadRequest, status, "create binds no request target: %s", got)
	status, got = s.mintTarget(t, s.member, "agent:create", s.controller.String(), "create-1", "")
	assert.Equal(t, http.StatusCreated, status, "the create control: %s", got)
	status, got = s.mintTarget(t, s.member, "agent-template:read", s.controller.String(), "read-3", templates)
	assert.Equal(t, http.StatusBadRequest, status, "an organization operation targets the organization: %s", got)
	status, got = s.mintTarget(t, s.member, "agent-template:read", orgTarget, "read-4", "https://control.example.com"+templates)
	assert.Equal(t, http.StatusBadRequest, status, "a request target carries no origin: %s", got)

	status, got = s.mintTarget(t, s.member, "agent-template:read", orgTarget, "read-1", templates+"&x=1")
	assert.Equal(t, http.StatusConflict, status, "the request id is bound to another query: %s", got)
	status, got = s.mintTarget(t, s.member, "agent-template:read", orgTarget, "read-1", templates)
	require.Equal(t, http.StatusCreated, status, "the same binding remints: %s", got)
	assert.Equal(t, read.OperationRef, authorityOf(t, got).OperationRef)

	status, got = s.mintTarget(t, s.member, "execution-profile:read", orgTarget, "profiles-1", "/v1/orgs/acme/profiles")
	assert.Equal(t, http.StatusCreated, status, "a member reads profiles: %s", got)

	const publish = "/v1/orgs/acme/templates/web/versions"
	status, got = s.mintTarget(t, s.member, "agent-template:publish", orgTarget, "publish-1", publish)
	assert.Equal(t, http.StatusForbidden, status, "a member does not publish: %s", got)
	status, got = s.mintTarget(t, s.owner, "agent-template:publish", orgTarget, "publish-1", publish)
	assert.Equal(t, http.StatusCreated, status, "the owner publishes: %s", got)

	agentGRN := s.provisionAgent(t)
	execution := "/v1/orgs/acme/agents/" + agentGRN + "/execution"
	status, got = s.mintTarget(t, s.member, "agent:execution-read", agentGRN, "execution-1", execution)
	assert.Equal(t, http.StatusForbidden, status, "agent:read is identity access, not execution: %s", got)
	status, got = s.mintTarget(t, s.owner, "agent:execution-read", agentGRN, "execution-1", execution)
	require.Equal(t, http.StatusCreated, status, got)
	status, got = s.introspect(t, s.hostedTLS, authorityOf(t, got).Authority)
	require.Equal(t, http.StatusOK, status, got)
	b = bindingOf(t, got)
	require.NotNil(t, b.Assignment, "an execution read binds where the agent runs")
	assert.Equal(t, s.controller.String(), b.Assignment.OperatorGrn)
	assert.Equal(t, execution, *b.RequestTarget)
}

// TestConsoleAuthority_EachReadNeedsItsOwnDelegation holds that a grant
// covering create authorizes no read: no route inherits a create
// delegation. The replacement grant that lists the read is the
// control, minting the same request.
func TestConsoleAuthority_EachReadNeedsItsOwnDelegation(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	d := s.grant(t, "g-1", nil, "agent:create", "agent:configure")

	status, got := s.mintTarget(t, s.owner, "agent-template:read", s.acme.String(), "read-1", "/v1/orgs/acme/templates")
	assert.Equal(t, http.StatusForbidden, status, "a create and configure grant covers no read: %s", got)
	s.grant(t, "g-2", &d.GrantId, "agent:create", "agent:configure", "agent-template:read")
	status, got = s.mintTarget(t, s.owner, "agent-template:read", s.acme.String(), "read-1", "/v1/orgs/acme/templates")
	assert.Equal(t, http.StatusCreated, status, got)
	status, got = s.mintTarget(t, s.owner, "agent-template:publish", s.acme.String(), "publish-1", "/v1/orgs/acme/templates/web/versions")
	assert.Equal(t, http.StatusForbidden, status, "the read grant covers no publication: %s", got)
}

// TestConsoleAuthority_RechecksBeforeTheOperatorAnswers holds that a
// read authority is decided again from current state each time the
// Operator introspects it, before it may answer: another audience, a
// removed membership, a policy Deny, an expired delegation and a
// withdrawn one each refuse. The owner's authority, which the first
// three do not reach, is the control for them, and each authority
// answered just before the change that refuses it.
func TestConsoleAuthority_RechecksBeforeTheOperatorAnswers(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	d := s.grant(t, "g-1", nil, consoleOperations...)
	minted := func(cookie *http.Cookie, requestID string) string {
		status, got := s.mintTarget(t, cookie, "agent-template:read", s.acme.String(), requestID, "/v1/orgs/acme/templates")
		require.Equal(t, http.StatusCreated, status, got)
		a := authorityOf(t, got).Authority
		status, got = s.introspect(t, s.hostedTLS, a)
		require.Equal(t, http.StatusOK, status, got)
		return a
	}
	byMember, byOwner := minted(s.member, "member-1"), minted(s.owner, "owner-1")
	status, _ := s.introspect(t, s.controlTLS, byOwner)
	assert.Equal(t, http.StatusNotFound, status, "another audience learns nothing")

	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.RemoveMember(ctx, s.acme, s.memberGRN))
	status, _ = s.introspect(t, s.hostedTLS, byMember)
	assert.Equal(t, http.StatusForbidden, status, "the member was removed")
	status, got := s.introspect(t, s.hostedTLS, byOwner)
	assert.Equal(t, http.StatusOK, status, "the owner is still a member: %s", got)
	require.NoError(t, orgs.AddMember(ctx, org.NewMembership(s.acme, s.memberGRN, org.RoleMember)))
	status, got = s.introspect(t, s.hostedTLS, byMember)
	require.Equal(t, http.StatusOK, status, "membership restored, so the refusal above was its: %s", got)

	denied := activePolicy(t, policies, s.acme, "deny-template-read",
		statementDoc("deny-template-read", wire.EffectDeny, "agent-template:read", s.acme.String()))
	attachPolicy(t, policies, denied, s.memberGRN)
	status, _ = s.introspect(t, s.hostedTLS, byMember)
	assert.Equal(t, http.StatusForbidden, status, "a Deny wins over the member default")
	status, got = s.introspect(t, s.hostedTLS, byOwner)
	assert.Equal(t, http.StatusOK, status, "the Deny reaches only the member: %s", got)

	_, err := p.Exec(ctx, `UPDATE operator_delegations SET expires_at = now() - interval '1 second' WHERE id = $1`, d.GrantId)
	require.NoError(t, err)
	status, _ = s.introspect(t, s.hostedTLS, byOwner)
	assert.Equal(t, http.StatusForbidden, status, "the delegation expired")

	d2 := s.grant(t, "g-2", &d.GrantId, consoleOperations...)
	again := minted(s.owner, "owner-3")
	status, got = call(t, http.DefaultClient, http.MethodDelete, s.delegationPath(), s.owner, `{"grantId":"`+d2.GrantId+`"}`)
	require.Equal(t, http.StatusNoContent, status, got)
	status, _ = s.introspect(t, s.hostedTLS, again)
	assert.Equal(t, http.StatusForbidden, status, "the delegation was withdrawn")
}

// plain sends one unversioned browser request and answers the status
// and body.
func plain(t *testing.T, method, url string, cookie *http.Cookie, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
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

// TestControlEndpoint_IsTheDelegatingActNotARename holds that the
// endpoint receiving operation authority moves only under
// operator:delegate. A member a policy lets rename the operator renames
// it, which is the control, and cannot set or clear its endpoint; the
// rename leaves the endpoint where the owner put it, and moving the
// endpoint moves no audience an authority already names.
func TestControlEndpoint_IsTheDelegatingActNotARename(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newAuthorityStack(t, engine, p)
	s.grant(t, "g-1", nil, consoleOperations...)
	endpointPath := s.browser + "/orgs/acme/operators/hosted/control-endpoint"
	operatorPath := s.browser + "/orgs/acme/operators/hosted"

	renamer := activePolicy(t, policies, s.acme, "rename-operators",
		statementDoc("rename-operators", wire.EffectAllow, org.ActionOperatorUpdate, s.acme.String()))
	attachPolicy(t, policies, renamer, s.memberGRN)
	status, got := plain(t, http.MethodPatch, operatorPath, s.member, `{"name":"Hosted control"}`)
	require.Equal(t, http.StatusOK, status, "the policy lets the member rename: %s", got)
	status, got = call(t, http.DefaultClient, http.MethodPut, endpointPath, s.member, `{"url":"https://evil.example.com"}`)
	assert.Equal(t, http.StatusForbidden, status, "renaming is not the delegating act: %s", got)

	status, got = s.mintTarget(t, s.member, "agent-template:read", s.acme.String(), "read-1", "/v1/orgs/acme/templates")
	require.Equal(t, http.StatusCreated, status, got)
	before := authorityOf(t, got)

	status, got = call(t, http.DefaultClient, http.MethodPut, endpointPath, s.owner, `{"url":"https://control.example.com/garam/"}`)
	assert.Equal(t, http.StatusBadRequest, status, "a trailing slash: %s", got)
	status, got = call(t, http.DefaultClient, http.MethodPut, endpointPath, s.owner, `{"url":"https://control.example.com/garam"}`)
	require.Equal(t, http.StatusOK, status, got)
	var o apigen.Operator
	require.NoError(t, json.Unmarshal([]byte(got), &o))
	require.NotNil(t, o.ControlEndpoint)
	assert.Equal(t, "https://control.example.com/garam", *o.ControlEndpoint)

	status, got = plain(t, http.MethodPatch, operatorPath, s.member, `{"name":"Renamed again"}`)
	require.Equal(t, http.StatusOK, status, got)
	status, got = plain(t, http.MethodGet, operatorPath, s.member, "")
	require.Equal(t, http.StatusOK, status, got)
	o = apigen.Operator{}
	require.NoError(t, json.Unmarshal([]byte(got), &o))
	assert.Equal(t, "Renamed again", o.Name)
	require.NotNil(t, o.ControlEndpoint, "a rename leaves the endpoint")
	assert.Equal(t, "https://control.example.com/garam", *o.ControlEndpoint)

	status, got = s.introspect(t, s.hostedTLS, before.Authority)
	require.Equal(t, http.StatusOK, status, got)
	assert.Equal(t, s.hosted.String(), bindingOf(t, got).AudienceGrn, "the endpoint moved no audience")
	status, got = call(t, s.hostedTLS, http.MethodGet, s.machine+"/operation-references/"+before.OperationRef, nil, "")
	require.Equal(t, http.StatusOK, status, got)
	assert.Equal(t, s.hosted.String(), bindingOf(t, got).AudienceGrn)

	status, got = call(t, http.DefaultClient, http.MethodDelete, endpointPath, s.member, "")
	assert.Equal(t, http.StatusForbidden, status, "a member clears nothing: %s", got)
	status, got = call(t, http.DefaultClient, http.MethodDelete, endpointPath, s.owner, "")
	require.Equal(t, http.StatusOK, status, got)
	o = apigen.Operator{}
	require.NoError(t, json.Unmarshal([]byte(got), &o))
	assert.Nil(t, o.ControlEndpoint, "cleared")
}
