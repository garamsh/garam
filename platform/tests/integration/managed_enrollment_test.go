package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/operator"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
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
	s.delegate(t, "agent:create", "agent:activate", "agent:recover", "agent:configure")
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

// enrollment sends one managed-enrollment.v2 request, whose every
// answer names that version.
func (s *executionStack) enrollment(t *testing.T, client *http.Client, path, body string) reply {
	t.Helper()
	got := s.send(t, client, http.MethodPost, path, agent.TokenEnrollmentContractVersion, "", body)
	assert.Equal(t, agent.TokenEnrollmentContractVersion, got.version, "every enrollment answer names its contract")
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

// tokensOf answers a client presenting m's own token, which the
// controller minted for garam in the lineage m's creation opened, and
// the token it minted to be shown to the hosted control service, which
// that service forwards.
func (s *executionStack) tokensOf(t *testing.T, m managed) (*http.Client, string) {
	t.Helper()
	return tokenPresenting(t, s.agentToken(t, m.agent, nil).Token), s.agentToken(t, m.agent, &s.hosted).Token
}

func managedActivationBody(requestID string, g agent.Generation, replaces, ref *string, forwarded string) string {
	raw, _ := json.Marshal(map[string]any{
		"requestId": requestID, "epoch": "1", "generation": string(g),
		"replacesActivationId": replaces, "operationRef": ref, "token": forwarded,
	})
	return string(raw)
}

func (s *executionStack) activateManaged(t *testing.T, m grn.GRN, requestID string, g agent.Generation, replaces, ref *string, forwarded string) reply {
	t.Helper()
	return s.lifecycle(t, s.hostedTLS, "/agents/"+m.String()+"/activations", "", managedActivationBody(requestID, g, replaces, ref, forwarded))
}

func activationOf(t *testing.T, got reply) machinegen.Activation {
	t.Helper()
	var a machinegen.Activation
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	return a
}

// standingOf answers what introspection says of the forwarded token
// for agent g.
func (s *executionStack) standingOf(t *testing.T, g grn.GRN, forwarded string) machinegen.ExecutionIntrospection {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"token": forwarded})
	require.NoError(t, err)
	got := s.lifecycle(t, s.hostedTLS, "/agents/"+g.String()+"/execution/introspection", "", string(raw))
	require.Equal(t, http.StatusOK, got.status, got.body)
	var in machinegen.ExecutionIntrospection
	require.NoError(t, json.Unmarshal([]byte(got.body), &in))
	return in
}

// collaborate makes protected calls of three domains as g over client
// carrying token: a runtime report and a turn event.
func (s *executionStack) collaborate(t *testing.T, client *http.Client, g grn.GRN, token string) map[string]int {
	t.Helper()
	path := "/agents/" + g.String()
	return map[string]int{
		"runtime-state": s.send(t, client, http.MethodPost, path+"/runtime-state", "", bearer(token), reportBody(1, "connected")).status,
		"turn-event":    s.send(t, client, http.MethodPost, path+"/turn-events", "", bearer(token), `{"kind":"turn.start","body":"b"}`).status,
	}
}

var collaborated = map[string]int{
	"runtime-state": http.StatusNoContent, "turn-event": http.StatusAccepted,
}

var fencedCollaboration = map[string]int{
	"runtime-state": http.StatusConflict, "turn-event": http.StatusConflict,
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
	require.NoError(t, orgpg.New(s.pool).RemoveMember(context.Background(), s.acme, g))
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
	assert.Equal(t, agent.TokenEnrollmentContractVersion, got.version)
	got = s.send(t, s.hostedTLS, http.MethodPost, path, agent.TokenExecutionContractVersion, "", creationBody("create-1", ref))
	assert.Equal(t, http.StatusBadRequest, got.status, "another contract: %s", got.body)

	before := s.agentRows(t)
	globex := scopedGRN("globex", operator.ServiceType, "controller")
	globexTLS := operatorClient(t, s.pool, s.machine, scopedGRN("globex", operator.ServiceType, "hosted"))
	refusals := []struct {
		name   string
		client *http.Client
		path   grn.GRN
		status int
	}{
		{"the controller is not the reference's audience", s.controlTLS, s.controller, http.StatusNotFound},
		{"another organization's operator", globexTLS, s.controller, http.StatusNotFound},
		{"a controller of another organization", s.hostedTLS, globex, http.StatusNotFound},
		{"an agent's own token", s.agentTLS, s.controller, http.StatusForbidden},
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
	assignment, err := agentpg.New(s.pool).GetAssignment(context.Background(), g)
	require.NoError(t, err)
	assert.Equal(t, s.controller, assignment.Assignee, "the assignment is the controller's, not the caller's")

	retried := s.createManaged(t, s.hostedTLS, s.controller, "create-1", ref)
	assert.Equal(t, http.StatusOK, retried.status, retried.body)
	assert.JSONEq(t, created.body, retried.body, "an identical retry answers the same agent")

	other := scopedGRN("acme", operator.ServiceType, "second")
	registerOperator(t, s.pool, other)
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
		return s.exchange(s.hostedTLS, http.MethodPost, path, agent.TokenEnrollmentContractVersion, "", creationBody("create-2", ref))
	})
	statuses := map[int]int{}
	for _, a := range answers {
		statuses[a.status]++
		assert.JSONEq(t, answers[0].body, a.body, "every concurrent answer names one agent")
	}
	assert.Equal(t, map[int]int{http.StatusCreated: 1, http.StatusOK: len(answers) - 1}, statuses)
	assert.Equal(t, 2, countRows(t, s.pool, `SELECT count(*) FROM agent_managed_creations`), "two references, two agents")
}

// TestManagedEnrollment_AManagedAgentIsFencedFromBirth shows a managed
// agent born in the lineage its creation opened and admitted to no
// write before its first activation: its own token reaches nothing
// protected, and another agent's token forwarded for it proves
// nothing. Its own tokens, current in that lineage, activating it and
// then collaborating under the activation are the control.
func TestManagedEnrollment_AManagedAgentIsFencedFromBirth(t *testing.T) {
	ctx := context.Background()
	s := newManagedStack(t)
	m := s.created(t, "birth")
	execution, err := agentpg.New(s.pool).GetExecution(ctx, m.agent)
	require.NoError(t, err)
	assert.NotNil(t, execution.Lineage, "fenced from birth, in the lineage its creation opened")

	ownTLS, forwarded := s.tokensOf(t, m)
	in := s.standingOf(t, m.agent, forwarded)
	assert.Equal(t, machinegen.ExecutionIntrospectionGenerationNone, in.Generation)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredentialCurrent, in.Credential, "the token minted in its lineage")
	assert.Equal(t, map[string]int{
		"runtime-state": http.StatusUnauthorized, "turn-event": http.StatusUnauthorized,
	}, s.collaborate(t, ownTLS, m.agent, ""), "it collaborates before no activation")

	raw, err := json.Marshal(map[string]any{"token": s.forwarded})
	require.NoError(t, err)
	got := s.lifecycle(t, s.hostedTLS, "/agents/"+m.agent.String()+"/execution/introspection", "", string(raw))
	assert.Equal(t, http.StatusUnprocessableEntity, got.status, "another agent's token proves nothing of it: %s", got.body)
	got = s.activateManaged(t, m.agent, "a-other", generationOf(1), nil, &m.ref, s.forwarded)
	assert.Equal(t, http.StatusUnprocessableEntity, got.status, "nor activates it: %s", got.body)

	first := s.activateManaged(t, m.agent, "a-1", generationOf(1), nil, &m.ref, forwarded)
	require.Equal(t, http.StatusCreated, first.status, "control: its own token activates: %s", first.body)
	assert.Equal(t, collaborated, s.collaborate(t, ownTLS, m.agent, activationOf(t, first).Token), "control: and collaborates")
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
}

// TestManagedEnrollment_TheFirstActivationNeedsTheCreationOrAConfigureReference
// starts two managed agents on one controller: the first on its own
// creation reference, the second on an administrator's configure
// reference after its creator lost authority.
func TestManagedEnrollment_TheFirstActivationNeedsTheCreationOrAConfigureReference(t *testing.T) {
	s := newManagedStack(t)
	a, b := s.created(t, "first-a"), s.created(t, "first-b")
	_, aLeaf := s.tokensOf(t, a)
	_, bLeaf := s.tokensOf(t, b)

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
// retries a creation whose answer is stored, once the authority it
// rests on has expired or been revoked and once the agent has moved,
// each beside the same retry answered before anything changed; and
// activates nothing for a caller allowed only to create.
func TestManagedEnrollment_CurrentAuthorityAndPlacementDecideEveryAnswer(t *testing.T) {
	ctx := context.Background()
	s := newManagedStack(t)
	// enrolled creates an agent as cookie's user, answering the request
	// so it can be retried.
	enrolled := func(cookie *http.Cookie, requestID string) (managed, string) {
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
		return managed{agent: g, ref: ref}, creation
	}
	retried := func(creation string) int {
		t.Helper()
		return s.enrollment(t, s.hostedTLS, "/operators/"+s.controller.String()+"/managed-agents", creation).status
	}

	t.Run("an expired grant", func(t *testing.T) {
		_, creation := enrolled(s.member, "expiring")
		assert.Equal(t, http.StatusOK, retried(creation), "control")
		_, err := s.pool.Exec(ctx, `UPDATE operator_delegations SET expires_at = now() - interval '1 second' WHERE id = $1`, *s.grant)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, retried(creation))
	})

	t.Run("a creator who lost agent:create, after the short token expired", func(t *testing.T) {
		s.delegate(t, "agent:create", "agent:activate")
		m, creation := enrolled(s.member, "revoked")
		_, err := s.pool.Exec(ctx, `UPDATE operation_authorities SET token_expires_at = now() - interval '1 second' WHERE ref = $1`, m.ref)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, retried(creation), "control: the reference outlives its token")
		s.removeMember(t, "bob")
		assert.Equal(t, http.StatusForbidden, retried(creation), "no stored answer is disclosed")
	})

	t.Run("a moved assignment", func(t *testing.T) {
		m, creation := enrolled(s.owner, "moved")
		assert.Equal(t, http.StatusOK, retried(creation), "control")
		_, err := agentpg.New(s.pool).Assign(ctx, m.agent, s.controller)
		require.NoError(t, err)
		assert.Equal(t, http.StatusConflict, retried(creation), "the stored answer no longer describes where the agent is")
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
		_, forwarded := s.tokensOf(t, managed{agent: g, ref: ref})
		got = s.activateManaged(t, g, "create-only", generationOf(1), nil, &ref, forwarded)
		assert.Equal(t, http.StatusForbidden, got.status, "activating needs agent:activate: %s", got.body)

		s.delegate(t, "agent:create", "agent:activate")
		ref = s.creationRef(t, s.owner, "create-and-activate")
		got = s.createManaged(t, s.hostedTLS, s.controller, "create-and-activate", ref)
		require.Equal(t, http.StatusCreated, got.status, got.body)
		require.NoError(t, json.Unmarshal([]byte(got.body), &m))
		g, err = grn.Parse(m.Grn)
		require.NoError(t, err)
		_, forwarded = s.tokensOf(t, managed{agent: g, ref: ref})
		got = s.activateManaged(t, g, "create-and-activate", generationOf(1), nil, &ref, forwarded)
		assert.Equal(t, http.StatusCreated, got.status, "control: the same request under a grant covering agent:activate: %s", got.body)
	})
}
