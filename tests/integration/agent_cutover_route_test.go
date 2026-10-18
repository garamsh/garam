package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
	"github.com/garamsh/garam/internal/wire"
)

// The agent-cutover.v1 routes over both real listeners and real
// authority. The Operator's half of the contract is not built
// (garamsh/garam-agent-operator#217), so the hosted control service's
// side of every exchange here is a Go client acting as the Operator:
// it reads the source, imports nothing, and calls the stages in the
// order the contract fixes.

// cutoverStages are the cutover routes of an agent, by the suffix each
// adds to its read's path.
var cutoverStages = []string{"", "/freeze", "/switch", "/rollback"}

// cutoverTarget is the request target of g's cutover route suffix.
func cutoverTarget(g grn.GRN, suffix string) string {
	return "/agents/" + g.String() + "/cutover" + suffix
}

// mintCutover mints, as cookie's user, an agent:cutover authority for
// the hosted control service over g, bound to target, and answers the
// status and body.
func (s *executionStack) mintCutover(t *testing.T, cookie *http.Cookie, g grn.GRN, requestID, target string) (int, string) {
	t.Helper()
	body := map[string]string{
		"audience": s.hosted.String(), "operation": "agent:cutover", "target": g.String(),
		"requestId": requestID, "bodySha256": strings.Repeat("ab", 32),
	}
	if target != "" {
		body["requestTarget"] = target
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", cookie, string(raw))
}

// cutoverRefs are the owner's agent:cutover references for g, one per
// route, keyed by the route's suffix.
func (s *executionStack) cutoverRefs(t *testing.T, g grn.GRN) map[string]string {
	t.Helper()
	refs := map[string]string{}
	for _, suffix := range cutoverStages {
		status, got := s.mintCutover(t, s.owner, g, "cutover-"+g.Identifier+"-"+s.key(), cutoverTarget(g, suffix))
		require.Equal(t, http.StatusCreated, status, got)
		refs[suffix] = authorityOf(t, got).OperationRef
	}
	return refs
}

// cutoverCall sends one agent-cutover.v1 request carrying ref, whose
// every answer names that version.
func (s *executionStack) cutoverCall(t *testing.T, method string, g grn.GRN, suffix, ref, body string) reply {
	t.Helper()
	authorization := ""
	if ref != "" {
		authorization = "Garam-Operation " + ref
	}
	got := s.send(t, s.hostedTLS, method, "/agents/"+g.String()+"/cutover"+suffix, agent.CutoverContractVersion, authorization, body)
	assert.Equal(t, agent.CutoverContractVersion, got.version, "every cutover answer names its contract")
	return got
}

// readCutover is the Operator's read of g's cutover.
func (s *executionStack) readCutover(t *testing.T, g grn.GRN, ref string) machinegen.Cutover {
	t.Helper()
	got := s.cutoverCall(t, http.MethodGet, g, "", ref, "")
	require.Equal(t, http.StatusOK, got.status, got.body)
	var c machinegen.Cutover
	require.NoError(t, json.Unmarshal([]byte(got.body), &c))
	return c
}

func freezeBody(importID, digest, epoch string) string {
	raw, _ := json.Marshal(map[string]string{"importId": importID, "sourceDigest": digest, "epoch": epoch})
	return string(raw)
}

func stageBody(importID string) string {
	raw, _ := json.Marshal(map[string]string{"importId": importID})
	return string(raw)
}

// cutOver runs g's cutover to its switch as the Operator would: read,
// freeze over what it read, switch.
func (s *executionStack) cutOver(t *testing.T, g grn.GRN) {
	t.Helper()
	refs := s.cutoverRefs(t, g)
	c := s.readCutover(t, g, refs[""])
	require.NotNil(t, c.SourceDigest)
	got := s.cutoverCall(t, http.MethodPost, g, "/freeze", refs["/freeze"], freezeBody("fixture-"+g.Identifier, *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	got = s.cutoverCall(t, http.MethodPost, g, "/switch", refs["/switch"], stageBody("fixture-"+g.Identifier))
	require.Equal(t, http.StatusOK, got.status, got.body)
}

func errorOf(t *testing.T, got reply) machinegen.Error {
	t.Helper()
	var e machinegen.Error
	require.NoError(t, json.Unmarshal([]byte(got.body), &e))
	return e
}

// TestCutoverRoutes_AGoClientActingAsTheOperatorCutsALegacyAgentOver
// walks the contract's order over the wire, against a legacy agent the
// controller created with no definition, so its source is empty. Each
// stage is replayed as a caller that lost the answer replays it, and
// the read survives a restart of the listener.
func TestCutoverRoutes_AGoClientActingAsTheOperatorCutsALegacyAgentOver(t *testing.T) {
	s := newLegacyExecutionStack(t)
	refs := s.cutoverRefs(t, s.agent)

	c := s.readCutover(t, s.agent, refs[""])
	assert.Equal(t, s.agent.String(), c.Agent)
	assert.Equal(t, s.controller.String(), c.Assignee)
	assert.Equal(t, machinegen.CutoverMode("legacy"), c.Mode)
	assert.Equal(t, machinegen.CutoverClass("eligible"), c.Class)
	assert.Nil(t, c.Source)
	assert.Nil(t, c.Attempt)
	require.NotNil(t, c.SourceDigest)
	assert.Equal(t, agent.SourceDigest(s.agent, s.controller, agent.Values{}).String(), *c.SourceDigest)

	// Before the switch the first activation is refused, naming why.
	got := s.activate(t, s.hostedTLS, "first", generation(1), nil, s.leaf)
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "cutover_required", *errorOf(t, got).Reason)

	freeze := freezeBody("import-1", *c.SourceDigest, c.Epoch)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"], freeze)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"], freeze)
	require.Equal(t, http.StatusOK, got.status, "a freeze whose answer was lost: %s", got.body)

	s.restart(t)
	c = s.readCutover(t, s.agent, refs[""])
	require.NotNil(t, c.Attempt)
	assert.Equal(t, "import-1", c.Attempt.ImportId)
	assert.Equal(t, machinegen.CutoverAttemptStage("frozen"), c.Attempt.Stage)
	assert.Equal(t, *c.SourceDigest, c.Attempt.FrozenDigest)

	// The legacy route still answers a frozen agent's operator.
	status, body := machineRequest(t, s.controlTLS, http.MethodPost, s.machine+"/agents/"+s.agent.String()+"/certificate")
	assert.Equal(t, http.StatusCreated, status, string(body))

	got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", refs["/switch"], stageBody("import-1"))
	require.Equal(t, http.StatusOK, got.status, got.body)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", refs["/switch"], stageBody("import-1"))
	require.Equal(t, http.StatusOK, got.status, "a switch whose answer was lost: %s", got.body)

	status, body = machineRequest(t, s.controlTLS, http.MethodPost, s.machine+"/agents/"+s.agent.String()+"/certificate")
	assert.Equal(t, http.StatusConflict, status, "the operator no longer obtains its certificate: %s", body)

	// Switched and not yet activated, the agent keeps running on what
	// it holds, and no operator polls it.
	assert.Equal(t, collaborated, s.collaborate(t, s.agentTLS, s.agent, ""))

	a := s.activated(t, "first", generation(1), nil, s.leaf)
	assert.Equal(t, s.agent.String(), a.Grn, "the same GRN activates")

	got = s.cutoverCall(t, http.MethodPost, s.agent, "/rollback", refs["/rollback"], stageBody("import-1"))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "reverse_migration_required", *errorOf(t, got).Reason)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"], freezeBody("import-2", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "attempt_open", *errorOf(t, got).Reason, "the switched attempt stays the open one")
}

// newLegacyExecutionStack is the execution stack before its agent's
// cutover.
func newLegacyExecutionStack(t *testing.T) *executionStack {
	t.Helper()
	p := pool(t)
	_, engine := policyEngine(t, p)
	return newLegacyStack(t, p, engine)
}

// TestCutoverRoutes_TheReasonIsSetOnlyWhereItsContractNamesIt holds the
// wire's reason to the refusals agent-cutover.v1 lists. Beside each, a
// refusal of the same status the contract does not list carries no
// reason field at all, byte for byte as before.
func TestCutoverRoutes_TheReasonIsSetOnlyWhereItsContractNamesIt(t *testing.T) {
	s := newLegacyExecutionStack(t)
	refs := s.cutoverRefs(t, s.agent)
	c := s.readCutover(t, s.agent, refs[""])

	got := s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"],
		freezeBody("import-1", strings.Repeat("00", 32), c.Epoch))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "digest_mismatch", *errorOf(t, got).Reason)
	assert.Equal(t, "aborted", errorOf(t, got).Kind, "the kind stays the errorx kind")

	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"], freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"], freezeBody("import-2", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "attempt_open", *errorOf(t, got).Reason)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/rollback", refs["/rollback"], stageBody("import-1"))
	require.Equal(t, http.StatusOK, got.status, got.body)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", refs["/switch"], stageBody("import-1"))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "attempt_ended", *errorOf(t, got).Reason)

	// A stale epoch is a 409 the contract names no reason for.
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"], freezeBody("import-3", *c.SourceDigest, "99"))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.NotContains(t, got.body, `"reason"`, "control: an unlisted refusal carries no reason")
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", refs["/switch"], stageBody("import-9"))
	require.Equal(t, http.StatusNotFound, got.status, got.body)
	assert.NotContains(t, got.body, `"reason"`)

	// Activation sets cutover_required and nothing else.
	got = s.activate(t, s.hostedTLS, "first", generation(1), nil, s.leaf)
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "cutover_required", *errorOf(t, got).Reason)
	s.cutOver(t, s.agent)
	s.activated(t, "first", generation(1), nil, s.leaf)
	got = s.activate(t, s.hostedTLS, "first", generation(2), nil, s.leaf)
	require.Equal(t, http.StatusConflict, got.status, "control: another 409 of the same route: %s", got.body)
	assert.NotContains(t, got.body, `"reason"`)

	// An activated agent's class says it is fenced.
	got = s.cutoverCall(t, http.MethodGet, s.agent, "", refs[""], "")
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.Contains(t, got.body, `"class":"not-applicable"`)
}

// TestCutoverRoutes_AnAgentFencedFromBirthIsNotEligible: a managed
// agent has no legacy source to move, so its freeze is 422 naming
// why, where the legacy agent beside it freezes.
func TestCutoverRoutes_AnAgentFencedFromBirthIsNotEligible(t *testing.T) {
	s := newManagedStack(t)
	m := s.created(t, "create-1")
	refs := s.cutoverRefs(t, m.agent)
	c := s.readCutover(t, m.agent, refs[""])
	assert.Equal(t, machinegen.CutoverClass("not-applicable"), c.Class)
	require.NotNil(t, c.SourceDigest)
	got := s.cutoverCall(t, http.MethodPost, m.agent, "/freeze", refs["/freeze"], freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusUnprocessableEntity, got.status, got.body)
	assert.Equal(t, "not_cut_over_eligible", *errorOf(t, got).Reason)

	legacy, _ := provisioned(t, s.machine, s.controlTLS)
	s.cutOver(t, legacy)
}

// TestCutoverRoutes_EachStageRechecksTheReferenceAndTheRoles holds the
// authority a stage rests on: an owner's or administrator's
// agent:cutover reference for this caller and agent, rechecked on every
// stage, where an explicit Deny wins. Each refusal sits beside the
// owner's own reference being accepted.
func TestCutoverRoutes_EachStageRechecksTheReferenceAndTheRoles(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newLegacyStack(t, p, engine)

	status, got := s.mintCutover(t, s.member, s.agent, "member-cutover", cutoverTarget(s.agent, ""))
	assert.Equal(t, http.StatusForbidden, status, "a member holds no agent:cutover: %s", got)
	refs := s.cutoverRefs(t, s.agent)
	c := s.readCutover(t, s.agent, refs[""])

	answer := s.cutoverCall(t, http.MethodGet, s.agent, "", "", "")
	assert.Equal(t, http.StatusUnauthorized, answer.status, "no reference: %s", answer.body)
	status, got = s.mintHandoff(t, s.owner, strings.Repeat("ab", 32), "recover-not-cutover")
	require.Equal(t, http.StatusCreated, status, got)
	answer = s.cutoverCall(t, http.MethodGet, s.agent, "", authorityOf(t, got).OperationRef, "")
	assert.Equal(t, http.StatusForbidden, answer.status, "a reference minted for another operation: %s", answer.body)
	// An operation that binds a request target, minted for this very
	// route, is still another operation.
	s.delegate(t, "agent:activate", "agent:recover", "agent:configure", "agent:cutover", "agent:execution-read")
	raw, err := json.Marshal(map[string]string{
		"audience": s.hosted.String(), "operation": "agent:execution-read", "target": s.agent.String(),
		"requestId": "read-not-cutover", "bodySha256": strings.Repeat("ab", 32),
		"requestTarget": cutoverTarget(s.agent, ""),
	})
	require.NoError(t, err)
	status, got = call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", s.owner, string(raw))
	require.Equal(t, http.StatusCreated, status, got)
	answer = s.cutoverCall(t, http.MethodGet, s.agent, "", authorityOf(t, got).OperationRef, "")
	assert.Equal(t, http.StatusForbidden, answer.status, "another operation bound to this route: %s", answer.body)
	refs = s.cutoverRefs(t, s.agent)
	answer = s.cutoverCall(t, http.MethodGet, s.peer, "", refs[""], "")
	assert.Equal(t, http.StatusForbidden, answer.status, "a reference minted for another agent: %s", answer.body)

	answer = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"], freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, answer.status, answer.body)

	// The owner is denied agent:cutover after the freeze: the switch,
	// which reads the reference again, is refused, and nothing moved.
	deny := activePolicy(t, policies, s.acme, "deny-cutover",
		statementDoc("deny-cutover", wire.EffectDeny, "agent:cutover", "grn:acme:*"))
	attachPolicy(t, policies, deny, grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada"))
	answer = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", refs["/switch"], stageBody("import-1"))
	assert.Equal(t, http.StatusForbidden, answer.status, "an explicit Deny wins: %s", answer.body)
	answer = s.cutoverCall(t, http.MethodGet, s.agent, "", refs[""], "")
	assert.Equal(t, http.StatusForbidden, answer.status, answer.body)
	var frozen string
	require.NoError(t, s.pool.QueryRow(t.Context(), `SELECT stage FROM agent_cutovers WHERE agent_grn = $1`, s.agent.String()).Scan(&frozen))
	assert.Equal(t, "frozen", frozen)
}

// TestCutoverRoutes_ADefinitionLeavesItsOperatorsPollWhileFrozen runs a
// definition's agent through a freeze and a rollback over the wire:
// its operator stops polling it and the console reads it as archival,
// then both return.
func TestCutoverRoutes_ADefinitionLeavesItsOperatorsPollWhileFrozen(t *testing.T) {
	s := newLegacyExecutionStack(t)
	d := defineAgent(t, s.pool, agent.NewPostgresStore(s.pool), s.controller, agent.Values{"model": "large"})
	status, body := machineRequest(t, s.controlTLS, http.MethodPost, s.machine+"/definitions/"+d.Agent().String()+"/claim")
	require.Equal(t, http.StatusCreated, status, string(body))

	polled := func() bool {
		for _, def := range definitionsOf(t, s.controlTLS, s.machine) {
			if def.AgentGrn == d.Agent().String() {
				return true
			}
		}
		return false
	}
	// The browser listener this stack serves carries no agent store,
	// so the console's read is taken from the store it reads; how the
	// stage renders is internal/api's test.
	stageInConsole := func() *agent.CutoverStage {
		defs, err := agent.NewPostgresStore(s.pool).ListDefinitionsByOrg(t.Context(), s.acme)
		require.NoError(t, err)
		for _, def := range defs {
			if def.Agent() == d.Agent() {
				assert.Equal(t, agent.Values{"model": "large"}, def.Values(), "the values stay readable")
				return def.Cutover
			}
		}
		t.Fatalf("the console does not list %s", d.Agent())
		return nil
	}
	require.True(t, polled(), "control: a claimed definition is polled")
	require.Nil(t, stageInConsole())

	refs := s.cutoverRefs(t, d.Agent())
	c := s.readCutover(t, d.Agent(), refs[""])
	require.NotNil(t, c.Source)
	assert.Equal(t, map[string]string{"model": "large"}, c.Source.Values)
	got := s.cutoverCall(t, http.MethodPost, d.Agent(), "/freeze", refs["/freeze"], freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	assert.False(t, polled())
	assert.Equal(t, agent.CutoverFrozen, *stageInConsole())
	status, body = machineRequest(t, s.controlTLS, http.MethodPost, s.machine+"/definitions/"+d.Agent().String()+"/claim")
	assert.Equal(t, http.StatusConflict, status, "the claim stays refused: %s", body)

	got = s.cutoverCall(t, http.MethodPost, d.Agent(), "/rollback", refs["/rollback"], stageBody("import-1"))
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.True(t, polled(), "a rolled-back definition is polled again")
	assert.Nil(t, stageInConsole())
}

// TestCutoverRoutes_AFreezeWhoseAnswerIsLostIsReadBack loses garam
// between a freeze and its answer, as the contract's unknown outcome
// does: the Operator reads the attempt back before retrying.
func TestCutoverRoutes_AFreezeWhoseAnswerIsLostIsReadBack(t *testing.T) {
	s := newLegacyExecutionStack(t)
	refs := s.cutoverRefs(t, s.agent)
	c := s.readCutover(t, s.agent, refs[""])

	require.NoError(t, s.server.Close())
	_, err := s.exchange(s.hostedTLS, http.MethodPost, "/agents/"+s.agent.String()+"/cutover/freeze",
		agent.CutoverContractVersion, "Garam-Operation "+refs["/freeze"], freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Error(t, err, "garam is unavailable")
	s.serve(t)

	read := s.readCutover(t, s.agent, refs[""])
	assert.Nil(t, read.Attempt, "the freeze never reached garam")
	got := s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"], freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	read = s.readCutover(t, s.agent, refs[""])
	require.NotNil(t, read.Attempt)
	assert.Equal(t, machinegen.CutoverAttemptStage("frozen"), read.Attempt.Stage)
}

// TestCutoverRoutes_AReferenceAuthorizesOnlyTheStageItWasMintedFor:
// each reference binds the request target of one cutover route, so an
// owner's reference to roll an attempt back cannot switch it, which no
// rollback undoes. The switch's own reference is the control.
func TestCutoverRoutes_AReferenceAuthorizesOnlyTheStageItWasMintedFor(t *testing.T) {
	s := newLegacyExecutionStack(t)
	refs := s.cutoverRefs(t, s.agent)
	c := s.readCutover(t, s.agent, refs[""])
	got := s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"], freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, got.status, got.body)

	got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", refs["/rollback"], stageBody("import-1"))
	assert.Equal(t, http.StatusForbidden, got.status, "a rollback reference presented to switch: %s", got.body)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/switch"], freezeBody("import-1", *c.SourceDigest, c.Epoch))
	assert.Equal(t, http.StatusForbidden, got.status, "a switch reference presented to freeze: %s", got.body)
	got = s.cutoverCall(t, http.MethodGet, s.agent, "", refs["/freeze"], "")
	assert.Equal(t, http.StatusForbidden, got.status, "a freeze reference presented to the read: %s", got.body)
	read := s.readCutover(t, s.agent, refs[""])
	require.NotNil(t, read.Attempt)
	assert.Equal(t, machinegen.CutoverAttemptStage("frozen"), read.Attempt.Stage, "nothing moved")

	got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", refs["/switch"], stageBody("import-1"))
	assert.Equal(t, http.StatusOK, got.status, "control: the switch's own reference: %s", got.body)

	status, body := s.mintCutover(t, s.owner, s.agent, "no-target", "")
	assert.Equal(t, http.StatusBadRequest, status, "an agent:cutover authority binds a request target: %s", body)
}

// TestCutoverRoutes_AReferenceTheAssignmentOrTheGrantOutlivedIsRefused
// mints the switch's reference, then moves what it was bound to: the
// agent's assignment, or the grant it was minted under, which a
// replacement ends. The old reference is refused on the switch; one
// minted after the move, under the same caller, is the control.
func TestCutoverRoutes_AReferenceTheAssignmentOrTheGrantOutlivedIsRefused(t *testing.T) {
	moves := map[string]func(t *testing.T, s *executionStack){
		"the agent is reassigned": func(t *testing.T, s *executionStack) {
			other := scopedGRN("acme", "operator", "other-controller")
			registerOperator(t, s.pool, other)
			s.delegateTo(t, []grn.GRN{s.controller, other}, "agent:activate", "agent:recover", "agent:configure", "agent:cutover")
			_, err := agent.NewPostgresStore(s.pool).Assign(t.Context(), s.agent, other)
			require.NoError(t, err)
		},
		"the grant is ended": func(t *testing.T, s *executionStack) {
			s.delegate(t, "agent:activate", "agent:recover", "agent:configure", "agent:cutover")
		},
	}
	for name, move := range moves {
		t.Run(name, func(t *testing.T) {
			s := newLegacyExecutionStack(t)
			refs := s.cutoverRefs(t, s.agent)
			c := s.readCutover(t, s.agent, refs[""])
			got := s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", refs["/freeze"], freezeBody("import-1", *c.SourceDigest, c.Epoch))
			require.Equal(t, http.StatusCreated, got.status, got.body)

			move(t, s)
			got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", refs["/switch"], stageBody("import-1"))
			assert.Equal(t, http.StatusForbidden, got.status, "the old reference: %s", got.body)
			var stage string
			require.NoError(t, s.pool.QueryRow(t.Context(), `SELECT stage FROM agent_cutovers WHERE agent_grn = $1`, s.agent.String()).Scan(&stage))
			assert.Equal(t, "frozen", stage, "nothing switched")

			fresh := s.cutoverRefs(t, s.agent)
			got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", fresh["/switch"], stageBody("import-1"))
			assert.Equal(t, http.StatusOK, got.status, "control: a reference minted after the move: %s", got.body)
		})
	}
}

// TestCutoverRoutes_AnotherOrganizationsOperatorReachesNoCutover gives
// globex a hosted control service of its own, delegated by globex's
// owner, holding a valid agent:cutover reference for a globex agent.
// Presented on an acme agent's cutover it is refused; on the globex
// agent it was minted for, it is answered, which is the control.
func TestCutoverRoutes_AnotherOrganizationsOperatorReachesNoCutover(t *testing.T) {
	ctx := t.Context()
	s := newLegacyExecutionStack(t)
	globex := orgRootGRN("globex")
	owner := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "gabe")
	require.NoError(t, userpg.New(s.pool).Create(ctx, user.New(owner, "gabe")))
	require.NoError(t, orgpg.New(s.pool).AddMember(ctx, org.NewMembership(globex, owner, org.RoleOwner)))
	token, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, authn.NewPostgresSessionStore(s.pool).Create(ctx, token, authn.NewSession(owner, time.Hour)))
	cookie := &http.Cookie{Name: "garam_session", Value: token}

	hosted := scopedGRN("globex", "operator", "hosted")
	controller := scopedGRN("globex", "operator", "controller")
	hostedTLS := machineClient(t, installed(t, s.pool, s.issuer, hosted))
	registerOperator(t, s.pool, controller)
	raw, err := json.Marshal(map[string]any{
		"requestId": "globex-grant", "controllers": []string{controller.String()},
		"operations": []string{"agent:activate", "agent:cutover"},
		"expiresAt":  time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339Nano),
	})
	require.NoError(t, err)
	status, got := call(t, http.DefaultClient, http.MethodPut, s.browser+"/orgs/globex/operators/hosted/delegation", cookie, string(raw))
	require.Equal(t, http.StatusCreated, status, got)

	agents := agent.NewPostgresStore(s.pool)
	own := seedAgent(t, agents, globex)
	_, err = agents.Assign(ctx, own, controller)
	require.NoError(t, err)
	mint := func(target string) string {
		raw, err := json.Marshal(map[string]string{
			"audience": hosted.String(), "operation": "agent:cutover", "target": own.String(),
			"requestId": "globex-" + s.key(), "bodySha256": strings.Repeat("ab", 32), "requestTarget": target,
		})
		require.NoError(t, err)
		status, got := call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/globex/operation-authorities", cookie, string(raw))
		require.Equal(t, http.StatusCreated, status, got)
		return authorityOf(t, got).OperationRef
	}
	read := func(g grn.GRN, ref string) reply {
		return s.send(t, hostedTLS, http.MethodGet, cutoverTarget(g, ""), agent.CutoverContractVersion, "Garam-Operation "+ref, "")
	}

	answer := read(s.agent, mint(cutoverTarget(s.agent, "")))
	assert.Equal(t, http.StatusForbidden, answer.status, "globex's operator on acme's agent: %s", answer.body)
	assert.NotContains(t, answer.body, `"source"`)
	answer = read(own, mint(cutoverTarget(own, "")))
	assert.Equal(t, http.StatusOK, answer.status, "control: the agent its reference was minted for: %s", answer.body)
}
