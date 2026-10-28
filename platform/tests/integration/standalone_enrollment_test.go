package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// standaloneHost is what a standalone host prepares before asking for
// an approval (ADR-0082, ADR-0100): a bootstrap secret and a request
// identifier. Only the secret's hash reaches the administrator, and no
// key is bound (ADR-0118).
type standaloneHost struct {
	secret    string
	requestID string
}

func newStandaloneHost(t *testing.T) standaloneHost {
	t.Helper()
	raw := make([]byte, 32)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	return standaloneHost{secret: base64.RawURLEncoding.EncodeToString(raw), requestID: "host-" + hex.EncodeToString(raw[:6])}
}

// approval is the body an administrator approves h with. replaces is
// nil for a new agent.
func (h standaloneHost) approval(t *testing.T, expiresAt time.Time, replaces map[string]any) string {
	t.Helper()
	secret := sha256.Sum256([]byte(h.secret))
	body := map[string]any{
		"requestId": h.requestID, "hostName": "laptop", "expiresAt": expiresAt.UTC().Format(time.RFC3339Nano),
		"secretSha256": hex.EncodeToString(secret[:]),
	}
	if replaces != nil {
		body["replaces"] = replaces
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return string(raw)
}

// enrollment is the exact body h spends its approval with.
func (h standaloneHost) enrollment(t *testing.T) string {
	t.Helper()
	return `{"requestId":"` + h.requestID + `"}`
}

// browse makes one browser request as cookie's user.
func (s *executionStack) browse(t *testing.T, method, path string, cookie *http.Cookie, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, s.browser+path, strings.NewReader(body))
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

func (s *executionStack) approve(t *testing.T, cookie *http.Cookie, org, body string) (int, string) {
	t.Helper()
	return s.browse(t, http.MethodPost, "/orgs/"+org+"/agent-enrollments", cookie, body)
}

// approved approves h as acme's owner and answers the approval.
func (s *executionStack) approved(t *testing.T, h standaloneHost, replaces map[string]any) machinegenAgentEnrollment {
	t.Helper()
	status, body := s.approve(t, s.owner, "acme", h.approval(t, time.Now().Add(time.Hour), replaces))
	require.Equal(t, http.StatusCreated, status, body)
	var e machinegenAgentEnrollment
	require.NoError(t, json.Unmarshal([]byte(body), &e))
	return e
}

// machinegenAgentEnrollment is the browser surface's answer to an
// approval, read here without importing that surface's package twice.
type machinegenAgentEnrollment struct {
	ID            string `json:"id"`
	AgentGrn      string `json:"agentGrn"`
	SupervisorGrn string `json:"supervisorGrn"`
	Spent         bool   `json:"spent"`
}

// spend sends one standalone-enrollment.v2 spending, whose every
// answer names that value. A client presenting nothing is what a host
// holding no credential yet has.
func (s *executionStack) spend(t *testing.T, secret, body string) reply {
	t.Helper()
	got, err := s.exchange(anonymousMachineClient(t), http.MethodPost, "/agent-enrollments",
		agent.CredentialStandaloneContractVersion, "Garam-Bootstrap "+secret, body)
	require.NoError(t, err)
	assert.Equal(t, agent.CredentialStandaloneContractVersion, got.version, "every standalone answer names its contract")
	return got
}

// standaloneAgent is a host after its enrollment: its agent and
// supervisor, the supervisor's credential, a client presenting the
// token the supervisor exchanged it for, and the agent's token for
// garam, which the supervisor minted and forwards on activation, with
// a client presenting it.
type standaloneAgent struct {
	agent         grn.GRN
	supervisor    grn.GRN
	epoch         int
	credential    string
	supervisorTLS *http.Client
	agentTLS      *http.Client
	agentToken    string
}

func (s *executionStack) enrolled(t *testing.T, _ standaloneHost, got reply) standaloneAgent {
	t.Helper()
	c := credentialOf(t, got)
	var e machinegen.StandaloneEnrollment
	require.NoError(t, json.Unmarshal([]byte(got.body), &e))
	epoch, err := strconv.Atoi(e.Epoch)
	require.NoError(t, err)
	supervisor := s.supervisorToken(t, c.credential)
	minted := s.send(t, tokenPresenting(t, supervisor), http.MethodPost, "/agents/"+c.agent.String()+"/tokens",
		operator.TokenContractVersion, "", `{}`)
	require.Equal(t, http.StatusCreated, minted.status, minted.body)
	var own machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(minted.body), &own))
	return standaloneAgent{
		agent: c.agent, supervisor: c.supervisor, epoch: epoch, credential: c.credential,
		supervisorTLS: tokenPresenting(t, supervisor), agentTLS: tokenPresenting(t, own.Token), agentToken: own.Token,
	}
}

// supervisorToken exchanges credential for the supervisor's token.
func (s *executionStack) supervisorToken(t *testing.T, credential string) string {
	t.Helper()
	got := s.send(t, s.client, http.MethodPost, "/tokens", operator.TokenContractVersion, "Garam-Credential "+credential, `{}`)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var token machinegen.MachineToken
	require.NoError(t, json.Unmarshal([]byte(got.body), &token))
	return token.Token
}

// standaloneOf enrolls a new standalone host and points the stack's
// agent at its agent, so the protected-call helpers act on it.
func (s *executionStack) standaloneOf(t *testing.T) (standaloneHost, standaloneAgent) {
	t.Helper()
	h := newStandaloneHost(t)
	s.approved(t, h, nil)
	e := s.enrolled(t, h, s.spend(t, h.secret, h.enrollment(t)))
	s.agent, s.epoch = e.agent, e.epoch
	return h, e
}

// TestStandalone_AnApprovedHostEnrollsWithoutAnOperator covers the
// approval and its spending end to end: only an administrator approves,
// only the approved request spends it, a retry answers the agent the
// first did, and what it writes is an agent fenced from birth held by
// its supervisor, with no operator record, grant or managed creation.
func TestStandalone_AnApprovedHostEnrollsWithoutAnOperator(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h := newStandaloneHost(t)
	operatorsBefore := countRows(t, s.pool, `SELECT count(*) FROM operators`)

	status, body := s.approve(t, s.member, "acme", h.approval(t, time.Now().Add(time.Hour), nil))
	assert.Equal(t, http.StatusForbidden, status, "a member holds no agent:enroll: %s", body)
	status, body = s.approve(t, s.owner, "globex", h.approval(t, time.Now().Add(time.Hour), nil))
	assert.Equal(t, http.StatusForbidden, status, "acme's owner approves nothing in globex: %s", body)
	status, body = s.approve(t, s.owner, "acme", h.approval(t, time.Now().Add(48*time.Hour), nil))
	assert.Equal(t, http.StatusBadRequest, status, "an approval lives at most a day: %s", body)
	request := h.approval(t, time.Now().Add(time.Hour), nil)
	status, body = s.approve(t, s.owner, "acme", request)
	require.Equal(t, http.StatusCreated, status, body)
	var approval machinegenAgentEnrollment
	require.NoError(t, json.Unmarshal([]byte(body), &approval))
	assert.False(t, approval.Spent)

	// The identical approval answers the same binding; another request
	// under the same identifier conflicts.
	status, again := s.approve(t, s.owner, "acme", request)
	require.Equal(t, http.StatusOK, status, again)
	assert.JSONEq(t, body, again)
	status, body = s.approve(t, s.owner, "acme", h.approval(t, time.Now().Add(2*time.Hour), nil))
	require.Equal(t, http.StatusConflict, status, "another expiry is another request: %s", body)

	unknown := s.spend(t, "not-the-secret", h.enrollment(t))
	assert.Equal(t, http.StatusUnauthorized, unknown.status, unknown.body)

	first := s.spend(t, h.secret, h.enrollment(t))
	require.Equal(t, http.StatusCreated, first.status, first.body)
	e := s.enrolled(t, h, first)
	assert.Equal(t, approval.AgentGrn, e.agent.String(), "the GRN was bound at approval")
	assert.Equal(t, approval.SupervisorGrn, e.supervisor.String())
	assert.Equal(t, 1, e.epoch)

	retry := s.spend(t, h.secret, h.enrollment(t))
	assert.Equal(t, http.StatusOK, retry.status, retry.body)
	retried := credentialOf(t, retry)
	assert.Equal(t, e.agent, retried.agent, "a retry answers the recorded agent")
	assert.Equal(t, e.supervisor, retried.supervisor)
	other := h
	other.requestID = "another-request"
	changed := s.spend(t, h.secret, other.enrollment(t))
	assert.Equal(t, http.StatusConflict, changed.status, "a spent approval answers its own request alone: %s", changed.body)

	ctx := context.Background()
	var assignee string
	var epoch int
	require.NoError(t, s.pool.QueryRow(ctx,
		`SELECT a.assignee_grn, a.epoch FROM agent_assignments a JOIN agent_executions x USING (agent_grn) WHERE agent_grn = $1`,
		e.agent.String()).Scan(&assignee, &epoch))
	assert.Equal(t, e.supervisor.String(), assignee)
	assert.Equal(t, 1, epoch)
	assert.Equal(t, operatorsBefore, countRows(t, s.pool, `SELECT count(*) FROM operators`), "no operator is registered for it")
	assert.Zero(t, countRows(t, s.pool, `SELECT count(*) FROM agent_managed_creations WHERE agent_grn = $1`, e.agent.String()))
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agents WHERE grn = $1`, e.agent.String()),
		"a retry wrote no second identity")
}

// TestStandalone_TheSupervisorActivatesItsOwnAgentAndNothingElse covers
// the supervisor's lifecycle authority on the existing execution
// routes: it activates, inspects and deactivates its agent's
// generation, and nothing else holds that authority for it — not the
// agent's own token, not the hosted operator, and not the
// supervisor over another agent or for a recovery.
func TestStandalone_TheSupervisorActivatesItsOwnAgentAndNothingElse(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	legacy := s.agent
	_, e := s.standaloneOf(t)

	// Before any activation the agent's protected calls are refused:
	// fenced from birth, a call with no token has nothing to match.
	s.open(t, e.agentTLS)
	allRefused(t, s.protectedCalls(t, e.agentTLS, ""), http.StatusUnauthorized, "no activation yet")

	byRuntime := s.activate(t, e.agentTLS, "a-runtime", generationOf(1), nil, e.agentToken)
	assert.Equal(t, http.StatusForbidden, byRuntime.status, "the agent's own token activates nothing: %s", byRuntime.body)
	byHosted := s.activate(t, s.hostedTLS, "a-hosted", generationOf(1), nil, e.agentToken)
	assert.Equal(t, http.StatusForbidden, byHosted.status, "no grant covers a supervisor's agent: %s", byHosted.body)

	got := s.activate(t, e.supervisorTLS, "a-1", generationOf(1), nil, e.agentToken)
	require.Equal(t, http.StatusCreated, got.status, "the first activation rests on the approval: %s", got.body)
	var a machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	assert.Equal(t, e.agent.String(), a.Grn)
	allAdmitted(t, s.protectedCalls(t, e.agentTLS, a.Token), "the activation's token")

	// The supervisor's token reaches no collaboration or operator route;
	// an agent of the organization posting is the control.
	posted := s.send(t, s.peerTLS, http.MethodPost, "/messages", "", "", submissionTo(e.agent, s.key()))
	assert.Equal(t, http.StatusCreated, posted.status, "the control: an ordinary sender posts: %s", posted.body)
	for _, route := range []struct{ method, path, body string }{
		{http.MethodPost, "/messages", submissionTo(e.agent, s.key())},
		{http.MethodPost, s.agentPath("/claims"), ""},
	} {
		got := s.send(t, e.supervisorTLS, route.method, route.path, "", "", route.body)
		assert.Equal(t, http.StatusForbidden, got.status, "%s %s: %s", route.method, route.path, got.body)
		assert.Contains(t, got.body, "lifecycle routes alone", "%s %s", route.method, route.path)
	}

	in := s.introspect(t, e.supervisorTLS, e.agentToken, &[]agent.Generation{generationOf(1)}[0])
	require.Equal(t, http.StatusOK, in.status, in.body)
	assert.Contains(t, in.body, `"assignee":"`+e.supervisor.String()+`"`)

	s.agent = legacy
	elsewhere := s.activate(t, e.supervisorTLS, "a-other", generationOf(2), nil, s.forwarded)
	assert.Equal(t, http.StatusForbidden, elsewhere.status, "a supervisor holds one agent: %s", elsewhere.body)
	s.agent = e.agent
	raw, err := json.Marshal(map[string]string{"requestId": "x", "epoch": strconv.Itoa(e.epoch)})
	require.NoError(t, err)
	recovery := s.send(t, e.supervisorTLS, http.MethodPost, s.agentPath("/credential-recovery"), agent.TokenExecutionContractVersion,
		"Garam-Operation anything", string(raw))
	assert.Equal(t, http.StatusForbidden, recovery.status, "recovery is an administrator's new approval: %s", recovery.body)

	ended := s.lifecycle(t, e.supervisorTLS, s.agentPath("/activations/"+a.ActivationId+"/deactivation"), "", "")
	require.Equal(t, http.StatusOK, ended.status, ended.body)
	allRefused(t, s.protectedCalls(t, e.agentTLS, a.Token), http.StatusForbidden, "a deactivated token")
}

// TestStandalone_BootstrapRefusals covers each way an approval stops
// being spendable, beside the control that spends a sibling: expiry,
// withdrawal, and the approving administrator losing agent:enroll.
func TestStandalone_BootstrapRefusals(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	ctx := context.Background()

	expired := newStandaloneHost(t)
	s.approved(t, expired, nil)
	_, err := s.pool.Exec(ctx, `UPDATE agent_enrollments SET expires_at = now() - interval '1 second' WHERE request_id = $1`, expired.requestID)
	require.NoError(t, err)
	got := s.spend(t, expired.secret, expired.enrollment(t))
	assert.Equal(t, http.StatusUnauthorized, got.status, "expired: %s", got.body)
	status, body := s.approve(t, s.owner, "acme", expired.approval(t, time.Now().Add(time.Hour), nil))
	assert.Equal(t, http.StatusConflict, status, "an expired approval is not revived: %s", body)

	withdrawn := newStandaloneHost(t)
	w := s.approved(t, withdrawn, nil)
	status, body = s.browse(t, http.MethodDelete, "/orgs/acme/agent-enrollments/"+w.ID, s.member, "")
	assert.Equal(t, http.StatusForbidden, status, "a member withdraws nothing: %s", body)
	status, body = s.browse(t, http.MethodDelete, "/orgs/acme/agent-enrollments/"+w.ID, s.owner, "")
	require.Equal(t, http.StatusNoContent, status, body)
	got = s.spend(t, withdrawn.secret, withdrawn.enrollment(t))
	assert.Equal(t, http.StatusUnauthorized, got.status, "withdrawn: %s", got.body)

	demoted := newStandaloneHost(t)
	s.approved(t, demoted, nil)
	_, err = s.pool.Exec(ctx, `UPDATE org_members SET role = 'member' WHERE org_grn = $1 AND role = 'owner'`, s.acme.String())
	require.NoError(t, err)
	got = s.spend(t, demoted.secret, demoted.enrollment(t))
	assert.Equal(t, http.StatusForbidden, got.status, "the approver no longer holds agent:enroll: %s", got.body)
	_, err = s.pool.Exec(ctx, `UPDATE org_members SET role = 'owner' WHERE org_grn = $1 AND user_grn LIKE '%:ada'`, s.acme.String())
	require.NoError(t, err)

	// The control: the same approval spends once its approver holds
	// the action again, and a spent approval is not withdrawn.
	got = s.spend(t, demoted.secret, demoted.enrollment(t))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var spent machinegenAgentEnrollment
	status, body = s.approve(t, s.owner, "acme", demoted.approval(t, time.Now().Add(time.Hour), nil))
	assert.Equal(t, http.StatusConflict, status, "another expiry is another request: %s", body)
	require.NoError(t, s.pool.QueryRow(ctx, `SELECT id::text FROM agent_enrollments WHERE request_id = $1`, demoted.requestID).Scan(&spent.ID))
	status, body = s.browse(t, http.MethodDelete, "/orgs/acme/agent-enrollments/"+spent.ID, s.owner, "")
	assert.Equal(t, http.StatusConflict, status, "a spent approval is revoked, not withdrawn: %s", body)
}

// TestStandalone_ReplacementFencesTheFormerHolderAndLineage covers an
// administrator replacing a supervisor for the same GRN: the agent
// moves to the next epoch and a new lineage, the former supervisor and
// every earlier agent token stop acting on connections already open, an
// approval of the same assignment made earlier cannot spend over the
// newer one, and the first enrollment no longer replays.
func TestStandalone_ReplacementFencesTheFormerHolderAndLineage(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h, e := s.standaloneOf(t)
	got := s.activate(t, e.supervisorTLS, "r-1", generationOf(1), nil, e.agentToken)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var a machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	s.open(t, e.agentTLS)
	s.open(t, e.supervisorTLS)

	replaces := map[string]any{"agentGrn": e.agent.String(), "assigneeGrn": e.supervisor.String(), "epoch": e.epoch}
	stale, next := newStandaloneHost(t), newStandaloneHost(t)
	s.approved(t, stale, replaces)
	s.approved(t, next, replaces)

	r := s.enrolled(t, next, s.spend(t, next.secret, next.enrollment(t)))
	assert.Equal(t, e.agent, r.agent, "a replacement keeps the GRN")
	assert.Equal(t, e.epoch+1, r.epoch)
	assert.NotEqual(t, e.supervisor, r.supervisor)

	lost := s.spend(t, stale.secret, stale.enrollment(t))
	assert.Equal(t, http.StatusConflict, lost.status, "an older approval of the replaced assignment: %s", lost.body)
	replay := s.spend(t, h.secret, h.enrollment(t))
	assert.Equal(t, http.StatusConflict, replay.status, "a superseded result is not disclosed: %s", replay.body)
	assert.NotContains(t, replay.body, "credential")

	// The former lineage's token is refused on the next request.
	allRefused(t, s.protectedCalls(t, e.agentTLS, a.Token), http.StatusUnauthorized, "the former lineage")
	former := s.activate(t, e.supervisorTLS, "r-2", generationOf(2), nil, r.agentToken)
	assert.Equal(t, http.StatusUnauthorized, former.status, "the former supervisor on its open connection: %s", former.body)
	assert.True(t, former.reused)

	s.epoch = r.epoch
	before := s.introspect(t, r.supervisorTLS, e.agentToken, nil)
	assert.Equal(t, http.StatusUnprocessableEntity, before.status,
		"the former supervisor's agent token proves nothing to its successor: %s", before.body)
	now := s.introspect(t, r.supervisorTLS, r.agentToken, nil)
	require.Equal(t, http.StatusOK, now.status, now.body)
	assert.Contains(t, now.body, `"credential":"current"`, "the control: the enrolled one is current")
	got = s.activate(t, r.supervisorTLS, "r-3", generationOf(3), &a.ActivationId, r.agentToken)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var b machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &b))
	s.open(t, r.agentTLS)
	allAdmitted(t, s.protectedCalls(t, r.agentTLS, b.Token), "the replacement's lineage and activation")
}

// TestStandalone_RevocationEndsTheSupervisorAndItsActivation covers an
// administrator revoking a supervisor: its agent's activation ends and
// its pending approvals are withdrawn in the same transaction, on
// connections already open.
func TestStandalone_RevocationEndsTheSupervisorAndItsActivation(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	_, e := s.standaloneOf(t)
	got := s.activate(t, e.supervisorTLS, "v-1", generationOf(1), nil, e.agentToken)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var a machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	pending := newStandaloneHost(t)
	s.approved(t, pending, map[string]any{"agentGrn": e.agent.String(), "assigneeGrn": e.supervisor.String(), "epoch": e.epoch})
	s.open(t, e.agentTLS)
	s.open(t, e.supervisorTLS)

	path := "/orgs/acme/agent-supervisors/" + e.supervisor.Identifier
	fresh := func() *http.Client { return anonymousMachineClient(t) }
	identity := s.send(t, fresh(), http.MethodPost, "/tokens", operator.TokenContractVersion, "Garam-Credential "+e.credential, `{}`)
	assert.Equal(t, http.StatusCreated, identity.status, "the control: a current supervisor exchanges its credential: %s", identity.body)
	status, body := s.browse(t, http.MethodDelete, path, s.member, "")
	assert.Equal(t, http.StatusForbidden, status, "a member revokes nothing: %s", body)
	allAdmitted(t, s.protectedCalls(t, e.agentTLS, a.Token), "the control: still admitted before the revocation")

	status, body = s.browse(t, http.MethodDelete, path, s.owner, "")
	require.Equal(t, http.StatusNoContent, status, body)
	identity = s.send(t, fresh(), http.MethodPost, "/tokens", operator.TokenContractVersion, "Garam-Credential "+e.credential, `{}`)
	assert.Equal(t, http.StatusUnauthorized, identity.status, "a revoked supervisor's credential exchanges nothing: %s", identity.body)
	allRefused(t, s.protectedCalls(t, e.agentTLS, a.Token), http.StatusForbidden, "the revoked supervisor's activation")
	lifecycle := s.activate(t, e.supervisorTLS, "v-2", generationOf(2), &a.ActivationId, e.agentToken)
	assert.Equal(t, http.StatusUnauthorized, lifecycle.status, "a revoked supervisor on its open connection: %s", lifecycle.body)
	withdrawn := s.spend(t, pending.secret, pending.enrollment(t))
	assert.Equal(t, http.StatusUnauthorized, withdrawn.status, "a pending approval cannot undo the revocation: %s", withdrawn.body)
	status, body = s.browse(t, http.MethodDelete, path, s.owner, "")
	assert.Equal(t, http.StatusNotFound, status, body)
}

// spending is spend for a goroutine, which cannot fail the test.
func (s *executionStack) spending(client *http.Client, h standaloneHost, body string) func() (reply, error) {
	return func() (reply, error) {
		return s.exchange(client, http.MethodPost, "/agent-enrollments", agent.CredentialStandaloneContractVersion, "Garam-Bootstrap "+h.secret, body)
	}
}

// browsing is a browser request for a goroutine, as a reply.
func (s *executionStack) browsing(method, path string, cookie *http.Cookie) func() (reply, error) {
	return func() (reply, error) {
		req, err := http.NewRequest(method, s.browser+path, nil)
		if err != nil {
			return reply{}, err
		}
		req.AddCookie(cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return reply{}, err
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		return reply{status: resp.StatusCode, body: string(body)}, err
	}
}

// TestStandalone_AWithdrawalAndASpendingNeverBothTakeEffect queues a
// spending and a withdrawal of one approval behind its row, in both
// orders: whichever takes the row first decides, and the other is
// refused rather than committing from what it read before.
func TestStandalone_AWithdrawalAndASpendingNeverBothTakeEffect(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	anonymous := anonymousMachineClient(t)
	lock := `SELECT 1 FROM agent_enrollments WHERE id = $1 FOR UPDATE`

	h := newStandaloneHost(t)
	e := s.approved(t, h, nil)
	spent, withdrawn := s.queuedBehind(t, lock, e.ID,
		s.spending(anonymous, h, h.enrollment(t)),
		s.browsing(http.MethodDelete, "/orgs/acme/agent-enrollments/"+e.ID, s.owner))
	assert.Equal(t, http.StatusCreated, spent.status, spent.body)
	assert.Equal(t, http.StatusConflict, withdrawn.status, "a spent approval is not withdrawn: %s", withdrawn.body)

	h = newStandaloneHost(t)
	e = s.approved(t, h, nil)
	withdrawn, spent = s.queuedBehind(t, lock, e.ID,
		s.browsing(http.MethodDelete, "/orgs/acme/agent-enrollments/"+e.ID, s.owner),
		s.spending(anonymous, h, h.enrollment(t)))
	assert.Equal(t, http.StatusNoContent, withdrawn.status, withdrawn.body)
	assert.Equal(t, http.StatusUnauthorized, spent.status, "read before the withdrawal, refused after it: %s", spent.body)
	assert.Zero(t, countRows(t, s.pool, `SELECT count(*) FROM agents WHERE grn = $1`, e.AgentGrn))
}

// TestStandalone_ARevocationAndAReplacementLeaveOneOutcome queues a
// revocation and a pending replacement of the same supervisor behind
// the agent's execution row, in both orders: the revocation withdraws
// the replacement, or the replacement ends the registration the
// revocation names. Either way exactly one registration, or none, is
// current.
func TestStandalone_ARevocationAndAReplacementLeaveOneOutcome(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	anonymous := anonymousMachineClient(t)

	for _, revokeFirst := range []bool{true, false} {
		_, e := s.standaloneOf(t)
		next := newStandaloneHost(t)
		s.approved(t, next, map[string]any{"agentGrn": e.agent.String(), "assigneeGrn": e.supervisor.String(), "epoch": e.epoch})
		revoke := s.browsing(http.MethodDelete, "/orgs/acme/agent-supervisors/"+e.supervisor.Identifier, s.owner)
		replace := s.spending(anonymous, next, next.enrollment(t))
		current := `SELECT count(*) FROM agent_supervisors WHERE agent_grn = $1 AND ended_at IS NULL`
		if revokeFirst {
			revoked, replaced := s.queued(t, revoke, replace)
			assert.Equal(t, http.StatusNoContent, revoked.status, revoked.body)
			assert.Equal(t, http.StatusUnauthorized, replaced.status, "the revocation withdrew it: %s", replaced.body)
			assert.Zero(t, countRows(t, s.pool, current, e.agent.String()))
			continue
		}
		replaced, revoked := s.queued(t, replace, revoke)
		assert.Equal(t, http.StatusCreated, replaced.status, replaced.body)
		assert.Equal(t, http.StatusNotFound, revoked.status, "the replacement ended what it names: %s", revoked.body)
		assert.Equal(t, 1, countRows(t, s.pool, current, e.agent.String()))
	}
}

// TestStandalone_ARevocationLeavesNoActivationInEitherOrder queues a
// revocation and a first activation of the same agent behind its
// execution row, each having read its authority before it waits, in
// both orders. Revoked first, the activation is refused under the lock
// rather than installing a token; activated first, the revocation ends
// it. Either way no token admits anything afterwards.
func TestStandalone_ARevocationLeavesNoActivationInEitherOrder(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	current := `SELECT count(*) FROM agent_executions WHERE agent_grn = $1 AND activation_id IS NOT NULL`

	for _, revokeFirst := range []bool{true, false} {
		_, e := s.standaloneOf(t)
		revoke := s.browsing(http.MethodDelete, "/orgs/acme/agent-supervisors/"+e.supervisor.Identifier, s.owner)
		activate := func() (reply, error) {
			return s.exchange(e.supervisorTLS, http.MethodPost, s.agentPath("/activations"), agent.TokenExecutionContractVersion, "",
				activationBodyOf("q-1", e.epoch, generationOf(1), nil, e.agentToken))
		}
		var activated reply
		if revokeFirst {
			revoked, got := s.queued(t, revoke, activate)
			assert.Equal(t, http.StatusNoContent, revoked.status, revoked.body)
			assert.Equal(t, http.StatusForbidden, got.status, "an activation queued behind a revocation: %s", got.body)
			activated = got
		} else {
			got, revoked := s.queued(t, activate, revoke)
			require.Equal(t, http.StatusCreated, got.status, "the control: an activation ahead of the revocation: %s", got.body)
			assert.Equal(t, http.StatusNoContent, revoked.status, revoked.body)
			activated = got
		}
		assert.Zero(t, countRows(t, s.pool, current, e.agent.String()), "no activation survives the revocation")
		if activated.status == http.StatusCreated {
			var a machinegen.Activation
			require.NoError(t, json.Unmarshal([]byte(activated.body), &a))
			s.open(t, e.agentTLS)
			allRefused(t, s.protectedCalls(t, e.agentTLS, a.Token), http.StatusForbidden, "the revoked supervisor's activation")
		}
	}
}

// TestStandalone_IdenticalFirstSpendingsCreateOneAgent queues two
// identical first spendings of one approval behind its row, each having
// found no agent before it waits. One creates the agent and the other
// answers the same agent rather than a supersession. A changed request
// is refused before it waits, and spends nothing, which the approval
// then spent is the control on.
func TestStandalone_IdenticalFirstSpendingsCreateOneAgent(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	anonymous := anonymousMachineClient(t)
	lock := `SELECT 1 FROM agent_enrollments WHERE id = $1 FOR UPDATE`

	h := newStandaloneHost(t)
	e := s.approved(t, h, nil)
	first, second := s.queuedBehind(t, lock, e.ID, s.spending(anonymous, h, h.enrollment(t)), s.spending(anonymous, h, h.enrollment(t)))
	assert.Equal(t, http.StatusCreated, first.status, first.body)
	assert.Equal(t, http.StatusOK, second.status, "an identical spending answers the recorded agent: %s", second.body)
	assert.Equal(t, credentialOf(t, first).agent, credentialOf(t, second).agent)
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agents WHERE grn = $1`, e.AgentGrn))
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agent_supervisors WHERE agent_grn = $1`, e.AgentGrn))

	other := newStandaloneHost(t)
	s.approved(t, other, nil)
	changed := other
	changed.requestID = "another-request"
	refused := s.spend(t, other.secret, changed.enrollment(t))
	assert.Equal(t, http.StatusConflict, refused.status, "a changed request conflicts: %s", refused.body)
	spent := s.spend(t, other.secret, other.enrollment(t))
	assert.Equal(t, http.StatusCreated, spent.status, "the control: the approved request spends it: %s", spent.body)
}
