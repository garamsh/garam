package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
)

// The agent-cutover.v1 routes over both real listeners and real
// authority. The Operator's half of the contract is not built
// (garamsh/garam-agent-operator#217), so the hosted control service's
// side of every exchange here is a Go client acting as the Operator:
// it reads the source, imports nothing, and calls the stages in the
// order the contract fixes.

// cutoverRef mints, as cookie's user, an agent:cutover authority for
// the hosted control service over g and answers the status and body.
func (s *executionStack) mintCutover(t *testing.T, cookie *http.Cookie, g grn.GRN, requestID string) (int, string) {
	t.Helper()
	body := `{"audience":"` + s.hosted.String() + `","operation":"agent:cutover","target":"` + g.String() +
		`","requestId":"` + requestID + `","bodySha256":"` + strings.Repeat("ab", 32) + `"}`
	return call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", cookie, body)
}

// cutoverRef is the owner's agent:cutover reference for g.
func (s *executionStack) cutoverRef(t *testing.T, g grn.GRN) string {
	t.Helper()
	status, got := s.mintCutover(t, s.owner, g, "cutover-"+g.Identifier+"-"+s.key())
	require.Equal(t, http.StatusCreated, status, got)
	return authorityOf(t, got).OperationRef
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
	ref := s.cutoverRef(t, g)
	c := s.readCutover(t, g, ref)
	require.NotNil(t, c.SourceDigest)
	got := s.cutoverCall(t, http.MethodPost, g, "/freeze", ref, freezeBody("fixture-"+g.Identifier, *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	got = s.cutoverCall(t, http.MethodPost, g, "/switch", ref, stageBody("fixture-"+g.Identifier))
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
	ref := s.cutoverRef(t, s.agent)

	c := s.readCutover(t, s.agent, ref)
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
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", ref, freeze)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", ref, freeze)
	require.Equal(t, http.StatusOK, got.status, "a freeze whose answer was lost: %s", got.body)

	s.restart(t)
	c = s.readCutover(t, s.agent, ref)
	require.NotNil(t, c.Attempt)
	assert.Equal(t, "import-1", c.Attempt.ImportId)
	assert.Equal(t, machinegen.CutoverAttemptStage("frozen"), c.Attempt.Stage)
	assert.Equal(t, *c.SourceDigest, c.Attempt.FrozenDigest)

	// The legacy route still answers a frozen agent's operator.
	status, body := machineRequest(t, s.controlTLS, http.MethodPost, s.machine+"/agents/"+s.agent.String()+"/certificate")
	assert.Equal(t, http.StatusCreated, status, string(body))

	got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", ref, stageBody("import-1"))
	require.Equal(t, http.StatusOK, got.status, got.body)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", ref, stageBody("import-1"))
	require.Equal(t, http.StatusOK, got.status, "a switch whose answer was lost: %s", got.body)

	status, body = machineRequest(t, s.controlTLS, http.MethodPost, s.machine+"/agents/"+s.agent.String()+"/certificate")
	assert.Equal(t, http.StatusConflict, status, "the operator no longer obtains its certificate: %s", body)

	// Switched and not yet activated, the agent keeps running on what
	// it holds, and no operator polls it.
	assert.Equal(t, collaborated, s.collaborate(t, s.agentTLS, s.agent, ""))

	a := s.activated(t, "first", generation(1), nil, s.leaf)
	assert.Equal(t, s.agent.String(), a.Grn, "the same GRN activates")

	got = s.cutoverCall(t, http.MethodPost, s.agent, "/rollback", ref, stageBody("import-1"))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "reverse_migration_required", *errorOf(t, got).Reason)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", ref, freezeBody("import-2", *c.SourceDigest, c.Epoch))
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
	ref := s.cutoverRef(t, s.agent)
	c := s.readCutover(t, s.agent, ref)

	got := s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", ref,
		freezeBody("import-1", strings.Repeat("00", 32), c.Epoch))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "digest_mismatch", *errorOf(t, got).Reason)
	assert.Equal(t, "aborted", errorOf(t, got).Kind, "the kind stays the errorx kind")

	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", ref, freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", ref, freezeBody("import-2", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "attempt_open", *errorOf(t, got).Reason)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/rollback", ref, stageBody("import-1"))
	require.Equal(t, http.StatusOK, got.status, got.body)
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", ref, stageBody("import-1"))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "attempt_ended", *errorOf(t, got).Reason)

	// A stale epoch is a 409 the contract names no reason for.
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", ref, freezeBody("import-3", *c.SourceDigest, "99"))
	require.Equal(t, http.StatusConflict, got.status, got.body)
	assert.NotContains(t, got.body, `"reason"`, "control: an unlisted refusal carries no reason")
	got = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", ref, stageBody("import-9"))
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
	got = s.cutoverCall(t, http.MethodGet, s.agent, "", ref, "")
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.Contains(t, got.body, `"class":"not-applicable"`)
}

// TestCutoverRoutes_AnAgentFencedFromBirthIsNotEligible: a managed
// agent has no legacy source to move, so its freeze is 422 naming
// why, where the legacy agent beside it freezes.
func TestCutoverRoutes_AnAgentFencedFromBirthIsNotEligible(t *testing.T) {
	s := newManagedStack(t)
	m := s.created(t, "create-1")
	ref := s.cutoverRef(t, m.agent)
	c := s.readCutover(t, m.agent, ref)
	assert.Equal(t, machinegen.CutoverClass("not-applicable"), c.Class)
	require.NotNil(t, c.SourceDigest)
	got := s.cutoverCall(t, http.MethodPost, m.agent, "/freeze", ref, freezeBody("import-1", *c.SourceDigest, c.Epoch))
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

	status, got := s.mintCutover(t, s.member, s.agent, "member-cutover")
	assert.Equal(t, http.StatusForbidden, status, "a member holds no agent:cutover: %s", got)
	ref := s.cutoverRef(t, s.agent)
	c := s.readCutover(t, s.agent, ref)

	answer := s.cutoverCall(t, http.MethodGet, s.agent, "", "", "")
	assert.Equal(t, http.StatusUnauthorized, answer.status, "no reference: %s", answer.body)
	status, got = s.mintHandoff(t, s.owner, strings.Repeat("ab", 32), "recover-not-cutover")
	require.Equal(t, http.StatusCreated, status, got)
	answer = s.cutoverCall(t, http.MethodGet, s.agent, "", authorityOf(t, got).OperationRef, "")
	assert.Equal(t, http.StatusForbidden, answer.status, "a reference minted for another operation: %s", answer.body)
	answer = s.cutoverCall(t, http.MethodGet, s.peer, "", ref, "")
	assert.Equal(t, http.StatusForbidden, answer.status, "a reference minted for another agent: %s", answer.body)

	answer = s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", ref, freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, answer.status, answer.body)

	// The owner is denied agent:cutover after the freeze: the switch,
	// which reads the reference again, is refused, and nothing moved.
	deny := activePolicy(t, policies, s.acme, "deny-cutover",
		statementDoc("deny-cutover", wire.EffectDeny, "agent:cutover", "grn:acme:*"))
	attachPolicy(t, policies, deny, grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada"))
	answer = s.cutoverCall(t, http.MethodPost, s.agent, "/switch", ref, stageBody("import-1"))
	assert.Equal(t, http.StatusForbidden, answer.status, "an explicit Deny wins: %s", answer.body)
	answer = s.cutoverCall(t, http.MethodGet, s.agent, "", ref, "")
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

	ref := s.cutoverRef(t, d.Agent())
	c := s.readCutover(t, d.Agent(), ref)
	require.NotNil(t, c.Source)
	assert.Equal(t, map[string]string{"model": "large"}, c.Source.Values)
	got := s.cutoverCall(t, http.MethodPost, d.Agent(), "/freeze", ref, freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	assert.False(t, polled())
	assert.Equal(t, agent.CutoverFrozen, *stageInConsole())
	status, body = machineRequest(t, s.controlTLS, http.MethodPost, s.machine+"/definitions/"+d.Agent().String()+"/claim")
	assert.Equal(t, http.StatusConflict, status, "the claim stays refused: %s", body)

	got = s.cutoverCall(t, http.MethodPost, d.Agent(), "/rollback", ref, stageBody("import-1"))
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.True(t, polled(), "a rolled-back definition is polled again")
	assert.Nil(t, stageInConsole())
}

// TestCutoverRoutes_AFreezeWhoseAnswerIsLostIsReadBack loses garam
// between a freeze and its answer, as the contract's unknown outcome
// does: the Operator reads the attempt back before retrying.
func TestCutoverRoutes_AFreezeWhoseAnswerIsLostIsReadBack(t *testing.T) {
	s := newLegacyExecutionStack(t)
	ref := s.cutoverRef(t, s.agent)
	c := s.readCutover(t, s.agent, ref)

	require.NoError(t, s.server.Close())
	_, err := s.exchange(s.hostedTLS, http.MethodPost, "/agents/"+s.agent.String()+"/cutover/freeze",
		agent.CutoverContractVersion, "Garam-Operation "+ref, freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Error(t, err, "garam is unavailable")
	s.serve(t)

	read := s.readCutover(t, s.agent, ref)
	assert.Nil(t, read.Attempt, "the freeze never reached garam")
	got := s.cutoverCall(t, http.MethodPost, s.agent, "/freeze", ref, freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	read = s.readCutover(t, s.agent, ref)
	require.NotNil(t, read.Attempt)
	assert.Equal(t, machinegen.CutoverAttemptStage("frozen"), read.Attempt.Stage)
}
