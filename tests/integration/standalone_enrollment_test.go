package integration_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
)

// standaloneHost is what a standalone host prepares before asking for
// an approval (ADR-0082): a bootstrap secret, a request identifier and
// two keys with their requests. None of it but hashes and fingerprints
// reaches the administrator.
type standaloneHost struct {
	secret        string
	requestID     string
	supervisorKey *ecdsa.PrivateKey
	supervisorCSR string
	agentKey      *ecdsa.PrivateKey
	agentCSR      string
}

func newStandaloneHost(t *testing.T) standaloneHost {
	t.Helper()
	raw := make([]byte, 32)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	h := standaloneHost{secret: base64.RawURLEncoding.EncodeToString(raw), requestID: "host-" + hex.EncodeToString(raw[:6])}
	h.supervisorCSR, h.supervisorKey = enrollmentRequest(t)
	h.agentCSR, h.agentKey = enrollmentRequest(t)
	return h
}

func fingerprintOf(t *testing.T, csr string) string {
	t.Helper()
	f, err := agent.RequestedKeyFingerprint([]byte(csr))
	require.NoError(t, err)
	return f.String()
}

// approval is the body an administrator approves h with. replaces is
// nil for a new agent.
func (h standaloneHost) approval(t *testing.T, expiresAt time.Time, replaces map[string]any) string {
	t.Helper()
	secret := sha256.Sum256([]byte(h.secret))
	body := map[string]any{
		"requestId": h.requestID, "hostName": "laptop", "expiresAt": expiresAt.UTC().Format(time.RFC3339Nano),
		"secretSha256": hex.EncodeToString(secret[:]), "supervisorKeySha256": fingerprintOf(t, h.supervisorCSR),
		"agentKeySha256": fingerprintOf(t, h.agentCSR),
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
	raw, err := json.Marshal(map[string]string{
		"requestId": h.requestID, "supervisorCertificateRequestPem": h.supervisorCSR, "agentCertificateRequestPem": h.agentCSR,
	})
	require.NoError(t, err)
	return string(raw)
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

// spend sends one standalone enrollment, whose every answer names the
// standalone contract. A client presenting no certificate is what a
// host holding none yet has.
func (s *executionStack) spend(t *testing.T, secret, body string) reply {
	t.Helper()
	got := s.send(t, anonymousMachineClient(t), http.MethodPost, "/agent-enrollments", agent.StandaloneContractVersion,
		"Garam-Bootstrap "+secret, body)
	assert.Equal(t, agent.StandaloneContractVersion, got.version, "every standalone answer names its contract")
	return got
}

// standaloneAgent is a host after its enrollment: its agent and
// supervisor, and a client presenting each certificate.
type standaloneAgent struct {
	agent          grn.GRN
	supervisor     grn.GRN
	epoch          int
	supervisorTLS  *http.Client
	supervisorLeaf string
	agentTLS       *http.Client
	agentLeaf      string
}

func (s *executionStack) enrolled(t *testing.T, h standaloneHost, got reply) standaloneAgent {
	t.Helper()
	require.Contains(t, []int{http.StatusCreated, http.StatusOK}, got.status, got.body)
	var e machinegen.StandaloneEnrollment
	require.NoError(t, json.Unmarshal([]byte(got.body), &e))
	a, err := grn.Parse(e.Grn)
	require.NoError(t, err)
	sup, err := grn.Parse(e.SupervisorGrn)
	require.NoError(t, err)
	epoch, err := strconv.Atoi(e.Epoch)
	require.NoError(t, err)
	assert.NotContains(t, got.body, "PRIVATE KEY", "an enrollment answers no private key")
	return standaloneAgent{
		agent: a, supervisor: sup, epoch: epoch,
		supervisorTLS: clientOver(t, e.SupervisorCertificatePem, h.supervisorKey), supervisorLeaf: e.SupervisorCertificatePem,
		agentTLS: clientOver(t, e.AgentCertificatePem, h.agentKey), agentLeaf: e.AgentCertificatePem,
	}
}

// clientOver presents certificatePEM over key.
func clientOver(t *testing.T, certificatePEM string, key *ecdsa.PrivateKey) *http.Client {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair([]byte(certificatePEM), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)
	return machineClient(t, pair)
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
// approval and its spending end to end: only an administrator
// approves, only the approved keys spend it, a retry answers what the
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

	// Keys the administrator did not approve spend nothing, so the
	// approved ones still enroll afterwards.
	other := h
	other.agentCSR, other.agentKey = enrollmentRequest(t)
	refused := s.spend(t, h.secret, other.enrollment(t))
	assert.Equal(t, http.StatusForbidden, refused.status, refused.body)

	first := s.spend(t, h.secret, h.enrollment(t))
	require.Equal(t, http.StatusCreated, first.status, first.body)
	e := s.enrolled(t, h, first)
	assert.Equal(t, approval.AgentGrn, e.agent.String(), "the GRN was bound at approval")
	assert.Equal(t, approval.SupervisorGrn, e.supervisor.String())
	assert.Equal(t, 1, e.epoch)

	retry := s.spend(t, h.secret, h.enrollment(t))
	assert.Equal(t, http.StatusOK, retry.status, retry.body)
	assert.JSONEq(t, first.body, retry.body, "a retry answers the recorded result")
	changed := s.spend(t, h.secret, other.enrollment(t))
	assert.Equal(t, http.StatusConflict, changed.status, "a spent approval answers its own request alone: %s", changed.body)

	ctx := context.Background()
	var assignee, mode string
	var epoch int
	require.NoError(t, s.pool.QueryRow(ctx,
		`SELECT a.assignee_grn, a.epoch, x.mode FROM agent_assignments a JOIN agent_executions x USING (agent_grn) WHERE agent_grn = $1`,
		e.agent.String()).Scan(&assignee, &epoch, &mode))
	assert.Equal(t, e.supervisor.String(), assignee)
	assert.Equal(t, 1, epoch)
	assert.Equal(t, "fenced", mode, "a standalone agent is fenced from birth")
	assert.Equal(t, operatorsBefore, countRows(t, s.pool, `SELECT count(*) FROM operators`), "no operator is registered for it")
	assert.Zero(t, countRows(t, s.pool, `SELECT count(*) FROM agent_managed_creations WHERE agent_grn = $1`, e.agent.String()))
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agents WHERE grn = $1`, e.agent.String()),
		"a retry wrote no second identity")
}

// TestStandalone_TheSupervisorActivatesItsOwnAgentAndNothingElse covers
// the supervisor's lifecycle authority on the existing execution
// routes: it activates, inspects and deactivates its agent's
// generation, and nothing else holds that authority for it — not the
// agent's own certificate, not the hosted operator, and not the
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

	byRuntime := s.activate(t, e.agentTLS, "a-runtime", generationOf(1), nil, e.agentLeaf)
	assert.Equal(t, http.StatusForbidden, byRuntime.status, "the execution credential activates nothing: %s", byRuntime.body)
	byHosted := s.activate(t, s.hostedTLS, "a-hosted", generationOf(1), nil, e.agentLeaf)
	assert.Equal(t, http.StatusForbidden, byHosted.status, "no grant covers a supervisor's agent: %s", byHosted.body)

	got := s.activate(t, e.supervisorTLS, "a-1", generationOf(1), nil, e.agentLeaf)
	require.Equal(t, http.StatusCreated, got.status, "the first activation rests on the approval: %s", got.body)
	var a machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	assert.Equal(t, e.agent.String(), a.Grn)
	allAdmitted(t, s.protectedCalls(t, e.agentTLS, a.Token), "the activation's token")

	// The supervisor's certificate reaches no collaboration or operator
	// route; an agent of the organization posting is the control.
	posted := s.send(t, s.peerTLS, http.MethodPost, "/messages", "", "", submissionTo(e.agent, s.key()))
	assert.Equal(t, http.StatusCreated, posted.status, "the control: an ordinary sender posts: %s", posted.body)
	for _, route := range []struct{ method, path, body string }{
		{http.MethodPost, "/messages", submissionTo(e.agent, s.key())},
		{http.MethodGet, "/definitions", ""},
		{http.MethodPost, "/agents", ""},
		{http.MethodPost, s.agentPath("/claims"), ""},
		{http.MethodPost, "/identity/certificate", ""},
	} {
		got := s.send(t, e.supervisorTLS, route.method, route.path, "", "", route.body)
		assert.Equal(t, http.StatusForbidden, got.status, "%s %s: %s", route.method, route.path, got.body)
		assert.Contains(t, got.body, "lifecycle routes alone", "%s %s", route.method, route.path)
	}

	in := s.introspect(t, e.supervisorTLS, e.agentLeaf, &[]agent.Generation{generationOf(1)}[0])
	require.Equal(t, http.StatusOK, in.status, in.body)
	assert.Contains(t, in.body, `"assignee":"`+e.supervisor.String()+`"`)

	s.agent = legacy
	elsewhere := s.activate(t, e.supervisorTLS, "a-other", generationOf(2), nil, s.leaf)
	assert.Equal(t, http.StatusForbidden, elsewhere.status, "a supervisor holds one agent: %s", elsewhere.body)
	s.agent = e.agent
	csr, _ := enrollmentRequest(t)
	raw, err := json.Marshal(map[string]string{"requestId": "x", "epoch": strconv.Itoa(e.epoch), "certificateRequestPem": csr})
	require.NoError(t, err)
	recovery := s.send(t, e.supervisorTLS, http.MethodPost, s.agentPath("/credential-recovery"), agent.ExecutionContractVersion,
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
// every earlier agent certificate stop acting on connections already
// open, an approval of the same assignment made earlier cannot spend
// over the newer one, and the first enrollment no longer replays.
func TestStandalone_ReplacementFencesTheFormerHolderAndLineage(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	h, e := s.standaloneOf(t)
	got := s.activate(t, e.supervisorTLS, "r-1", generationOf(1), nil, e.agentLeaf)
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
	assert.NotContains(t, replay.body, "CERTIFICATE")

	// The certificate's own renewal asks the lineage before the token,
	// so it is refused as fenced where the others are refused as stale.
	for route, r := range s.protectedCalls(t, e.agentTLS, a.Token) {
		assert.Contains(t, []int{http.StatusForbidden, http.StatusConflict}, r.status, "the former lineage: %s: %s", route, r.body)
		assert.True(t, r.reused, "%s reused an open connection", route)
	}
	former := s.activate(t, e.supervisorTLS, "r-2", generationOf(2), nil, r.agentLeaf)
	assert.Equal(t, http.StatusForbidden, former.status, "the former supervisor on its open connection: %s", former.body)
	assert.True(t, former.reused)

	s.epoch = r.epoch
	before := s.introspect(t, r.supervisorTLS, e.agentLeaf, nil)
	require.Equal(t, http.StatusOK, before.status, before.body)
	assert.Contains(t, before.body, `"credential":"fenced"`, "the former agent certificate left the lineage")
	now := s.introspect(t, r.supervisorTLS, r.agentLeaf, nil)
	require.Equal(t, http.StatusOK, now.status, now.body)
	assert.Contains(t, now.body, `"credential":"current"`, "the control: the enrolled one is current")
	got = s.activate(t, r.supervisorTLS, "r-3", generationOf(3), &a.ActivationId, r.agentLeaf)
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
	h, e := s.standaloneOf(t)
	supervisorKey := h.supervisorKey
	got := s.activate(t, e.supervisorTLS, "v-1", generationOf(1), nil, e.agentLeaf)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var a machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	pending := newStandaloneHost(t)
	s.approved(t, pending, map[string]any{"agentGrn": e.agent.String(), "assigneeGrn": e.supervisor.String(), "epoch": e.epoch})
	s.open(t, e.agentTLS)
	s.open(t, e.supervisorTLS)

	path := "/orgs/acme/agent-supervisors/" + e.supervisor.Identifier
	fresh := func() *http.Client { return clientOver(t, e.supervisorLeaf, supervisorKey) }
	identity := s.send(t, fresh(), http.MethodGet, "/identity", "", "", "")
	assert.Equal(t, http.StatusOK, identity.status, "the control: a current supervisor authenticates: %s", identity.body)
	status, body := s.browse(t, http.MethodDelete, path, s.member, "")
	assert.Equal(t, http.StatusForbidden, status, "a member revokes nothing: %s", body)
	allAdmitted(t, s.protectedCalls(t, e.agentTLS, a.Token), "the control: still admitted before the revocation")

	status, body = s.browse(t, http.MethodDelete, path, s.owner, "")
	require.Equal(t, http.StatusNoContent, status, body)
	identity = s.send(t, fresh(), http.MethodGet, "/identity", "", "", "")
	assert.Equal(t, http.StatusUnauthorized, identity.status, "a revoked supervisor's next connection: %s", identity.body)
	allRefused(t, s.protectedCalls(t, e.agentTLS, a.Token), http.StatusForbidden, "the revoked supervisor's activation")
	lifecycle := s.activate(t, e.supervisorTLS, "v-2", generationOf(2), &a.ActivationId, e.agentLeaf)
	assert.Equal(t, http.StatusForbidden, lifecycle.status, "a revoked supervisor on its open connection: %s", lifecycle.body)
	withdrawn := s.spend(t, pending.secret, pending.enrollment(t))
	assert.Equal(t, http.StatusUnauthorized, withdrawn.status, "a pending approval cannot undo the revocation: %s", withdrawn.body)
	status, body = s.browse(t, http.MethodDelete, path, s.owner, "")
	assert.Equal(t, http.StatusNotFound, status, body)
}

// renewal sends one supervisor renewal as client.
func (s *executionStack) renewal(t *testing.T, client *http.Client, requestID, csr string) reply {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"requestId": requestID, "certificateRequestPem": csr})
	require.NoError(t, err)
	got := s.send(t, client, http.MethodPost, "/supervisors/self/certificate", agent.StandaloneContractVersion, "", string(raw))
	assert.Equal(t, agent.StandaloneContractVersion, got.version)
	return got
}

// TestStandalone_ASupervisorRenewalReplaysForItsPredecessor covers the
// supervisor renewing its own certificate: too early is refused, the
// renewal is answered again to the certificate it replaced for the same
// request after a lost answer, two identical renewals racing install
// one successor, and an older certificate or a changed request is
// refused. An activation retry survives the renewal.
func TestStandalone_ASupervisorRenewalReplaysForItsPredecessor(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	_, e := s.standaloneOf(t)
	got := s.activate(t, e.supervisorTLS, "n-1", generationOf(1), nil, e.agentLeaf)
	require.Equal(t, http.StatusCreated, got.status, got.body)

	csr, key := enrollmentRequest(t)
	early := s.renewal(t, e.supervisorTLS, "renew-1", csr)
	assert.Equal(t, http.StatusConflict, early.status, "two thirds of its validity left: %s", early.body)

	s.clock.shift(17 * time.Hour)
	var (
		wg      sync.WaitGroup
		answers [2]reply
	)
	for i := range answers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, _ := json.Marshal(map[string]string{"requestId": "renew-1", "certificateRequestPem": csr})
			answers[i], _ = s.exchange(e.supervisorTLS, http.MethodPost, "/supervisors/self/certificate",
				agent.StandaloneContractVersion, "", string(raw))
		}()
	}
	wg.Wait()
	statuses := []int{answers[0].status, answers[1].status}
	assert.ElementsMatch(t, []int{http.StatusCreated, http.StatusOK}, statuses, "%s / %s", answers[0].body, answers[1].body)
	assert.JSONEq(t, answers[0].body, answers[1].body, "racing identical renewals answer one certificate")
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agent_supervisor_renewals WHERE supervisor_grn = $1`, e.supervisor.String()))

	lost := s.renewal(t, e.supervisorTLS, "renew-1", csr)
	assert.Equal(t, http.StatusOK, lost.status, "a lost answer is answered again: %s", lost.body)
	assert.JSONEq(t, answers[0].body, lost.body)
	otherCSR, _ := enrollmentRequest(t)
	assert.Equal(t, http.StatusConflict, s.renewal(t, e.supervisorTLS, "renew-1", otherCSR).status, "another request")

	var issued machinegen.InitialCertificate
	require.NoError(t, json.Unmarshal([]byte(lost.body), &issued))
	renewed := clientOver(t, issued.CertificatePem, key)
	retry := s.activate(t, renewed, "n-1", generationOf(1), nil, e.agentLeaf)
	assert.Equal(t, http.StatusOK, retry.status, "an activation retry survives the renewal: %s", retry.body)

	csr2, _ := enrollmentRequest(t)
	second := s.renewal(t, renewed, "renew-2", csr2)
	require.Equal(t, http.StatusCreated, second.status, second.body)
	stale := s.renewal(t, e.supervisorTLS, "renew-1", csr)
	assert.Equal(t, http.StatusConflict, stale.status, "two renewals back is not replayed: %s", stale.body)
}

// spending is spend for a goroutine, which cannot fail the test.
func (s *executionStack) spending(client *http.Client, h standaloneHost, body string) func() (reply, error) {
	return func() (reply, error) {
		return s.exchange(client, http.MethodPost, "/agent-enrollments", agent.StandaloneContractVersion, "Garam-Bootstrap "+h.secret, body)
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
			return s.exchange(e.supervisorTLS, http.MethodPost, s.agentPath("/activations"), agent.ExecutionContractVersion, "",
				activationBodyOf("q-1", e.epoch, generationOf(1), nil, e.agentLeaf))
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
			allRefused(t, s.protectedCalls(t, e.agentTLS, a.Token), http.StatusForbidden, "the revoked supervisor's token")
		}
	}
}

// TestStandalone_IdenticalFirstSpendingsCreateOneAgent queues two
// identical first spendings of one approval behind its row, each having
// found no agent before it waits. One creates the agent and the other
// answers the same recorded result rather than a supersession; a
// changed request queued the same way still conflicts.
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
	assert.Equal(t, http.StatusOK, second.status, "an identical spending answers the recorded result: %s", second.body)
	assert.JSONEq(t, first.body, second.body)
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agents WHERE grn = $1`, e.AgentGrn))
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agent_supervisors WHERE agent_grn = $1`, e.AgentGrn))
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(DISTINCT lineage) FROM agent_credentials WHERE agent_grn = $1`, e.AgentGrn))

	other := newStandaloneHost(t)
	o := s.approved(t, other, nil)
	// The same approved key under a request signed again: it passes
	// the fingerprint and waits on the row, then differs from the
	// recorded request.
	changed := other
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, other.agentKey)
	require.NoError(t, err)
	changed.agentCSR = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	first, second = s.queuedBehind(t, lock, o.ID, s.spending(anonymous, other, other.enrollment(t)), s.spending(anonymous, other, changed.enrollment(t)))
	assert.Equal(t, http.StatusCreated, first.status, first.body)
	assert.Equal(t, http.StatusConflict, second.status, "a changed request still conflicts: %s", second.body)
}
