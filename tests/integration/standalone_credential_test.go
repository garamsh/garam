package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// credentialApproval is the body an administrator approves a host
// enrolling under standalone-enrollment.v2 with: no key fingerprint.
func (h standaloneHost) credentialApproval(t *testing.T) string {
	t.Helper()
	secret := sha256.Sum256([]byte(h.secret))
	raw, err := json.Marshal(map[string]any{
		"requestId": h.requestID, "hostName": "laptop", "expiresAt": time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano),
		"secretSha256": hex.EncodeToString(secret[:]),
	})
	require.NoError(t, err)
	return string(raw)
}

// credentialEnrollment is the exact body h spends a keyless approval
// with.
func (h standaloneHost) credentialEnrollment() string { return `{"requestId":"` + h.requestID + `"}` }

// spendV2 sends one standalone-enrollment.v2 spending, whose every
// answer names that value.
func (s *executionStack) spendV2(t *testing.T, secret, body string) reply {
	t.Helper()
	got, err := s.spendingV2(anonymousMachineClient(t), secret, body)()
	require.NoError(t, err)
	assert.Equal(t, agent.CredentialStandaloneContractVersion, got.version, "every answer names the value asked for")
	return got
}

func (s *executionStack) spendingV2(client *http.Client, secret, body string) func() (reply, error) {
	return func() (reply, error) {
		return s.exchange(client, http.MethodPost, "/agent-enrollments",
			agent.CredentialStandaloneContractVersion, "Garam-Bootstrap "+secret, body)
	}
}

// credentialed is a v2 enrollment's answer.
type credentialed struct {
	agent, supervisor grn.GRN
	credential        string
}

func credentialOf(t *testing.T, got reply) credentialed {
	t.Helper()
	require.Contains(t, []int{http.StatusCreated, http.StatusOK}, got.status, got.body)
	var e machinegen.StandaloneEnrollment
	require.NoError(t, json.Unmarshal([]byte(got.body), &e))
	assert.Nil(t, e.SupervisorCertificatePem, "a v2 enrollment signs nothing")
	assert.Nil(t, e.AgentCertificatePem)
	assert.Nil(t, e.IssuerPem)
	assert.NotEmpty(t, e.ServerRootPem)
	require.NotNil(t, e.Credential)
	require.True(t, agent.WellFormedSupervisorSecret(*e.Credential, agent.SupervisorCredentialPrefix))
	a, err := grn.Parse(e.Grn)
	require.NoError(t, err)
	sup, err := grn.Parse(e.SupervisorGrn)
	require.NoError(t, err)
	return credentialed{agent: a, supervisor: sup, credential: *e.Credential}
}

// supervisorExchange exchanges credential for a supervisor token and
// answers the status and the token.
func (s *tokenStack) supervisorExchange(t *testing.T, credential, body string) (int, string) {
	t.Helper()
	got := s.tokenSend(t, http.MethodPost, "/tokens", map[string]string{
		"Authorization": "Garam-Credential " + credential, operator.ContractVersionHeader: operator.TokenContractVersion,
	}, body)
	if got.status != http.StatusCreated {
		return got.status, ""
	}
	var token machinegen.MachineToken
	require.NoError(t, json.Unmarshal([]byte(got.body), &token))
	return got.status, token.Token
}

func (s *tokenStack) identityStatus(t *testing.T, token string) int {
	t.Helper()
	return s.tokenSend(t, http.MethodGet, "/identity", map[string]string{"Authorization": "Garam-Token " + token}, "").status
}

// TestStandaloneCredential_ASupervisorRunsItsAgentOnTokensAlone is G1d
// end to end against Postgres and the listener `serve machine`
// assembles: an approval binding no key is spent under
// standalone-enrollment.v2 for a credential, the supervisor exchanges
// it for a token, mints its agent's token, activates the agent
// forwarding that token, and the agent claims under the activation. No
// certificate is presented anywhere. The supervisor's token reaches no
// route outside its own, beside an identity read as the control.
func TestStandaloneCredential_ASupervisorRunsItsAgentOnTokensAlone(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := &tokenStack{executionStack: newExecutionStack(t, p, engine)}
	s.client, _ = tokenClient(t)
	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.credentialApproval(t))
	require.Equal(t, http.StatusCreated, status, body)

	c := credentialOf(t, s.spendV2(t, h.secret, h.credentialEnrollment()))
	assert.Equal(t, 0, countRows(t, s.pool, `SELECT count(*) FROM agent_credentials WHERE agent_grn = $1`, c.agent.String()),
		"no certificate of the agent is recorded")
	status, token := s.supervisorExchange(t, c.credential, `{}`)
	require.Equal(t, http.StatusCreated, status)
	require.True(t, agent.WellFormedSupervisorSecret(token, agent.SupervisorTokenPrefix))
	assert.Equal(t, http.StatusOK, s.identityStatus(t, token), "the supervisor's token authenticates")
	status, _ = s.supervisorExchange(t, c.credential, `{"audience":"`+s.hosted.String()+`"}`)
	assert.Equal(t, http.StatusBadRequest, status, "a supervisor's token names no audience")

	minted := s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/tokens", operator.TokenContractVersion, token, "", `{}`)
	require.Equal(t, http.StatusCreated, minted.status, "the supervisor mints its agent's token: %s", minted.body)
	var own machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(minted.body), &own))

	got := s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/activations", agent.TokenExecutionContractVersion, token, "",
		tokenActivationBody("first", generation(1), nil, nil, own.Token))
	require.Equal(t, http.StatusCreated, got.status, "the first activation rests on the approval: %s", got.body)
	activation := activationOf(t, got)
	assert.Equal(t, http.StatusNoContent, s.claimWith(t, c.agent, own.Token, activation.Token),
		"the agent claims under the activation with its own token")
	assert.Equal(t, machinegen.ExecutionIntrospectionCredential("current"), s.supervisorStanding(t, c.agent, token, own.Token).Credential)

	refused := s.tokenSend(t, http.MethodPost, "/messages", map[string]string{"Authorization": "Garam-Token " + token},
		submissionTo(c.agent, s.key()))
	assert.Equal(t, http.StatusForbidden, refused.status, "a supervisor posts nothing: %s", refused.body)
}

// supervisorStanding is execution-fence.v2 introspection by the
// supervisor of the token it forwards.
func (s *tokenStack) supervisorStanding(t *testing.T, g grn.GRN, supervisorToken, forwarded string) machinegen.ExecutionIntrospection {
	t.Helper()
	got := s.v2(t, http.MethodPost, "/agents/"+g.String()+"/execution/introspection", agent.TokenExecutionContractVersion,
		supervisorToken, "", `{"token":"`+forwarded+`"}`)
	require.Equal(t, http.StatusOK, got.status, got.body)
	var in machinegen.ExecutionIntrospection
	require.NoError(t, json.Unmarshal([]byte(got.body), &in))
	return in
}

// TestStandaloneCredential_EachContractRefusesTheOthersApproval holds
// the rule the approval's keys decide: one binding the host's keys is
// spent under v1 alone, and one binding none under v2 alone. Each
// refusal spends nothing, which the same approval spent under its own
// value afterwards shows.
func TestStandaloneCredential_EachContractRefusesTheOthersApproval(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)

	keyless := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", keyless.credentialApproval(t))
	require.Equal(t, http.StatusCreated, status, body)
	got := s.spend(t, keyless.secret, keyless.enrollment(t))
	assert.Equal(t, http.StatusBadRequest, got.status, "v1 refuses an approval binding no key: %s", got.body)
	assert.Contains(t, got.body, agent.CredentialStandaloneContractVersion)
	credentialOf(t, s.spendV2(t, keyless.secret, keyless.credentialEnrollment()))

	keyed := newStandaloneHost(t)
	s.approved(t, keyed, nil)
	got = s.spendV2(t, keyed.secret, keyed.credentialEnrollment())
	assert.Equal(t, http.StatusBadRequest, got.status, "v2 refuses an approval binding the host's keys: %s", got.body)
	assert.Contains(t, got.body, agent.StandaloneContractVersion)
	s.enrolled(t, keyed, s.spend(t, keyed.secret, keyed.enrollment(t)))

	half := newStandaloneHost(t)
	var approval map[string]any
	require.NoError(t, json.Unmarshal([]byte(half.approval(t, time.Now().Add(time.Hour), nil)), &approval))
	delete(approval, "agentKeySha256")
	raw, err := json.Marshal(approval)
	require.NoError(t, err)
	status, body = s.approve(t, s.owner, "acme", string(raw))
	assert.Equal(t, http.StatusBadRequest, status, "one fingerprint binds no contract: %s", body)
}

// TestStandaloneCredential_ARetryReissuesTheCredentialAndVoidsTheLast
// holds the retry garam answers where it keeps only a hash: an
// identical v2 spending re-issues the credential, and the one answered
// before, with the token exchanged for it, is refused from the next
// request over a connection already open. A revocation ends the
// window: the retry is refused, and so is the current credential.
func TestStandaloneCredential_ARetryReissuesTheCredentialAndVoidsTheLast(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := &tokenStack{executionStack: newExecutionStack(t, p, engine)}
	s.client, _ = tokenClient(t)
	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.credentialApproval(t))
	require.Equal(t, http.StatusCreated, status, body)

	first := credentialOf(t, s.spendV2(t, h.secret, h.credentialEnrollment()))
	_, firstToken := s.supervisorExchange(t, first.credential, `{}`)
	require.Equal(t, http.StatusOK, s.identityStatus(t, firstToken))

	again := s.spendV2(t, h.secret, h.credentialEnrollment())
	assert.Equal(t, http.StatusOK, again.status, again.body)
	second := credentialOf(t, again)
	assert.Equal(t, first.agent, second.agent, "the same agent")
	assert.NotEqual(t, first.credential, second.credential, "a new credential")
	assert.Equal(t, http.StatusUnauthorized, s.identityStatus(t, firstToken), "the earlier credential's token is refused")
	status, _ = s.supervisorExchange(t, first.credential, `{}`)
	assert.Equal(t, http.StatusUnauthorized, status, "the earlier credential exchanges nothing")
	status, secondToken := s.supervisorExchange(t, second.credential, `{}`)
	require.Equal(t, http.StatusCreated, status, "control: the re-issued credential exchanges")

	changed := s.spendV2(t, h.secret, `{"requestId":"`+h.requestID+`" }`)
	assert.Equal(t, http.StatusConflict, changed.status, "another body re-issues nothing: %s", changed.body)
	assert.Equal(t, http.StatusOK, s.identityStatus(t, secondToken), "and voids nothing")

	status, body = s.browse(t, http.MethodDelete, "/orgs/acme/agent-supervisors/"+first.supervisor.Identifier, s.owner, "")
	require.Equal(t, http.StatusNoContent, status, body)
	assert.Equal(t, http.StatusUnauthorized, s.identityStatus(t, secondToken), "a revocation refuses the token")
	status, _ = s.supervisorExchange(t, second.credential, `{}`)
	assert.Equal(t, http.StatusUnauthorized, status, "and the credential")
	after := s.spendV2(t, h.secret, h.credentialEnrollment())
	assert.Equal(t, http.StatusConflict, after.status, "the retry window ends with the registration: %s", after.body)
	assert.NotContains(t, after.body, agent.SupervisorCredentialPrefix)
}

// TestStandaloneCredential_ConcurrentRetriesLeaveOneLiveCredential
// queues two identical retries behind the approval's row: each
// re-issues in turn, so one credential row stands and exactly one of
// the two answered credentials exchanges.
func TestStandaloneCredential_ConcurrentRetriesLeaveOneLiveCredential(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := &tokenStack{executionStack: newExecutionStack(t, p, engine)}
	s.client, _ = tokenClient(t)
	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.credentialApproval(t))
	require.Equal(t, http.StatusCreated, status, body)
	var e machinegenAgentEnrollment
	require.NoError(t, json.Unmarshal([]byte(body), &e))
	credentialOf(t, s.spendV2(t, h.secret, h.credentialEnrollment()))

	lock := `SELECT 1 FROM agent_enrollments WHERE id = $1 FOR UPDATE`
	anonymous := anonymousMachineClient(t)
	a, b := s.queuedBehind(t, lock, e.ID, s.spendingV2(anonymous, h.secret, h.credentialEnrollment()),
		s.spendingV2(anonymous, h.secret, h.credentialEnrollment()))
	first, second := credentialOf(t, a), credentialOf(t, b)
	require.NotEqual(t, first.credential, second.credential)
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM supervisor_credentials WHERE supervisor_grn = $1`, e.SupervisorGrn))
	live := 0
	for _, c := range []string{first.credential, second.credential} {
		if status, _ := s.supervisorExchange(t, c, `{}`); status == http.StatusCreated {
			live++
		}
	}
	assert.Equal(t, 1, live, "exactly one answered credential is live")
}
