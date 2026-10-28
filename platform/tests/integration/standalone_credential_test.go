package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/operator"
)

// credentialed is an enrollment's answer.
type credentialed struct {
	agent, supervisor grn.GRN
	credential        string
}

func credentialOf(t *testing.T, got reply) credentialed {
	t.Helper()
	require.Contains(t, []int{http.StatusCreated, http.StatusOK}, got.status, got.body)
	var e machinegen.StandaloneEnrollment
	require.NoError(t, json.Unmarshal([]byte(got.body), &e))
	assert.NotContains(t, got.body, "certificatePem", "an enrollment signs nothing")
	assert.NotContains(t, got.body, "issuerPem")
	assert.NotEmpty(t, e.ServerRootPem)
	require.True(t, agent.WellFormedSupervisorSecret(e.Credential, agent.SupervisorCredentialPrefix))
	a, err := grn.Parse(e.Grn)
	require.NoError(t, err)
	sup, err := grn.Parse(e.SupervisorGrn)
	require.NoError(t, err)
	return credentialed{agent: a, supervisor: sup, credential: e.Credential}
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
	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.approval(t, time.Now().Add(time.Hour), nil))
	require.Equal(t, http.StatusCreated, status, body)

	c := credentialOf(t, s.spend(t, h.secret, h.enrollment(t)))
	status, token := s.supervisorExchange(t, c.credential, `{}`)
	require.Equal(t, http.StatusCreated, status)
	require.True(t, agent.WellFormedSupervisorSecret(token, agent.SupervisorTokenPrefix))
	assert.Equal(t, http.StatusOK, s.identityStatus(t, token), "the supervisor's token authenticates")
	status, _ = s.supervisorExchange(t, c.credential, `{"audience":"`+s.hosted.String()+`"}`)
	assert.Equal(t, http.StatusBadRequest, status, "a supervisor's token names no audience")

	shown := s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/tokens", operator.TokenContractVersion, token, "",
		`{"audience":"`+s.hosted.String()+`"}`)
	assert.Equal(t, http.StatusBadRequest, shown.status, "a supervisor mints for garam alone: %s", shown.body)
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
	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.approval(t, time.Now().Add(time.Hour), nil))
	require.Equal(t, http.StatusCreated, status, body)

	first := credentialOf(t, s.spend(t, h.secret, h.enrollment(t)))
	_, firstToken := s.supervisorExchange(t, first.credential, `{}`)
	require.Equal(t, http.StatusOK, s.identityStatus(t, firstToken))

	again := s.spend(t, h.secret, h.enrollment(t))
	assert.Equal(t, http.StatusOK, again.status, again.body)
	second := credentialOf(t, again)
	assert.Equal(t, first.agent, second.agent, "the same agent")
	assert.NotEqual(t, first.credential, second.credential, "a new credential")
	assert.Equal(t, http.StatusUnauthorized, s.identityStatus(t, firstToken), "the earlier credential's token is refused")
	status, _ = s.supervisorExchange(t, first.credential, `{}`)
	assert.Equal(t, http.StatusUnauthorized, status, "the earlier credential exchanges nothing")
	status, secondToken := s.supervisorExchange(t, second.credential, `{}`)
	require.Equal(t, http.StatusCreated, status, "control: the re-issued credential exchanges")

	changed := s.spend(t, h.secret, `{"requestId":"`+h.requestID+`" }`)
	assert.Equal(t, http.StatusConflict, changed.status, "another body re-issues nothing: %s", changed.body)
	assert.Equal(t, http.StatusOK, s.identityStatus(t, secondToken), "and voids nothing")

	status, body = s.browse(t, http.MethodDelete, "/orgs/acme/agent-supervisors/"+first.supervisor.Identifier, s.owner, "")
	require.Equal(t, http.StatusNoContent, status, body)
	assert.Equal(t, http.StatusUnauthorized, s.identityStatus(t, secondToken), "a revocation refuses the token")
	status, _ = s.supervisorExchange(t, second.credential, `{}`)
	assert.Equal(t, http.StatusUnauthorized, status, "and the credential")
	after := s.spend(t, h.secret, h.enrollment(t))
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
	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.approval(t, time.Now().Add(time.Hour), nil))
	require.Equal(t, http.StatusCreated, status, body)
	var e machinegenAgentEnrollment
	require.NoError(t, json.Unmarshal([]byte(body), &e))
	credentialOf(t, s.spend(t, h.secret, h.enrollment(t)))

	lock := `SELECT 1 FROM agent_enrollments WHERE id = $1 FOR UPDATE`
	anonymous := anonymousMachineClient(t)
	a, b := s.queuedBehind(t, lock, e.ID, s.spending(anonymous, h, h.enrollment(t)),
		s.spending(anonymous, h, h.enrollment(t)))
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

// enrolledSupervisor approves a keyless host, spends it under v2 and
// answers the enrollment with the supervisor's token.
func (s *tokenStack) enrolledSupervisor(t *testing.T) (credentialed, string) {
	t.Helper()
	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.approval(t, time.Now().Add(time.Hour), nil))
	require.Equal(t, http.StatusCreated, status, body)
	c := credentialOf(t, s.spend(t, h.secret, h.enrollment(t)))
	status, token := s.supervisorExchange(t, c.credential, `{}`)
	require.Equal(t, http.StatusCreated, status)
	return c, token
}

// TestStandaloneCredential_ASupervisorReachesItsOwnAgentAlone holds a
// supervisor's token to the one agent it holds: at another
// supervisor's agent it mints nothing and activates nothing, while the
// same calls at its own agent are the control.
func TestStandaloneCredential_ASupervisorReachesItsOwnAgentAlone(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := &tokenStack{executionStack: newExecutionStack(t, p, engine)}
	mine, token := s.enrolledSupervisor(t)
	theirs, theirToken := s.enrolledSupervisor(t)

	mint := func(g grn.GRN) reply {
		return s.v2(t, http.MethodPost, "/agents/"+g.String()+"/tokens", operator.TokenContractVersion, token, "", `{}`)
	}
	assert.Equal(t, http.StatusForbidden, mint(theirs.agent).status, "another supervisor's agent")
	own := mint(mine.agent)
	require.Equal(t, http.StatusCreated, own.status, "control: its own agent: %s", own.body)
	var minted machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(own.body), &minted))

	got := s.v2(t, http.MethodPost, "/agents/"+theirs.agent.String()+"/tokens", operator.TokenContractVersion, theirToken, "", `{}`)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var theirMinted machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(got.body), &theirMinted))
	activate := func(g grn.GRN, forwarded string) reply {
		return s.v2(t, http.MethodPost, "/agents/"+g.String()+"/activations", agent.TokenExecutionContractVersion, token, "",
			tokenActivationBody("first", generation(1), nil, nil, forwarded))
	}
	assert.Equal(t, http.StatusForbidden, activate(theirs.agent, theirMinted.Token).status,
		"another supervisor's agent activates nothing")
	assert.Equal(t, http.StatusCreated, activate(mine.agent, minted.Token).status, "control: its own agent activates")

}

// TestStandaloneCredential_AReplacementEndsTheRetryWindow holds a v2
// retry to the registration it re-issues for: answered while the
// approval's supervisor holds the agent, and refused once a
// replacement has moved the agent to another, which writes nothing.
func TestStandaloneCredential_AReplacementEndsTheRetryWindow(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := &tokenStack{executionStack: newExecutionStack(t, p, engine)}
	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.approval(t, time.Now().Add(time.Hour), nil))
	require.Equal(t, http.StatusCreated, status, body)
	first := credentialOf(t, s.spend(t, h.secret, h.enrollment(t)))
	again := s.spend(t, h.secret, h.enrollment(t))
	require.Equal(t, http.StatusOK, again.status, "control: a retry before the replacement: %s", again.body)
	current := credentialOf(t, again)

	next := newStandaloneHost(t)
	var approval map[string]any
	require.NoError(t, json.Unmarshal([]byte(next.approval(t, time.Now().Add(time.Hour), nil)), &approval))
	approval["replaces"] = map[string]any{"agentGrn": first.agent.String(), "assigneeGrn": first.supervisor.String(), "epoch": 1}
	raw, err := json.Marshal(approval)
	require.NoError(t, err)
	status, body = s.approve(t, s.owner, "acme", string(raw))
	require.Equal(t, http.StatusCreated, status, body)
	replaced := credentialOf(t, s.spend(t, next.secret, next.enrollment(t)))
	require.Equal(t, first.agent, replaced.agent, "the replacement holds the same agent")

	after := s.spend(t, h.secret, h.enrollment(t))
	assert.Equal(t, http.StatusConflict, after.status, "the replaced registration's retry: %s", after.body)
	assert.NotContains(t, after.body, agent.SupervisorCredentialPrefix)
	status, _ = s.supervisorExchange(t, current.credential, `{}`)
	assert.Equal(t, http.StatusUnauthorized, status, "and its credential exchanges nothing")
}

// TestStandaloneCredential_AReplacementAnswersTheActivationItsFirstActivationNames
// holds a replacement onto a new host whose supervisor has never
// activated. The spending answers the agent's most recent activation,
// read under the execution lock the move held, and its replay answers
// the same one. The new supervisor's first activation naming it is
// activated, where one naming none is refused as superseded, which is
// all a fresh supervisor could name before.
func TestStandaloneCredential_AReplacementAnswersTheActivationItsFirstActivationNames(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := &tokenStack{executionStack: newExecutionStack(t, p, engine)}
	h := newStandaloneHost(t)
	status, body := s.approve(t, s.owner, "acme", h.approval(t, time.Now().Add(time.Hour), nil))
	require.Equal(t, http.StatusCreated, status, body)
	first := credentialOf(t, s.spend(t, h.secret, h.enrollment(t)))
	activated := s.supervisorActivates(t, first, tokenActivationBody("first", generation(1), nil, nil, ""))
	require.Equal(t, http.StatusCreated, activated.status, activated.body)
	ended := activationOf(t, activated).ActivationId

	next := newStandaloneHost(t)
	var approval map[string]any
	require.NoError(t, json.Unmarshal([]byte(next.approval(t, time.Now().Add(time.Hour), nil)), &approval))
	approval["replaces"] = map[string]any{"agentGrn": first.agent.String(), "assigneeGrn": first.supervisor.String(), "epoch": 1}
	raw, err := json.Marshal(approval)
	require.NoError(t, err)
	status, body = s.approve(t, s.owner, "acme", string(raw))
	require.Equal(t, http.StatusCreated, status, body)
	spent := s.spend(t, next.secret, next.enrollment(t))
	replaced := credentialOf(t, spent)
	var answer machinegen.StandaloneEnrollment
	require.NoError(t, json.Unmarshal([]byte(spent.body), &answer))
	require.NotNil(t, answer.ReplacesActivationId, "a replacement answers the activation it found")
	assert.Equal(t, ended, answer.ReplacesActivationId.String())
	replay := s.spend(t, next.secret, next.enrollment(t))
	require.NoError(t, json.Unmarshal([]byte(replay.body), &answer))
	require.NotNil(t, answer.ReplacesActivationId)
	assert.Equal(t, ended, answer.ReplacesActivationId.String(), "a replay answers the same one")
	replaced = credentialOf(t, replay)

	activation := func(requestID string, replaces *string) string {
		raw, err := json.Marshal(map[string]any{
			"requestId": requestID, "epoch": "2", "generation": string(generation(2)),
			"replacesActivationId": replaces, "operationRef": nil,
		})
		require.NoError(t, err)
		return string(raw)
	}
	got := s.supervisorActivates(t, replaced, activation("unanchored", nil))
	assert.Equal(t, http.StatusConflict, got.status, "a first activation naming nothing: %s", got.body)
	got = s.supervisorActivates(t, replaced, activation("anchored", &ended))
	assert.Equal(t, http.StatusCreated, got.status, "the control: naming the activation answered: %s", got.body)
}

// supervisorActivates has c's supervisor activate its agent, forwarding
// an agent token it mints for garam in body's token member.
func (s *tokenStack) supervisorActivates(t *testing.T, c credentialed, body string) reply {
	t.Helper()
	status, token := s.supervisorExchange(t, c.credential, `{}`)
	require.Equal(t, http.StatusCreated, status)
	minted := s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/tokens", operator.TokenContractVersion, token, "", `{}`)
	require.Equal(t, http.StatusCreated, minted.status, minted.body)
	var own machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(minted.body), &own))
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &fields))
	fields["token"] = own.Token
	raw, err := json.Marshal(fields)
	require.NoError(t, err)
	return s.v2(t, http.MethodPost, "/agents/"+c.agent.String()+"/activations", agent.TokenExecutionContractVersion, token, "",
		string(raw))
}
