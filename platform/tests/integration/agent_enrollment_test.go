package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// bindingBody is one agent-enrollment.v1 body: requestID, placement,
// the assignee and the member's reference, with replaces where given.
func bindingBody(requestID, placement string, assignee grn.GRN, ref string, replaces map[string]string) string {
	body := map[string]any{"requestId": requestID, "placement": placement, "assignee": assignee.String(), "operationRef": ref}
	if replaces != nil {
		body["replaces"] = replaces
	}
	raw, _ := json.Marshal(body)
	return string(raw)
}

// prepare sends prepareAgentEnrollment as the hosted control service.
func (s *tokenStack) prepare(t *testing.T, body string) reply {
	t.Helper()
	return s.v2(t, http.MethodPost, "/agent-enrollment-approvals", agent.AdmissionContractVersion, s.hostedToken, "", body)
}

// admit sends admitAgentEnrollment for approval as the hosted control
// service.
func (s *tokenStack) admit(t *testing.T, approval, body string) reply {
	t.Helper()
	return s.v2(t, http.MethodPost, "/agent-enrollment-approvals/"+approval+"/admission",
		agent.AdmissionContractVersion, s.hostedToken, "", body)
}

// prepared prepares an approval of body and answers its identifier.
func (s *tokenStack) prepared(t *testing.T, body string) string {
	t.Helper()
	got := s.prepare(t, body)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var a machinegen.AgentEnrollmentApproval
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	return a.ApprovalId.String()
}

func admissionOf(t *testing.T, got reply) machinegen.AgentAdmission {
	t.Helper()
	var a machinegen.AgentAdmission
	require.NoError(t, json.Unmarshal([]byte(got.body), &a))
	return a
}

// reasonOf answers the reason an Error body names, or "" for none.
func reasonOf(t *testing.T, got reply) string {
	t.Helper()
	var e machinegen.Error
	require.NoError(t, json.Unmarshal([]byte(got.body), &e), got.body)
	if e.Reason == nil {
		return ""
	}
	return *e.Reason
}

// count answers how many rows query finds for arg.
func (s *tokenStack) count(t *testing.T, query string, arg any) int {
	t.Helper()
	var n int
	require.NoError(t, s.pool.QueryRow(context.Background(), query, arg).Scan(&n))
	return n
}

// otherBinding is ref's binding of requestID onto another controller
// of the organization. A reference binds one request identifier and
// one assignee, so the assignee is what another binding under that
// request identifier can name.
func (s *tokenStack) otherBinding(requestID, ref string) string {
	return bindingBody(requestID, "managed", scopedGRN("acme", operator.ServiceType, "second"), ref, nil)
}

// TestAgentEnrollment_AdmitsAManagedAgentFencedAndActivatesItThroughTheSharedStages
// walks the four stages ADR-0102 names for managed placement: the
// approval, admission into a fenced execution with no generation, the
// assignee's mint of both tokens, and the first activation on the
// creation reference.
func TestAgentEnrollment_AdmitsAManagedAgentFencedAndActivatesItThroughTheSharedStages(t *testing.T) {
	s := newTokenStack(t)
	ref := s.creationRef(t, s.member, "enrol-1")
	body := bindingBody("enrol-1", "managed", s.controller, ref, nil)

	got := s.admit(t, s.prepared(t, body), body)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	admitted := admissionOf(t, got)
	assert.Equal(t, "1", admitted.Epoch)
	assert.Equal(t, s.controller.String(), admitted.Assignee)
	assert.Equal(t, machinegen.AgentAdmissionModeFenced, admitted.Mode)
	assert.Equal(t, machinegen.MintAgentToken, admitted.CredentialDelivery)
	g, err := grn.Parse(admitted.Grn)
	require.NoError(t, err)

	var (
		lineage   string
		activated bool
		assignee  string
		epoch     int
	)
	require.NoError(t, s.pool.QueryRow(context.Background(),
		`SELECT e.lineage::text, e.activation_id IS NOT NULL, a.assignee_grn, a.epoch
		   FROM agent_executions e JOIN agent_assignments a USING (agent_grn) WHERE agent_grn = $1`,
		g.String()).Scan(&lineage, &activated, &assignee, &epoch))
	assert.False(t, activated, "admission activates no generation")
	assert.Equal(t, admitted.Lineage.String(), lineage, "the answer names the lineage the execution holds")
	assert.Equal(t, s.controller.String(), assignee)
	assert.Equal(t, 1, epoch)

	own := s.agentToken(t, g, nil)
	shown := s.agentToken(t, g, &s.hosted)
	got = s.v2(t, http.MethodPost, "/agents/"+g.String()+"/activations", agent.TokenExecutionContractVersion,
		s.hostedToken, "", tokenActivationBody("first", generation(1), nil, &ref, shown.Token))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	assert.Equal(t, http.StatusNoContent, s.claimWith(t, g, own.Token, activationOf(t, got).Token),
		"the token mintAgentToken delivered acts under the activation")
}

// TestAgentEnrollment_AnIdenticalRetryAnswersTheRecordedBindingAndMintsNoSecondGRN
// retries both stages, the admission from concurrent requests too.
func TestAgentEnrollment_AnIdenticalRetryAnswersTheRecordedBindingAndMintsNoSecondGRN(t *testing.T) {
	s := newTokenStack(t)
	ref := s.creationRef(t, s.member, "enrol-1")
	body := bindingBody("enrol-1", "managed", s.controller, ref, nil)
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
		assert.Equal(t, first, admissionOf(t, got), "every answer is the one admission")
	}
	assert.Equal(t, 1, created, "one request admitted and the rest answered it")

	got := s.admit(t, approval, body)
	require.Equal(t, http.StatusOK, got.status, got.body)
	assert.Equal(t, first, admissionOf(t, got))
	assert.Equal(t, 1, s.count(t, `SELECT count(*) FROM agent_managed_creations WHERE operation_ref = $1`, ref),
		"one GRN for the reference")
}

// TestAgentEnrollment_AnotherBindingUnderTheRequestIDConflicts names
// another assignee under the request identifier an approval holds.
func TestAgentEnrollment_AnotherBindingUnderTheRequestIDConflicts(t *testing.T) {
	s := newTokenStack(t)
	ref := s.creationRef(t, s.member, "enrol-1")
	held := bindingBody("enrol-1", "managed", s.controller, ref, nil)
	approval := s.prepared(t, held)
	other := s.otherBinding("enrol-1", ref)

	got := s.prepare(t, other)
	assert.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "binding_conflict", reasonOf(t, got))

	got = s.admit(t, approval, other)
	assert.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "binding_conflict", reasonOf(t, got), "an unspent approval admits its own binding alone")
	assert.Equal(t, 0, s.count(t, `SELECT count(*) FROM agent_enrollment_approvals WHERE agent_grn IS NOT NULL AND request_id = $1`, "enrol-1"),
		"nothing was admitted")

	again := s.prepare(t, held)
	assert.Equal(t, http.StatusOK, again.status, "control: the held binding is answered: %s", again.body)
}

// TestAgentEnrollment_ASpentApprovalReusedForAnotherIntentIsRefused
// reuses an admitted approval with another binding, and admits a
// reference a managed-enrollment creation already spent.
func TestAgentEnrollment_ASpentApprovalReusedForAnotherIntentIsRefused(t *testing.T) {
	s := newTokenStack(t)
	ref := s.creationRef(t, s.member, "enrol-1")
	body := bindingBody("enrol-1", "managed", s.controller, ref, nil)
	approval := s.prepared(t, body)
	got := s.admit(t, approval, body)
	require.Equal(t, http.StatusCreated, got.status, got.body)

	got = s.admit(t, approval, s.otherBinding("enrol-1", ref))
	assert.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "approval_spent", reasonOf(t, got))
	got = s.admit(t, approval, body)
	assert.Equal(t, http.StatusOK, got.status, "control: the spent approval's own binding is answered: %s", got.body)

	spent := s.createdV2(t, "c-1")
	cross := bindingBody("c-1", "managed", s.controller, spent.ref, nil)
	got = s.admit(t, s.prepared(t, cross), cross)
	assert.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "approval_spent", reasonOf(t, got), "a reference createManagedAgent spent admits nothing here")
	assert.Equal(t, 1, s.count(t, `SELECT count(*) FROM agent_managed_creations WHERE operation_ref = $1`, spent.ref))
}

// TestAgentEnrollment_ModelledPlacementsRefuseByName holds direct
// placement to its reason and a replacement to its own reference kind,
// beside a managed binding of the same reference the route admits.
func TestAgentEnrollment_ModelledPlacementsRefuseByName(t *testing.T) {
	s := newTokenStack(t)
	ref := s.creationRef(t, s.member, "enrol-1")

	got := s.prepare(t, bindingBody("enrol-1", "direct", s.controller, ref, nil))
	assert.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "direct_placement_gated", reasonOf(t, got))

	got = s.prepare(t, bindingBody("enrol-1", "managed", s.controller, ref, map[string]string{
		"agent": s.agent.String(), "expectedAssignee": s.controller.String(), "expectedEpoch": "1",
	}))
	assert.Equal(t, http.StatusForbidden, got.status, "an agent:create reference replaces nothing: %s", got.body)
	assert.Equal(t, 0, s.count(t, `SELECT count(*) FROM agent_enrollment_approvals WHERE operation_ref = $1`, ref),
		"neither refusal wrote an approval")

	body := bindingBody("enrol-1", "managed", s.controller, ref, nil)
	approval := s.prepared(t, body)
	got = s.admit(t, approval, bindingBody("enrol-1", "direct", s.controller, ref, nil))
	assert.Equal(t, http.StatusConflict, got.status, got.body)
	assert.Equal(t, "direct_placement_gated", reasonOf(t, got))
	got = s.admit(t, approval, body)
	assert.Equal(t, http.StatusCreated, got.status, "control: the managed binding admits: %s", got.body)
}

// TestAgentEnrollment_TakesAToken refuses a request presenting no
// token and one under another contract's value, beside the hosted
// control service's token admitted.
func TestAgentEnrollment_TakesAToken(t *testing.T) {
	s := newTokenStack(t)
	ref := s.creationRef(t, s.member, "enrol-1")
	body := bindingBody("enrol-1", "managed", s.controller, ref, nil)

	got := s.send(t, s.client, http.MethodPost, "/agent-enrollment-approvals", agent.AdmissionContractVersion, "", body)
	assert.Equal(t, http.StatusUnauthorized, got.status, got.body)
	assert.Equal(t, agent.AdmissionContractVersion, got.version)

	got = s.tokenSend(t, http.MethodPost, "/agent-enrollment-approvals", map[string]string{
		"Authorization": "Garam-Token " + s.hostedToken, operator.ContractVersionHeader: agent.TokenEnrollmentContractVersion,
	}, body)
	assert.Equal(t, http.StatusBadRequest, got.status, "another contract's value is refused: %s", got.body)

	got = s.prepare(t, body)
	assert.Equal(t, http.StatusCreated, got.status, "control: the token prepares: %s", got.body)
}
