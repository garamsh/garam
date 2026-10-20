package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// replacementStack is the token stack with a second controller the
// hosted control service's grant covers beside the first, holding a
// token of its own, which is where a replacement moves an agent
// (ADR-0107).
type replacementStack struct {
	*tokenStack
	destination      grn.GRN
	destinationToken string
}

func newReplacementStack(t *testing.T) *replacementStack {
	t.Helper()
	s := &replacementStack{tokenStack: newTokenStack(t)}
	s.destination = registerOperator(t, s.pool, scopedGRN("acme", operator.ServiceType, "destination"))
	s.destinationToken = s.keyedToken(t, s.destination)
	s.delegateTo(t, []grn.GRN{s.controller, s.destination}, "agent:create", "agent:activate", "agent:replace")
	return s
}

// admitted admits a new agent on the first controller under
// agent-enrollment.v1 and answers its GRN and creation reference.
func (s *replacementStack) admitted(t *testing.T, requestID string) (grn.GRN, string) {
	t.Helper()
	ref := s.creationRef(t, s.member, requestID)
	body := bindingBody(requestID, "managed", s.controller, ref, nil)
	got := s.admit(t, s.prepared(t, body), body)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	g, err := grn.Parse(admissionOf(t, got).Grn)
	require.NoError(t, err)
	return g, ref
}

// mintReplace asks, as the owner, for an agent:replace reference
// moving g to destination, minted under requestID.
func (s *replacementStack) mintReplace(t *testing.T, g, destination grn.GRN, requestID string) (int, string) {
	t.Helper()
	raw, err := json.Marshal(map[string]string{
		"audience": s.hosted.String(), "operation": "agent:replace", "target": g.String(),
		"destination": destination.String(), "requestId": requestID, "bodySha256": emptyDigest,
	})
	require.NoError(t, err)
	return call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", s.owner, string(raw))
}

// replaceRef is the owner's agent:replace reference moving g to
// destination.
func (s *replacementStack) replaceRef(t *testing.T, g, destination grn.GRN, requestID string) string {
	t.Helper()
	status, got := s.mintReplace(t, g, destination, requestID)
	require.Equal(t, http.StatusCreated, status, got)
	return authorityOf(t, got).OperationRef
}

// replacement is the agent-enrollment.v1 body moving g from expected at
// epoch to assignee under ref.
func replacement(requestID string, g, expected grn.GRN, epoch int, assignee grn.GRN, ref string) string {
	return bindingBody(requestID, "managed", assignee, ref, map[string]string{
		"agent": g.String(), "expectedAssignee": expected.String(), "expectedEpoch": strconv.Itoa(epoch),
	})
}

// assignmentOf reads where g is assigned now.
func (s *replacementStack) assignmentOf(t *testing.T, g grn.GRN) (string, int) {
	t.Helper()
	var (
		assignee string
		epoch    int
	)
	require.NoError(t, s.pool.QueryRow(context.Background(),
		`SELECT assignee_grn, epoch FROM agent_assignments WHERE agent_grn = $1`, g.String()).Scan(&assignee, &epoch))
	return assignee, epoch
}

// agentTokenAs is the mint of g's token by the controller token
// belongs to: garam's where audience is nil, one shown to audience
// otherwise.
func (s *replacementStack) agentTokenAs(t *testing.T, token string, g grn.GRN, audience *grn.GRN) reply {
	t.Helper()
	body := `{}`
	if audience != nil {
		body = `{"audience":"` + audience.String() + `"}`
	}
	return s.v2(t, http.MethodPost, "/agents/"+g.String()+"/tokens", operator.TokenContractVersion, token, "", body)
}

// activationAt is an execution-fence.v2 activation body at epoch.
func activationAt(requestID, epoch string, g agent.Generation, replaces, ref *string, token string) string {
	raw, _ := json.Marshal(map[string]any{
		"requestId": requestID, "epoch": epoch, "generation": string(g),
		"replacesActivationId": replaces, "operationRef": ref, "token": token,
	})
	return string(raw)
}

const emptyDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// TestAgentReplacement_MovesTheAgentFromTheExpectedEpochAndItStartsOnTheDestination
// replaces an agent that never ran: admission keeps the GRN, moves the
// assignment to the destination at the next epoch in a new lineage
// with nothing active, the destination mints its tokens, and its first
// activation carries the replacement's reference.
func TestAgentReplacement_MovesTheAgentFromTheExpectedEpochAndItStartsOnTheDestination(t *testing.T) {
	s := newReplacementStack(t)
	g, _ := s.admitted(t, "enrol-1")
	opened := s.lineageOf(t, g)
	agents := s.count(t, `SELECT count(*) FROM agents WHERE org_grn = $1`, s.acme.String())
	ref := s.replaceRef(t, g, s.destination, "move-1")
	body := replacement("move-1", g, s.controller, 1, s.destination, ref)

	got := s.prepare(t, body)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var a machinegen.AgentEnrollmentApproval
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	require.NotNil(t, a.Replaces, "the approval names what it replaces")
	assert.Equal(t, machinegen.AgentEnrollmentReplacement{
		Agent: g.String(), ExpectedAssignee: s.controller.String(), ExpectedEpoch: "1",
	}, *a.Replaces)

	got = s.admit(t, a.ApprovalId.String(), body)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	admitted := admissionOf(t, got)
	assert.Equal(t, g.String(), admitted.Grn, "a replacement keeps the GRN")
	assert.Equal(t, s.destination.String(), admitted.Assignee)
	assert.Equal(t, "2", admitted.Epoch, "and raises the epoch")
	assert.Equal(t, machinegen.AgentAdmissionModeFenced, admitted.Mode)
	assert.Equal(t, s.lineageOf(t, g), admitted.Lineage.String(), "the answer names the lineage the execution holds")
	assert.NotEqual(t, opened, admitted.Lineage.String(), "which is a new one")
	assignee, epoch := s.assignmentOf(t, g)
	assert.Equal(t, s.destination.String(), assignee)
	assert.Equal(t, 2, epoch)
	assert.Equal(t, agents, s.count(t, `SELECT count(*) FROM agents WHERE org_grn = $1`, s.acme.String()),
		"no agent was written")

	got = s.agentTokenAs(t, s.controllerToken, g, nil)
	assert.Equal(t, http.StatusForbidden, got.status, "the controller it left mints nothing: %s", got.body)
	got = s.agentTokenAs(t, s.destinationToken, g, &s.hosted)
	require.Equal(t, http.StatusCreated, got.status, "control: the destination mints: %s", got.body)
	var shown machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(got.body), &shown))
	assert.Equal(t, 2, shown.Epoch)

	got = s.v2(t, http.MethodPost, "/agents/"+g.String()+"/activations", agent.TokenExecutionContractVersion,
		s.hostedToken, "", activationAt("first", "2", generation(1), nil, nil, shown.Token))
	assert.Equal(t, http.StatusForbidden, got.status, "a first activation needs its reference: %s", got.body)
	got = s.v2(t, http.MethodPost, "/agents/"+g.String()+"/activations", agent.TokenExecutionContractVersion,
		s.hostedToken, "", activationAt("first", "2", generation(1), nil, &ref, shown.Token))
	assert.Equal(t, http.StatusCreated, got.status, "the replacement's reference starts it: %s", got.body)
}

// TestAgentReplacement_TheEarlierLineagesTokensAreRefusedFromTheNextRequest
// replaces a running agent: its activation ends, the token it claimed
// with and the one forwarded before are refused on the next request,
// and the destination's tokens are current and activate it again.
func TestAgentReplacement_TheEarlierLineagesTokensAreRefusedFromTheNextRequest(t *testing.T) {
	s := newReplacementStack(t)
	g, created := s.admitted(t, "enrol-1")
	own := s.agentToken(t, g, nil)
	shown := s.agentToken(t, g, &s.hosted)
	path := "/agents/" + g.String()
	got := s.v2(t, http.MethodPost, path+"/activations", agent.TokenExecutionContractVersion, s.hostedToken, "",
		tokenActivationBody("first", generation(1), nil, &created, shown.Token))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	running := activationOf(t, got)
	require.Equal(t, http.StatusNoContent, s.claimWith(t, g, own.Token, running.Token),
		"the agent claims before its replacement")

	ref := s.replaceRef(t, g, s.destination, "move-1")
	body := replacement("move-1", g, s.controller, 1, s.destination, ref)
	got = s.admit(t, s.prepared(t, body), body)
	require.Equal(t, http.StatusCreated, got.status, got.body)

	assert.Equal(t, http.StatusUnauthorized, s.claimWith(t, g, own.Token, running.Token),
		"the replaced lineage's token claims nothing on the next request")
	assert.Equal(t, machinegen.ExecutionIntrospectionCredential("fenced"), s.standingV2(t, g, shown.Token).Credential,
		"the token forwarded before the replacement is answered fenced")
	got = s.agentTokenAs(t, s.destinationToken, g, &s.hosted)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var next machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(got.body), &next))
	assert.Equal(t, machinegen.ExecutionIntrospectionCredential("current"), s.standingV2(t, g, next.Token).Credential,
		"control: a token the destination minted in the new lineage is current")

	replaces := running.ActivationId
	got = s.v2(t, http.MethodPost, path+"/activations", agent.TokenExecutionContractVersion, s.hostedToken, "",
		activationAt("second", "2", generation(2), &replaces, nil, shown.Token))
	assert.Equal(t, http.StatusConflict, got.status, "the replaced lineage's token activates nothing: %s", got.body)
	got = s.v2(t, http.MethodPost, path+"/activations", agent.TokenExecutionContractVersion, s.hostedToken, "",
		activationAt("second", "2", generation(2), &replaces, nil, next.Token))
	assert.Equal(t, http.StatusCreated, got.status, "control: the new lineage's token activates: %s", got.body)
}

// TestAgentReplacement_AStaleEpochIsRefused admits two replacements
// prepared at the same assignment: the first moves the agent, and the
// second, whose expected epoch the first raised past, is superseded
// and writes nothing. A binding naming an epoch its reference was not
// minted at conflicts, and a reference minted at an assignment the
// agent has left authorizes nothing.
func TestAgentReplacement_AStaleEpochIsRefused(t *testing.T) {
	s := newReplacementStack(t)
	g, _ := s.admitted(t, "enrol-1")
	firstRef := s.replaceRef(t, g, s.destination, "move-1")
	secondRef := s.replaceRef(t, g, s.destination, "move-2")
	first := replacement("move-1", g, s.controller, 1, s.destination, firstRef)
	second := replacement("move-2", g, s.controller, 1, s.destination, secondRef)
	firstApproval, secondApproval := s.prepared(t, first), s.prepared(t, second)

	got := s.admit(t, firstApproval, first)
	require.Equal(t, http.StatusCreated, got.status, "control: the first replacement moves the agent: %s", got.body)
	lineage := s.lineageOf(t, g)

	got = s.admit(t, secondApproval, second)
	assert.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "superseded", reasonOf(t, got), "the agent left the epoch the second expected")
	assignee, epoch := s.assignmentOf(t, g)
	assert.Equal(t, s.destination.String(), assignee)
	assert.Equal(t, 2, epoch, "the refused replacement raised no epoch")
	assert.Equal(t, lineage, s.lineageOf(t, g), "and moved no lineage")
	assert.Equal(t, 0, s.count(t, `SELECT count(*) FROM agent_enrollment_approvals WHERE admitted_at IS NOT NULL AND operation_ref = $1`, secondRef),
		"and spent no approval")

	back := s.replaceRef(t, g, s.controller, "back-1")
	got = s.prepare(t, replacement("back-1", g, s.destination, 1, s.controller, back))
	assert.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "binding_conflict", reasonOf(t, got), "the reference was minted at epoch 2, not 1")
	backBody := replacement("back-1", g, s.destination, 2, s.controller, back)
	backApproval := s.prepared(t, backBody)

	// Moved by hand to a controller the reference does not name, the
	// agent is at neither assignment the reference holds.
	third := registerOperator(t, s.pool, scopedGRN("acme", operator.ServiceType, "third"))
	_, err := agentpg.New(s.pool).Assign(t.Context(), g, third)
	require.NoError(t, err)
	got = s.admit(t, backApproval, backBody)
	assert.Equal(t, http.StatusForbidden, got.status, "a reference the agent moved away from: %s", got.body)
	assignee, _ = s.assignmentOf(t, g)
	assert.Equal(t, third.String(), assignee, "and nothing moved it back")
}

// TestAgentReplacement_ARetryAnswersTheSameBinding retries both stages,
// the admission from concurrent requests too, and once the agent has
// moved again the retry is superseded rather than answered.
func TestAgentReplacement_ARetryAnswersTheSameBinding(t *testing.T) {
	s := newReplacementStack(t)
	g, _ := s.admitted(t, "enrol-1")
	ref := s.replaceRef(t, g, s.destination, "move-1")
	body := replacement("move-1", g, s.controller, 1, s.destination, ref)
	approval := s.prepared(t, body)

	again := s.prepare(t, body)
	require.Equal(t, http.StatusOK, again.status, again.body)
	var a machinegen.AgentEnrollmentApproval
	require.NoError(t, json.Unmarshal([]byte(again.body), &a))
	assert.Equal(t, approval, a.ApprovalId.String(), "the retry answers the approval already prepared")

	const concurrent = 4
	replies := make([]reply, concurrent)
	var wg sync.WaitGroup
	for i := range replies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			replies[i] = s.admit(t, approval, body)
		}()
	}
	wg.Wait()
	created := 0
	first := admissionOf(t, replies[0])
	for _, got := range replies {
		require.Contains(t, []int{http.StatusCreated, http.StatusOK}, got.status, got.body)
		if got.status == http.StatusCreated {
			created++
		}
		assert.Equal(t, first, admissionOf(t, got), "every answer is the one replacement")
	}
	assert.Equal(t, 1, created, "one request replaced and the rest answered it")
	_, epoch := s.assignmentOf(t, g)
	assert.Equal(t, 2, epoch, "the epoch was raised once")

	got := s.admit(t, approval, body)
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.Equal(t, first, admissionOf(t, got))

	back := s.replaceRef(t, g, s.controller, "back-1")
	backBody := replacement("back-1", g, s.destination, 2, s.controller, back)
	got = s.admit(t, s.prepared(t, backBody), backBody)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	got = s.admit(t, approval, body)
	assert.Equal(t, http.StatusForbidden, got.status,
		"once the agent moved on, the first replacement's reference authorizes nothing: %s", got.body)
}

// TestAgentReplacement_AGrantCoveringOnlyTheSourceIsRefused mints a
// replacement under a grant that names the controller the agent is on
// and not the destination, and under one naming both as the control.
func TestAgentReplacement_AGrantCoveringOnlyTheSourceIsRefused(t *testing.T) {
	s := newReplacementStack(t)
	g, _ := s.admitted(t, "enrol-1")

	s.delegateTo(t, []grn.GRN{s.controller}, "agent:create", "agent:activate", "agent:replace")
	status, got := s.mintReplace(t, g, s.destination, "move-1")
	assert.Equal(t, http.StatusForbidden, status, "a destination the grant does not name: %s", got)

	s.delegateTo(t, []grn.GRN{s.destination}, "agent:create", "agent:activate", "agent:replace")
	status, got = s.mintReplace(t, g, s.destination, "move-1")
	assert.Equal(t, http.StatusForbidden, status, "a source the grant does not name: %s", got)

	s.delegateTo(t, []grn.GRN{s.controller, s.destination}, "agent:create", "agent:activate", "agent:replace")
	ref := s.replaceRef(t, g, s.destination, "move-1")
	body := replacement("move-1", g, s.controller, 1, s.destination, ref)
	approval := s.prepared(t, body)

	s.delegateTo(t, []grn.GRN{s.controller}, "agent:create", "agent:activate", "agent:replace")
	reply := s.admit(t, approval, body)
	assert.Equal(t, http.StatusForbidden, reply.status, "a reference whose grant was narrowed admits nothing: %s", reply.body)
	_, epoch := s.assignmentOf(t, g)
	assert.Equal(t, 1, epoch, "nothing moved")
}

// TestAgentReplacement_TheReplaceActionIsAnOwnersOrAdministratorsAlone
// mints the reference as a member, which no member role grants, beside
// the owner's mint of the same binding.
func TestAgentReplacement_TheReplaceActionIsAnOwnersOrAdministratorsAlone(t *testing.T) {
	s := newReplacementStack(t)
	g, _ := s.admitted(t, "enrol-1")
	raw, err := json.Marshal(map[string]string{
		"audience": s.hosted.String(), "operation": "agent:replace", "target": g.String(),
		"destination": s.destination.String(), "requestId": "move-1", "bodySha256": emptyDigest,
	})
	require.NoError(t, err)
	status, got := call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", s.member, string(raw))
	assert.Equal(t, http.StatusForbidden, status, "a member holds no agent:replace: %s", got)

	status, got = s.mintReplace(t, g, s.destination, "move-1")
	assert.Equal(t, http.StatusCreated, status, "control: the owner mints it: %s", got)

	raw, err = json.Marshal(map[string]string{
		"audience": s.hosted.String(), "operation": "agent:replace", "target": g.String(),
		"requestId": "move-2", "bodySha256": emptyDigest,
	})
	require.NoError(t, err)
	status, got = call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", s.owner, string(raw))
	assert.Equal(t, http.StatusBadRequest, status, "a replacement names its destination: %s", got)
}
