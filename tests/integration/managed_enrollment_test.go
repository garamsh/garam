package integration_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// newManagedStack is the execution stack whose hosted control service
// may also create agents on the controller. The member is the creator:
// every role holds agent:create, and only the owner holds
// agent:configure and agent:recover.
func newManagedStack(t *testing.T) *executionStack {
	t.Helper()
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	s.delegate(t, "agent:create", "agent:activate", "agent:recover", "agent:configure", "agent:cutover")
	return s
}

// mint asks, as cookie's user, for an authority handing the hosted
// control service operation on target.
func (s *executionStack) mint(t *testing.T, cookie *http.Cookie, operation string, target grn.GRN, requestID string) (int, string) {
	t.Helper()
	body := `{"audience":"` + s.hosted.String() + `","operation":"` + operation + `","target":"` + target.String() +
		`","requestId":"` + requestID + `","bodySha256":"` + strings.Repeat("ab", 32) + `"}`
	return call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", cookie, body)
}

// creationRef is cookie's user's agent:create reference for the
// controller, minted under requestID.
func (s *executionStack) creationRef(t *testing.T, cookie *http.Cookie, requestID string) string {
	t.Helper()
	status, got := s.mint(t, cookie, "agent:create", s.controller, requestID)
	require.Equal(t, http.StatusCreated, status, got)
	return authorityOf(t, got).OperationRef
}

// enrollment sends one managed-enrollment.v1 request, whose every
// answer names that version.
func (s *executionStack) enrollment(t *testing.T, client *http.Client, path, body string) reply {
	t.Helper()
	got := s.send(t, client, http.MethodPost, path, agent.EnrollmentContractVersion, "", body)
	assert.Equal(t, agent.EnrollmentContractVersion, got.version, "every enrollment answer names its contract")
	return got
}

func creationBody(requestID, ref string) string {
	raw, _ := json.Marshal(map[string]string{"requestId": requestID, "operationRef": ref})
	return string(raw)
}

func (s *executionStack) createManaged(t *testing.T, client *http.Client, controller grn.GRN, requestID, ref string) reply {
	t.Helper()
	return s.enrollment(t, client, "/operators/"+controller.String()+"/managed-agents", creationBody(requestID, ref))
}

// managed is a created agent and the reference that created it.
type managed struct {
	agent grn.GRN
	ref   string
}

// created creates an agent on the controller under a fresh reference
// and answers it.
func (s *executionStack) created(t *testing.T, requestID string) managed {
	t.Helper()
	ref := s.creationRef(t, s.member, requestID)
	got := s.createManaged(t, s.hostedTLS, s.controller, requestID, ref)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var m machinegen.ManagedAgent
	require.NoError(t, json.Unmarshal([]byte(got.body), &m))
	assert.Equal(t, "1", m.Epoch)
	g, err := grn.Parse(m.Grn)
	require.NoError(t, err)
	return managed{agent: g, ref: ref}
}

func initialBody(requestID, epoch, request, ref string) string {
	raw, _ := json.Marshal(map[string]string{
		"requestId": requestID, "epoch": epoch, "certificateRequestPem": request, "operationRef": ref,
	})
	return string(raw)
}

func (s *executionStack) initialCertificate(t *testing.T, client *http.Client, g grn.GRN, body string) reply {
	t.Helper()
	return s.enrollment(t, client, "/agents/"+g.String()+"/initial-certificate", body)
}

// issuedInitial issues m's first certificate over a key made where
// the controller runs, and answers a client presenting it and its
// leaf.
func (s *executionStack) issuedInitial(t *testing.T, m managed) (*http.Client, string) {
	t.Helper()
	request, key := enrollmentRequest(t)
	got := s.initialCertificate(t, s.hostedTLS, m.agent, initialBody("initial-"+m.agent.Identifier, "1", request, m.ref))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	return initialClient(t, got, key)
}

// initialClient answers a client presenting the certificate an initial
// issuance answered, over the key its request was made with.
func initialClient(t *testing.T, got reply, key *ecdsa.PrivateKey) (*http.Client, string) {
	t.Helper()
	var issued machinegen.InitialCertificate
	require.NoError(t, json.Unmarshal([]byte(got.body), &issued))
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair([]byte(issued.CertificatePem), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)
	return machineClient(t, pair), issued.CertificatePem
}

// uninstalled is a certificate the organization's authority signed
// for g that no initial issuance, renewal or recovery recorded.
func (s *executionStack) uninstalled(t *testing.T, g grn.GRN) (*http.Client, string) {
	t.Helper()
	issued, err := s.issuer.Issue(context.Background(), g)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair(issued.CertificatePEM(), issued.PrivateKeyPEM())
	require.NoError(t, err)
	return machineClient(t, pair), string(issued.CertificatePEM())
}

func managedActivationBody(requestID string, g agent.Generation, replaces, ref *string, leaf string) string {
	raw, _ := json.Marshal(map[string]any{
		"requestId": requestID, "epoch": "1", "generation": string(g),
		"replacesActivationId": replaces, "operationRef": ref, "certificatePem": leaf,
	})
	return string(raw)
}

func (s *executionStack) activateManaged(t *testing.T, m grn.GRN, requestID string, g agent.Generation, replaces, ref *string, leaf string) reply {
	t.Helper()
	return s.lifecycle(t, s.hostedTLS, "/agents/"+m.String()+"/activations", "", managedActivationBody(requestID, g, replaces, ref, leaf))
}

func activationOf(t *testing.T, got reply) machinegen.Activation {
	t.Helper()
	var a machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	return a
}

// standingOf answers what introspection says of leaf for agent g.
func (s *executionStack) standingOf(t *testing.T, g grn.GRN, leaf string) machinegen.ExecutionIntrospection {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"certificatePem": leaf})
	require.NoError(t, err)
	got := s.lifecycle(t, s.hostedTLS, "/agents/"+g.String()+"/execution/introspection", "", string(raw))
	require.Equal(t, http.StatusOK, got.status, got.body)
	var in machinegen.ExecutionIntrospection
	require.NoError(t, json.Unmarshal([]byte(got.body), &in))
	return in
}

// collaborate makes protected calls of three domains as g over client
// carrying token: a runtime report, a turn event and a renewal.
func (s *executionStack) collaborate(t *testing.T, client *http.Client, g grn.GRN, token string) map[string]int {
	t.Helper()
	path := "/agents/" + g.String()
	return map[string]int{
		"runtime-state": s.send(t, client, http.MethodPost, path+"/runtime-state", "", bearer(token), reportBody(1, "connected")).status,
		"turn-event":    s.send(t, client, http.MethodPost, path+"/turn-events", "", bearer(token), `{"kind":"turn.start","body":"b"}`).status,
		"certificate":   s.send(t, client, http.MethodPost, path+"/certificate", "", bearer(token), "").status,
	}
}

var collaborated = map[string]int{
	"runtime-state": http.StatusNoContent, "turn-event": http.StatusAccepted, "certificate": http.StatusCreated,
}

var fencedCollaboration = map[string]int{
	"runtime-state": http.StatusConflict, "turn-event": http.StatusConflict, "certificate": http.StatusConflict,
}

// racing sends three requests at once while a connection of its own
// holds lock, which each of them queues behind, and answers them once
// every one is queued and the lock is released. The pool may hold as
// few as four connections, and each queued request holds one.
func (s *executionStack) racing(t *testing.T, lock string, args []any, send func() (reply, error)) []reply {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, s.pool.Config().ConnString())
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, lock, args...)
	require.NoError(t, err)

	answers := make([]reply, 3)
	var wg sync.WaitGroup
	for i := range answers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := send()
			assert.NoError(t, err)
			answers[i] = r
		}()
	}
	awaitLockWaiters(t, s.pool, len(answers))
	require.NoError(t, tx.Commit(ctx))
	wg.Wait()
	return answers
}

func (s *executionStack) agentRows(t *testing.T) map[string]int {
	t.Helper()
	return map[string]int{
		"agents":      countRows(t, s.pool, `SELECT count(*) FROM agents`),
		"executions":  countRows(t, s.pool, `SELECT count(*) FROM agent_executions`),
		"assignments": countRows(t, s.pool, `SELECT count(*) FROM agent_assignments`),
		"creations":   countRows(t, s.pool, `SELECT count(*) FROM agent_managed_creations`),
	}
}

func (s *executionStack) removeMember(t *testing.T, id string) {
	t.Helper()
	g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, id)
	require.NoError(t, org.NewPostgresStore(s.pool).RemoveMember(context.Background(), s.acme, g))
}

// failing makes every insert into table fail until the returned func
// runs, standing in for a failure between a transaction's writes.
func (s *executionStack) failing(t *testing.T, table string) func() {
	t.Helper()
	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `CREATE OR REPLACE FUNCTION test_fail_insert() RETURNS trigger
		LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected failure' USING ERRCODE = 'XX000'; END $$`)
	require.NoError(t, err)
	_, err = s.pool.Exec(ctx, `CREATE TRIGGER test_fail_insert BEFORE INSERT ON `+table+
		` FOR EACH ROW EXECUTE FUNCTION test_fail_insert()`)
	require.NoError(t, err)
	restore := func() {
		_, err := s.pool.Exec(ctx, `DROP TRIGGER IF EXISTS test_fail_insert ON `+table)
		require.NoError(t, err)
	}
	t.Cleanup(restore)
	return restore
}

// TestManagedEnrollment_AReferenceCreatesOneFencedAgent carries one
// agent:create reference through creation, an identical retry, and
// concurrent identical requests, and refuses every request the
// reference does not bind.
func TestManagedEnrollment_AReferenceCreatesOneFencedAgent(t *testing.T) {
	s := newManagedStack(t)
	ref := s.creationRef(t, s.member, "create-1")
	path := "/operators/" + s.controller.String() + "/managed-agents"

	got := s.send(t, s.hostedTLS, http.MethodPost, path, "", "", creationBody("create-1", ref))
	assert.Equal(t, http.StatusBadRequest, got.status, "no contract version: %s", got.body)
	assert.Equal(t, agent.EnrollmentContractVersion, got.version)
	got = s.send(t, s.hostedTLS, http.MethodPost, path, agent.ExecutionContractVersion, "", creationBody("create-1", ref))
	assert.Equal(t, http.StatusBadRequest, got.status, "another contract: %s", got.body)

	before := s.agentRows(t)
	globex := scopedGRN("globex", operator.ServiceType, "controller")
	globexTLS := machineClient(t, installed(t, s.pool, s.issuer, scopedGRN("globex", operator.ServiceType, "hosted")))
	refusals := []struct {
		name   string
		client *http.Client
		path   grn.GRN
		status int
	}{
		{"the controller is not the reference's audience", s.controlTLS, s.controller, http.StatusNotFound},
		{"another organization's operator", globexTLS, s.controller, http.StatusNotFound},
		{"a controller of another organization", s.hostedTLS, globex, http.StatusNotFound},
		{"an agent's own certificate", s.agentTLS, s.controller, http.StatusForbidden},
	}
	for _, r := range refusals {
		got := s.createManaged(t, r.client, r.path, "create-1", ref)
		assert.Equal(t, r.status, got.status, "%s: %s", r.name, got.body)
	}
	status, configure := s.mint(t, s.owner, "agent:configure", s.agent, "configure-1")
	require.Equal(t, http.StatusCreated, status, configure)
	got = s.createManaged(t, s.hostedTLS, s.controller, "configure-1", authorityOf(t, configure).OperationRef)
	assert.Equal(t, http.StatusForbidden, got.status, "a configure reference creates nothing: %s", got.body)
	assert.Equal(t, before, s.agentRows(t), "no refusal wrote a row")

	created := s.createManaged(t, s.hostedTLS, s.controller, "create-1", ref)
	require.Equal(t, http.StatusCreated, created.status, "control: the audience creates: %s", created.body)
	var first machinegen.ManagedAgent
	require.NoError(t, json.Unmarshal([]byte(created.body), &first))
	g, err := grn.Parse(first.Grn)
	require.NoError(t, err)
	assert.Equal(t, "1", first.Epoch)
	assert.Equal(t, "acme", g.Organization)
	assignment, err := agent.NewPostgresStore(s.pool).GetAssignment(context.Background(), g)
	require.NoError(t, err)
	assert.Equal(t, s.controller, assignment.Assignee(), "the assignment is the controller's, not the caller's")

	retried := s.createManaged(t, s.hostedTLS, s.controller, "create-1", ref)
	assert.Equal(t, http.StatusOK, retried.status, retried.body)
	assert.JSONEq(t, created.body, retried.body, "an identical retry answers the same agent")

	other := scopedGRN("acme", operator.ServiceType, "second")
	_ = installed(t, s.pool, s.issuer, other)
	s.delegateTo(t, []grn.GRN{s.controller, other}, "agent:create", "agent:activate")
	ref = s.creationRef(t, s.member, "create-2")
	got = s.createManaged(t, s.hostedTLS, other, "create-2", ref)
	assert.Equal(t, http.StatusConflict, got.status, "a reference bound to another controller: %s", got.body)
	got = s.createManaged(t, s.hostedTLS, s.controller, "create-3", ref)
	assert.Equal(t, http.StatusConflict, got.status, "a reference bound to another request: %s", got.body)

	// Concurrent identical requests, every one past its read of earlier
	// creations before any commits: one creates, every other answers
	// what it created.
	answers := s.racing(t, `LOCK TABLE agent_managed_creations IN EXCLUSIVE MODE`, nil, func() (reply, error) {
		return s.exchange(s.hostedTLS, http.MethodPost, path, agent.EnrollmentContractVersion, "", creationBody("create-2", ref))
	})
	statuses := map[int]int{}
	for _, a := range answers {
		statuses[a.status]++
		assert.JSONEq(t, answers[0].body, a.body, "every concurrent answer names one agent")
	}
	assert.Equal(t, map[int]int{http.StatusCreated: 1, http.StatusOK: len(answers) - 1}, statuses)
	assert.Equal(t, 2, countRows(t, s.pool, `SELECT count(*) FROM agent_managed_creations`), "two references, two agents")
}

// TestManagedEnrollment_AManagedAgentIsFencedFromBirth shows the
// shared admission predicate refusing every certificate of a managed
// agent that its initial issuance did not record, before that
// issuance and after it, with the recorded leaf as the control; and
// that the legacy certificate route and definition claiming reach it
// at neither point.
func TestManagedEnrollment_AManagedAgentIsFencedFromBirth(t *testing.T) {
	ctx := context.Background()
	s := newManagedStack(t)
	m := s.created(t, "birth")
	strayTLS, stray := s.uninstalled(t, m.agent)
	path := "/agents/" + m.agent.String()

	in := s.standingOf(t, m.agent, stray)
	assert.Equal(t, machinegen.ExecutionIntrospectionModeFenced, in.Mode, "fenced from birth")
	assert.Equal(t, machinegen.ExecutionIntrospectionGenerationNone, in.Generation)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialFenced, in.Credential, "a signed leaf no issuance recorded")
	got := s.activateManaged(t, m.agent, "a-stray", generationOf(1), nil, &m.ref, stray)
	assert.Equal(t, http.StatusConflict, got.status, "it cannot activate: %s", got.body)
	got = s.send(t, s.controlTLS, http.MethodPost, path+"/certificate", "", "", "")
	assert.Equal(t, http.StatusConflict, got.status, "the legacy route issues nothing for it: %s", got.body)
	got = s.send(t, s.controlTLS, http.MethodPost, "/definitions/"+m.agent.String()+"/claim", "", "", "")
	assert.Equal(t, http.StatusNotFound, got.status, "no definition claims it: %s", got.body)
	assert.Equal(t, map[string]int{
		"runtime-state": http.StatusUnauthorized, "turn-event": http.StatusUnauthorized, "certificate": http.StatusConflict,
	}, s.collaborate(t, strayTLS, m.agent, ""), "nor does it collaborate before an activation")
	execution, err := agentpg.New(s.pool).GetExecution(ctx, m.agent)
	require.NoError(t, err)
	assert.Equal(t, agent.ModeFenced, execution.Mode)

	// The legacy cohort's predicate is the same one: a legacy agent's
	// signed leaf is current, which is what a managed agent never
	// inherits.
	_, legacyStray := s.uninstalled(t, s.agent)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, s.standingOf(t, s.agent, legacyStray).Credential)

	initialTLS, initial := s.issuedInitial(t, m)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, s.standingOf(t, m.agent, initial).Credential, "control: the recorded leaf")
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialFenced, s.standingOf(t, m.agent, stray).Credential, "still fenced after issuance")
	got = s.activateManaged(t, m.agent, "a-stray-2", generationOf(1), nil, &m.ref, stray)
	assert.Equal(t, http.StatusConflict, got.status, "it still cannot activate: %s", got.body)
	got = s.send(t, s.controlTLS, http.MethodPost, path+"/certificate", "", "", "")
	assert.Equal(t, http.StatusConflict, got.status, "nor reach the legacy route: %s", got.body)

	first := s.activateManaged(t, m.agent, "a-1", generationOf(1), nil, &m.ref, initial)
	require.Equal(t, http.StatusCreated, first.status, "control: the recorded leaf activates: %s", first.body)
	token := activationOf(t, first).Token
	assert.Equal(t, collaborated, s.collaborate(t, initialTLS, m.agent, token), "control: the recorded leaf collaborates")
	assert.Equal(t, fencedCollaboration, s.collaborate(t, strayTLS, m.agent, token), "the stray leaf does not, with the current token")
}

// TestManagedEnrollment_TheInitialCertificateIsIssuedOnceAndReplayedExactly
// issues a managed agent's first certificate, answers its identical
// retries with the stored public result, and refuses every other
// request for it.
func TestManagedEnrollment_TheInitialCertificateIsIssuedOnceAndReplayedExactly(t *testing.T) {
	s := newManagedStack(t)
	m := s.created(t, "initial")
	other := s.created(t, "other")

	// The controller's request names somebody else; only its key is
	// read.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	elsewhere, err := url.Parse(s.agent.String())
	require.NoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: "someone else"}, URIs: []*url.URL{elsewhere},
	}, key)
	require.NoError(t, err)
	request := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	body := initialBody("i-1", "1", request, m.ref)

	garbage := s.initialCertificate(t, s.hostedTLS, other.agent, initialBody("i-x", "1", "not a request", other.ref))
	assert.Equal(t, http.StatusUnprocessableEntity, garbage.status, "an invalid request: %s", garbage.body)
	oversize := s.initialCertificate(t, s.hostedTLS, m.agent, initialBody("i-x", "1", strings.Repeat("A", agent.MaxRequestPEM+1), m.ref))
	assert.Equal(t, http.StatusBadRequest, oversize.status, oversize.body)
	refusals := []struct {
		name   string
		client *http.Client
		agent  grn.GRN
		body   string
		status int
	}{
		{"another agent's creation reference on the same controller", s.hostedTLS, m.agent, initialBody("i-1", "1", request, other.ref), http.StatusForbidden},
		{"a creation reference for a legacy agent", s.hostedTLS, s.agent, initialBody("i-1", "1", request, m.ref), http.StatusForbidden},
		{"the controller", s.controlTLS, m.agent, body, http.StatusForbidden},
		{"a stale epoch", s.hostedTLS, m.agent, initialBody("i-1", "2", request, m.ref), http.StatusConflict},
	}
	for _, r := range refusals {
		got := s.initialCertificate(t, r.client, r.agent, r.body)
		assert.Equal(t, r.status, got.status, "%s: %s", r.name, got.body)
	}
	assert.Zero(t, countRows(t, s.pool, `SELECT count(*) FROM agent_credentials WHERE agent_grn = $1`, m.agent.String()))

	issued := s.initialCertificate(t, s.hostedTLS, m.agent, body)
	require.Equal(t, http.StatusCreated, issued.status, "control: %s", issued.body)
	var fields map[string]any
	require.NoError(t, json.Unmarshal([]byte(issued.body), &fields))
	assert.ElementsMatch(t, []string{"certificatePem", "issuerPem", "serverRootPem", "notAfter"}, keysOf(fields), "no private key travels")
	var answer machinegen.InitialCertificate
	require.NoError(t, json.Unmarshal([]byte(issued.body), &answer))
	block, _ := pem.Decode([]byte(answer.CertificatePem))
	require.NotNil(t, block)
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	require.Len(t, leaf.URIs, 1)
	assert.Equal(t, m.agent.String(), leaf.URIs[0].String(), "the agent is the one SAN")
	assert.Empty(t, leaf.Subject.CommonName)
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, leaf.ExtKeyUsage)
	assert.True(t, key.PublicKey.Equal(leaf.PublicKey), "signed over the controller's key")
	assert.Equal(t, string(s.config.ServerRoot), answer.ServerRootPem)
	assert.Equal(t, leaf.NotAfter.UTC(), answer.NotAfter.UTC())

	// A lost answer: the same request, before and after the listener
	// restarts, answers exactly what was stored and issues nothing.
	for _, when := range []string{"retry", "after restart"} {
		if when == "after restart" {
			s.restart(t)
		}
		again := s.initialCertificate(t, s.hostedTLS, m.agent, body)
		assert.Equal(t, http.StatusOK, again.status, "%s: %s", when, again.body)
		assert.Equal(t, issued.body, again.body, "%s: the stored public result, byte for byte", when)
	}
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agent_credentials WHERE agent_grn = $1`, m.agent.String()))

	second, _ := enrollmentRequest(t)
	for name, b := range map[string]string{
		"a second request":           initialBody("i-1", "1", second, m.ref),
		"another request identifier": initialBody("i-2", "1", request, m.ref),
	} {
		got := s.initialCertificate(t, s.hostedTLS, m.agent, b)
		assert.Equal(t, http.StatusConflict, got.status, "%s: %s", name, got.body)
	}

	// Concurrent identical first requests, each signed before any
	// records: they issue one effective certificate between them.
	request, _ = enrollmentRequest(t)
	body = initialBody("i-1", "1", request, other.ref)
	answers := s.racing(t, `SELECT 1 FROM agent_executions WHERE agent_grn = $1 FOR UPDATE`, []any{other.agent.String()}, func() (reply, error) {
		return s.exchange(s.hostedTLS, http.MethodPost, "/agents/"+other.agent.String()+"/initial-certificate",
			agent.EnrollmentContractVersion, "", body)
	})
	statuses := map[int]int{}
	for _, a := range answers {
		statuses[a.status]++
		assert.Equal(t, answers[0].body, a.body, "every concurrent answer is one certificate")
	}
	assert.Equal(t, map[int]int{http.StatusCreated: 1, http.StatusOK: len(answers) - 1}, statuses)
	assert.Equal(t, 1, countRows(t, s.pool, `SELECT count(*) FROM agent_credentials WHERE agent_grn = $1`, other.agent.String()))
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestManagedEnrollment_AFailureBetweenWritesLeavesNothingAndARetryRecovers
// fails each enrollment write after the ones before it in its
// transaction, and retries it under the same request.
func TestManagedEnrollment_AFailureBetweenWritesLeavesNothingAndARetryRecovers(t *testing.T) {
	s := newManagedStack(t)
	ref := s.creationRef(t, s.member, "partial")
	before := s.agentRows(t)

	restore := s.failing(t, "agent_managed_creations")
	got := s.createManaged(t, s.hostedTLS, s.controller, "partial", ref)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, got.body)
	assert.Equal(t, before, s.agentRows(t), "no agent, execution or assignment outlives the failure")
	restore()

	created := s.createManaged(t, s.hostedTLS, s.controller, "partial", ref)
	require.Equal(t, http.StatusCreated, created.status, created.body)
	s.restart(t)
	again := s.createManaged(t, s.hostedTLS, s.controller, "partial", ref)
	assert.Equal(t, http.StatusOK, again.status, again.body)
	assert.JSONEq(t, created.body, again.body, "a restart answers the same agent")
	after := s.agentRows(t)
	for table, n := range before {
		assert.Equal(t, n+1, after[table], "one %s row for one creation", table)
	}

	var m machinegen.ManagedAgent
	require.NoError(t, json.Unmarshal([]byte(created.body), &m))
	g, err := grn.Parse(m.Grn)
	require.NoError(t, err)
	request, key := enrollmentRequest(t)
	body := initialBody("partial", "1", request, ref)
	restore = s.failing(t, "agent_initial_certificates")
	got = s.initialCertificate(t, s.hostedTLS, g, body)
	assert.Equal(t, http.StatusServiceUnavailable, got.status, got.body)
	assert.Zero(t, countRows(t, s.pool, `SELECT count(*) FROM agent_credentials WHERE agent_grn = $1`, g.String()),
		"the certificate signed for the failed attempt is in no lineage")
	restore()
	issued := s.initialCertificate(t, s.hostedTLS, g, body)
	require.Equal(t, http.StatusCreated, issued.status, issued.body)
	_, leaf := initialClient(t, issued, key)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, s.standingOf(t, g, leaf).Credential)
}

// TestManagedEnrollment_ARecoveredLineageIsNotRevivedByAnInitialRetry
// recovers a managed agent's credential and retries its initial
// issuance under the identical request.
func TestManagedEnrollment_ARecoveredLineageIsNotRevivedByAnInitialRetry(t *testing.T) {
	s := newManagedStack(t)
	m := s.created(t, "recovered")
	request, key := enrollmentRequest(t)
	body := initialBody("r-initial", "1", request, m.ref)
	issued := s.initialCertificate(t, s.hostedTLS, m.agent, body)
	require.Equal(t, http.StatusCreated, issued.status, issued.body)
	_, initial := initialClient(t, issued, key)

	// The stack's recovery helpers act on s.agent.
	s.agent, s.epoch = m.agent, 1
	r := s.prepareRecovery(t, "r-recover")
	recoveredTLS, leaf := recovered(t, s.recover(t, r, r.body), r.key)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, s.standingOf(t, m.agent, leaf).Credential, "control: the recovered lineage")
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialFenced, s.standingOf(t, m.agent, initial).Credential)

	again := s.initialCertificate(t, s.hostedTLS, m.agent, body)
	assert.Equal(t, http.StatusConflict, again.status, "an identical retry after recovery: %s", again.body)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialFenced, s.standingOf(t, m.agent, initial).Credential, "the initial lineage stays superseded")

	first := s.activateManaged(t, m.agent, "a-1", generationOf(1), nil, &m.ref, leaf)
	require.Equal(t, http.StatusCreated, first.status, first.body)
	assert.Equal(t, collaborated, s.collaborate(t, recoveredTLS, m.agent, activationOf(t, first).Token))
}

// TestManagedEnrollment_TheFirstActivationNeedsTheCreationOrAConfigureReference
// starts two managed agents on one controller: the first on its own
// creation reference, the second on an administrator's configure
// reference after its creator lost authority.
func TestManagedEnrollment_TheFirstActivationNeedsTheCreationOrAConfigureReference(t *testing.T) {
	s := newManagedStack(t)
	a, b := s.created(t, "first-a"), s.created(t, "first-b")
	_, aLeaf := s.issuedInitial(t, a)
	_, bLeaf := s.issuedInitial(t, b)

	got := s.activateManaged(t, a.agent, "a-null", generationOf(1), nil, nil, aLeaf)
	assert.Equal(t, http.StatusForbidden, got.status, "no reference before any activation: %s", got.body)
	got = s.activateManaged(t, a.agent, "a-wrong", generationOf(1), nil, &b.ref, aLeaf)
	assert.Equal(t, http.StatusForbidden, got.status, "another agent's creation on the same controller: %s", got.body)
	got = s.activateManaged(t, a.agent, "a-first", generationOf(1), nil, &a.ref, aLeaf)
	require.Equal(t, http.StatusCreated, got.status, "control: its own creation reference: %s", got.body)
	first := activationOf(t, got)

	got = s.activateManaged(t, a.agent, "a-first", generationOf(1), nil, &a.ref, aLeaf)
	require.Equal(t, http.StatusOK, got.status, "the identical first request, retried: %s", got.body)
	retried := activationOf(t, got)
	assert.Equal(t, first.ActivationId, retried.ActivationId)
	assert.Greater(t, retried.TokenVersion, first.TokenVersion)

	got = s.activateManaged(t, a.agent, "a-restart", generationOf(2), &first.ActivationId, nil, aLeaf)
	require.Equal(t, http.StatusCreated, got.status, "control: a restart needs no reference once one succeeded: %s", got.body)
	second := activationOf(t, got)
	got = s.activateManaged(t, a.agent, "a-first", generationOf(1), nil, &a.ref, aLeaf)
	assert.Equal(t, http.StatusConflict, got.status, "the first request replayed after a later activation: %s", got.body)
	got = s.activateManaged(t, a.agent, "a-reuse", generationOf(3), &second.ActivationId, &a.ref, aLeaf)
	assert.Equal(t, http.StatusForbidden, got.status, "the creation reference launches no later generation: %s", got.body)

	got = s.lifecycle(t, s.hostedTLS, "/agents/"+a.agent.String()+"/activations/"+second.ActivationId+"/deactivation", "", "")
	require.Equal(t, http.StatusOK, got.status, got.body)
	got = s.activateManaged(t, a.agent, "a-after-deactivation", generationOf(3), &second.ActivationId, nil, aLeaf)
	assert.Equal(t, http.StatusCreated, got.status, "deactivation keeps the history a restart rests on: %s", got.body)

	status, minted := s.mint(t, s.member, "agent:configure", b.agent, "b-member-configure")
	assert.Equal(t, http.StatusForbidden, status, "membership grants no configuration authority: %s", minted)
	s.removeMember(t, "bob")
	got = s.activateManaged(t, b.agent, "b-first", generationOf(1), nil, &b.ref, bLeaf)
	assert.Equal(t, http.StatusForbidden, got.status, "the creator no longer holds agent:create: %s", got.body)
	status, minted = s.mint(t, s.owner, "agent:configure", b.agent, "b-configure")
	require.Equal(t, http.StatusCreated, status, minted)
	configure := authorityOf(t, minted).OperationRef
	got = s.activateManaged(t, b.agent, "b-first", generationOf(1), nil, &configure, bLeaf)
	assert.Equal(t, http.StatusCreated, got.status, "control: a fresh configure reference starts it: %s", got.body)
}

// TestManagedEnrollment_CurrentAuthorityAndPlacementDecideEveryAnswer
// retries a creation and an initial issuance whose answers are stored,
// once the authority they rest on has expired or been revoked and once
// the agent has moved, each beside the same retry answered before
// anything changed; and issues nothing for a caller allowed only to
// create.
func TestManagedEnrollment_CurrentAuthorityAndPlacementDecideEveryAnswer(t *testing.T) {
	ctx := context.Background()
	s := newManagedStack(t)
	// enrolled creates an agent as cookie's user and issues its first
	// certificate, answering both requests so they can be retried.
	enrolled := func(cookie *http.Cookie, requestID string) (managed, string, string) {
		t.Helper()
		status, minted := s.mint(t, cookie, "agent:create", s.controller, requestID)
		require.Equal(t, http.StatusCreated, status, minted)
		ref := authorityOf(t, minted).OperationRef
		creation := creationBody(requestID, ref)
		got := s.enrollment(t, s.hostedTLS, "/operators/"+s.controller.String()+"/managed-agents", creation)
		require.Equal(t, http.StatusCreated, got.status, got.body)
		var m machinegen.ManagedAgent
		require.NoError(t, json.Unmarshal([]byte(got.body), &m))
		g, err := grn.Parse(m.Grn)
		require.NoError(t, err)
		request, _ := enrollmentRequest(t)
		initial := initialBody("initial-"+requestID, "1", request, ref)
		got = s.initialCertificate(t, s.hostedTLS, g, initial)
		require.Equal(t, http.StatusCreated, got.status, got.body)
		return managed{agent: g, ref: ref}, creation, initial
	}
	retried := func(m managed, creation, initial string) (int, int) {
		t.Helper()
		created := s.enrollment(t, s.hostedTLS, "/operators/"+s.controller.String()+"/managed-agents", creation)
		issued := s.initialCertificate(t, s.hostedTLS, m.agent, initial)
		return created.status, issued.status
	}

	t.Run("an expired grant", func(t *testing.T) {
		m, creation, initial := enrolled(s.member, "expiring")
		created, issued := retried(m, creation, initial)
		assert.Equal(t, [2]int{http.StatusOK, http.StatusOK}, [2]int{created, issued}, "control")
		_, err := s.pool.Exec(ctx, `UPDATE operator_delegations SET expires_at = now() - interval '1 second' WHERE id = $1`, *s.grant)
		require.NoError(t, err)
		created, issued = retried(m, creation, initial)
		assert.Equal(t, [2]int{http.StatusForbidden, http.StatusForbidden}, [2]int{created, issued})
	})

	t.Run("a creator who lost agent:create, after the short token expired", func(t *testing.T) {
		s.delegate(t, "agent:create", "agent:activate")
		m, creation, initial := enrolled(s.member, "revoked")
		_, err := s.pool.Exec(ctx, `UPDATE operation_authorities SET token_expires_at = now() - interval '1 second' WHERE ref = $1`, m.ref)
		require.NoError(t, err)
		created, issued := retried(m, creation, initial)
		assert.Equal(t, [2]int{http.StatusOK, http.StatusOK}, [2]int{created, issued}, "control: the reference outlives its token")
		s.removeMember(t, "bob")
		created, issued = retried(m, creation, initial)
		assert.Equal(t, [2]int{http.StatusForbidden, http.StatusForbidden}, [2]int{created, issued}, "no stored answer is disclosed")
	})

	t.Run("a moved assignment", func(t *testing.T) {
		m, creation, initial := enrolled(s.owner, "moved")
		created, issued := retried(m, creation, initial)
		assert.Equal(t, [2]int{http.StatusOK, http.StatusOK}, [2]int{created, issued}, "control")
		_, err := agent.NewPostgresStore(s.pool).Assign(ctx, m.agent, s.controller)
		require.NoError(t, err)
		created, issued = retried(m, creation, initial)
		assert.Equal(t, [2]int{http.StatusConflict, http.StatusConflict}, [2]int{created, issued},
			"the stored answers no longer describe where the agent is")
		request, _ := enrollmentRequest(t)
		got := s.initialCertificate(t, s.hostedTLS, m.agent, initialBody("initial-moved", "2", request, m.ref))
		assert.Equal(t, http.StatusConflict, got.status, "at the new epoch: %s", got.body)
	})

	t.Run("a caller allowed only to create", func(t *testing.T) {
		s.delegate(t, "agent:create")
		ref := s.creationRef(t, s.owner, "create-only")
		got := s.createManaged(t, s.hostedTLS, s.controller, "create-only", ref)
		require.Equal(t, http.StatusCreated, got.status, "creation needs no lifecycle authority: %s", got.body)
		var m machinegen.ManagedAgent
		require.NoError(t, json.Unmarshal([]byte(got.body), &m))
		g, err := grn.Parse(m.Grn)
		require.NoError(t, err)
		request, _ := enrollmentRequest(t)
		got = s.initialCertificate(t, s.hostedTLS, g, initialBody("create-only", "1", request, ref))
		assert.Equal(t, http.StatusForbidden, got.status, "issuing needs agent:activate: %s", got.body)

		s.delegate(t, "agent:create", "agent:activate")
		ref = s.creationRef(t, s.owner, "create-and-activate")
		got = s.createManaged(t, s.hostedTLS, s.controller, "create-and-activate", ref)
		require.Equal(t, http.StatusCreated, got.status, got.body)
		require.NoError(t, json.Unmarshal([]byte(got.body), &m))
		g, err = grn.Parse(m.Grn)
		require.NoError(t, err)
		got = s.initialCertificate(t, s.hostedTLS, g, initialBody("create-and-activate", "1", request, ref))
		assert.Equal(t, http.StatusCreated, got.status, "control: the same request under a grant covering agent:activate: %s", got.body)
	})
}
