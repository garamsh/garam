package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
	"github.com/garamsh/garam/platform/internal/wire"
)

// enrollmentsListed lists acme's approvals as cookie, at query.
func (s *executionStack) enrollmentsListed(t *testing.T, cookie *http.Cookie, query string) ([]apigen.AgentEnrollmentListing, string) {
	t.Helper()
	status, body := s.browse(t, http.MethodGet, "/orgs/acme/agent-enrollments"+query, cookie, "")
	require.Equal(t, http.StatusOK, status, body)
	var out []apigen.AgentEnrollmentListing
	require.NoError(t, json.Unmarshal([]byte(body), &out))
	return out, body
}

// supervisorsListed lists acme's supervisors as cookie, at query.
func (s *executionStack) supervisorsListed(t *testing.T, cookie *http.Cookie, query string) ([]apigen.AgentSupervisor, string) {
	t.Helper()
	status, body := s.browse(t, http.MethodGet, "/orgs/acme/agent-supervisors"+query, cookie, "")
	require.Equal(t, http.StatusOK, status, body)
	var out []apigen.AgentSupervisor
	require.NoError(t, json.Unmarshal([]byte(body), &out))
	return out, body
}

// TestStandaloneListing_ListsApprovalsInEveryStateWithoutWhatSpendsThem
// approves four hosts — one spent, one withdrawn, one pending and one
// pending replacement of the spent one's agent — and lists them as a
// member. Each is answered in its own state, in approval order, and
// nothing a host spends or proves with appears: no secret hash, no key
// fingerprint, no certificate.
func TestStandaloneListing_ListsApprovalsInEveryStateWithoutWhatSpendsThem(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	spentHost, spent := s.standaloneOf(t)
	withdrawnHost := newStandaloneHost(t)
	withdrawn := s.approved(t, withdrawnHost, nil)
	status, body := s.browse(t, http.MethodDelete, "/orgs/acme/agent-enrollments/"+withdrawn.ID, s.owner, "")
	require.Equal(t, http.StatusNoContent, status, body)
	pendingHost := newStandaloneHost(t)
	pending := s.approved(t, pendingHost, nil)
	replacementHost := newStandaloneHost(t)
	replacement := s.approved(t, replacementHost,
		map[string]any{"agentGrn": spent.agent.String(), "assigneeGrn": spent.supervisor.String(), "epoch": spent.epoch})

	listed, raw := s.enrollmentsListed(t, s.member, "")
	require.Len(t, listed, 4)
	assert.Equal(t, apigen.EnrollmentSpent, listed[0].State)
	assert.Equal(t, spent.agent.String(), listed[0].AgentGrn)
	assert.Equal(t, spent.supervisor.String(), listed[0].SupervisorGrn)
	require.NotNil(t, listed[0].Epoch)
	assert.Equal(t, spent.epoch, *listed[0].Epoch)
	assert.NotNil(t, listed[0].SpentAt)
	assert.Equal(t, withdrawn.ID, listed[1].Id.String())
	assert.Equal(t, apigen.EnrollmentWithdrawn, listed[1].State)
	assert.NotNil(t, listed[1].WithdrawnAt)
	assert.Nil(t, listed[1].Epoch, "a withdrawn approval assigned no epoch")
	assert.Equal(t, pending.ID, listed[2].Id.String())
	assert.Equal(t, apigen.EnrollmentPending, listed[2].State)
	assert.Nil(t, listed[2].Replaces)
	assert.Equal(t, replacement.ID, listed[3].Id.String())
	require.NotNil(t, listed[3].Replaces)
	assert.Equal(t, spent.supervisor.String(), listed[3].Replaces.AssigneeGrn)
	assert.Equal(t, spent.epoch, listed[3].Replaces.Epoch)
	for _, e := range listed {
		assert.Equal(t, apigen.ListingPlacementStandalone, e.Placement)
		assert.Equal(t, "laptop", e.HostName)
	}

	for _, h := range []standaloneHost{spentHost, withdrawnHost, pendingHost, replacementHost} {
		secret := sha256.Sum256([]byte(h.secret))
		assert.NotContains(t, raw, hex.EncodeToString(secret[:]), "a secret's hash")
		assert.NotContains(t, raw, h.secret, "a secret")
	}
	assert.NotContains(t, raw, "KeySha256", "no key fingerprint field")
}

// TestStandaloneListing_ListsSupervisorsActiveAndRevoked enrolls two
// hosts, activates the first's agent and revokes the second. The first
// is listed active with its last activation; the second revoked, with
// when it ended and no activation. Neither carries a certificate.
func TestStandaloneListing_ListsSupervisorsActiveAndRevoked(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	_, active := s.standaloneOf(t)
	got := s.activate(t, active.supervisorTLS, "v-1", generationOf(1), nil, active.agentToken)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	_, revoked := s.standaloneOf(t)
	status, body := s.browse(t, http.MethodDelete, "/orgs/acme/agent-supervisors/"+revoked.supervisor.Identifier, s.owner, "")
	require.Equal(t, http.StatusNoContent, status, body)

	listed, raw := s.supervisorsListed(t, s.member, "")
	require.Len(t, listed, 2)
	assert.Equal(t, active.supervisor.String(), listed[0].Grn)
	assert.Equal(t, active.agent.String(), listed[0].AgentGrn)
	assert.Equal(t, apigen.SupervisorActive, listed[0].State)
	assert.Equal(t, active.epoch, listed[0].Epoch)
	assert.Equal(t, "laptop", listed[0].HostName)
	assert.Nil(t, listed[0].EndedAt)
	assert.NotNil(t, listed[0].LastActivatedAt, "the active supervisor's agent activated")

	assert.Equal(t, revoked.supervisor.String(), listed[1].Grn)
	assert.Equal(t, apigen.SupervisorRevoked, listed[1].State)
	assert.NotNil(t, listed[1].EndedAt)
	assert.Nil(t, listed[1].LastActivatedAt, "the revoked supervisor's agent never activated")
	assert.NotContains(t, raw, "CERTIFICATE")
}

// TestStandaloneListing_IsReadOnAgentRead attaches a Deny of agent:read
// to the member: both lists refuse it with 403. The member before the
// Deny is the control, so what refuses is the Deny on agent:read and
// not a gate that refuses members; the owner after it is the control
// that the Deny reached the member alone.
func TestStandaloneListing_IsReadOnAgentRead(t *testing.T) {
	p := pool(t)
	policies, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	s.standaloneOf(t)
	paths := []string{"/orgs/acme/agent-enrollments", "/orgs/acme/agent-supervisors"}
	for _, path := range paths {
		status, body := s.browse(t, http.MethodGet, path, s.member, "")
		require.Equal(t, http.StatusOK, status, "the control, before the Deny: %s %s", path, body)
	}

	denied := activePolicy(t, policies, s.acme, "no-agent-read",
		statementDoc("no-agent-read", wire.EffectDeny, org.ActionAgentRead, s.acme.String()))
	attachPolicy(t, policies, denied, grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "bob"))

	for _, path := range paths {
		status, body := s.browse(t, http.MethodGet, path, s.member, "")
		assert.Equal(t, http.StatusForbidden, status, "%s %s", path, body)
		status, body = s.browse(t, http.MethodGet, path, s.owner, "")
		assert.Equal(t, http.StatusOK, status, "the owner, whom the Deny does not reach: %s %s", path, body)
	}
}

// TestStandaloneListing_Pages approves three hosts and enrolls two, and
// reads each list a page at a time: the pages together are the whole
// list in order, and a limit the paging columns cannot hold is refused.
func TestStandaloneListing_Pages(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	_, first := s.standaloneOf(t)
	_, second := s.standaloneOf(t)
	third := s.approved(t, newStandaloneHost(t), nil)

	whole, _ := s.enrollmentsListed(t, s.member, "")
	require.Len(t, whole, 3)
	assert.Equal(t, third.ID, whole[2].Id.String())
	page, _ := s.enrollmentsListed(t, s.member, "?limit=2")
	assert.Equal(t, whole[:2], page)
	page, _ = s.enrollmentsListed(t, s.member, "?limit=2&offset=2")
	assert.Equal(t, whole[2:], page)

	supervisors, _ := s.supervisorsListed(t, s.member, "?limit=1")
	require.Len(t, supervisors, 1)
	assert.Equal(t, first.supervisor.String(), supervisors[0].Grn)
	supervisors, _ = s.supervisorsListed(t, s.member, "?limit=1&offset=1")
	require.Len(t, supervisors, 1)
	assert.Equal(t, second.supervisor.String(), supervisors[0].Grn)

	status, body := s.browse(t, http.MethodGet, "/orgs/acme/agent-enrollments?limit=2147483648", s.member, "")
	assert.Equal(t, http.StatusBadRequest, status, body)
}

// TestStandaloneListing_ASupervisorsLastActivationIsAtItsOwnEpoch
// activates a host's agent and replaces the host: the agent is the
// same, at the next epoch, under a new supervisor that has not
// activated it. The replaced supervisor is listed revoked with the
// activation its epoch saw; its successor, active, with none — the
// agent's activation belongs to the epoch it was made at.
func TestStandaloneListing_ASupervisorsLastActivationIsAtItsOwnEpoch(t *testing.T) {
	p := pool(t)
	_, engine := policyEngine(t, p)
	s := newExecutionStack(t, p, engine)
	_, former := s.standaloneOf(t)
	got := s.activate(t, former.supervisorTLS, "r-1", generationOf(1), nil, former.agentToken)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	next := newStandaloneHost(t)
	s.approved(t, next, map[string]any{"agentGrn": former.agent.String(), "assigneeGrn": former.supervisor.String(), "epoch": former.epoch})
	successor := s.enrolled(t, next, s.spend(t, next.secret, next.enrollment(t)))
	require.Equal(t, former.agent, successor.agent)

	listed, _ := s.supervisorsListed(t, s.member, "")
	require.Len(t, listed, 2)
	assert.Equal(t, former.supervisor.String(), listed[0].Grn)
	assert.Equal(t, apigen.SupervisorRevoked, listed[0].State, "a replacement ends the registration it replaced")
	assert.NotNil(t, listed[0].LastActivatedAt)
	assert.Equal(t, successor.supervisor.String(), listed[1].Grn)
	assert.Equal(t, apigen.SupervisorActive, listed[1].State)
	assert.Equal(t, former.epoch+1, listed[1].Epoch)
	assert.Nil(t, listed[1].LastActivatedAt, "the agent's activation was at the former epoch")
}
