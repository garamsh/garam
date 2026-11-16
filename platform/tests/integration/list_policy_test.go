//go:build policymatrix

package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policydecision"
	"github.com/garamsh/garam/platform/internal/wire"
)

// The cases here hold the lists and the search the browser listener
// filters against the stack's real policy service (ADR-0172): one
// Evaluations call per page, each question a probe, a refused object
// hidden beside an allowed one, and a failing service a 503.

// listedKind is one kind a list and the search filter by the object's
// own read.
type listedKind struct {
	name, searchKind, path, entity, action string
	seed                                   func(t *testing.T, s *enforcementStack, id string) grn.GRN
}

func listedKinds() []listedKind {
	return []listedKind{
		{"groups", "team", "/orgs/acme/groups", "Iam::Team", org.ActionGroupRead,
			func(t *testing.T, s *enforcementStack, id string) grn.GRN {
				g := grn.New("acme", org.ServiceTypeGroup, id)
				require.NoError(t, s.orgs.CreateGroup(context.Background(), org.NewGroup(g, id, s.acme)))
				return g
			}},
		{"projects", "project", "/orgs/acme/projects", "Iam::Project", org.ActionProjectRead,
			func(t *testing.T, s *enforcementStack, id string) grn.GRN {
				g := grn.New("acme", org.ServiceTypeProject, id)
				require.NoError(t, s.orgs.CreateProject(context.Background(), org.NewProject(g, id, s.acme, string(org.RoleAdmin))))
				return g
			}},
		{"agents", "agent", "/orgs/acme/agents", "Iam::Agent", org.ActionAgentRead,
			func(t *testing.T, s *enforcementStack, id string) grn.GRN {
				g := grn.New("acme", agent.ServiceType, id)
				a, err := agent.New(g, s.acme)
				require.NoError(t, err)
				require.NoError(t, s.agents.Create(context.Background(), a))
				return g
			}},
	}
}

// forbid writes and activates a guardrail through the relay.
func (s *enforcementStack) forbid(t *testing.T, text string) {
	t.Helper()
	status, got := browse(t, http.MethodPost, s.url+"/orgs/acme/iam-policies", s.owner,
		`{"name":"`+guardrailName+`","body":`+textBody("guardrail", text)+`}`)
	require.Equal(t, http.StatusCreated, status, got)
	status, got = browse(t, http.MethodPost, s.url+"/orgs/acme/iam-policies/"+policyID(t, got)+"/activate", s.owner, "")
	require.Equal(t, http.StatusNoContent, status, got)
}

// grns lists path as cookie and answers the GRNs of the objects of a
// list, or of one kind of a search, in order.
func (s *enforcementStack) grns(t *testing.T, path, searchKind string, cookie *http.Cookie) []string {
	t.Helper()
	status, body := s.get(t, path, cookie)
	require.Equal(t, http.StatusOK, status, body)
	out := []string{}
	if searchKind == "" {
		var objects []struct {
			Grn string `json:"grn"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &objects))
		for _, o := range objects {
			out = append(out, o.Grn)
		}
		return out
	}
	var answer struct {
		Found []struct {
			Kind    string `json:"kind"`
			Objects []struct {
				Grn string `json:"grn"`
			} `json:"objects"`
		} `json:"found"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &answer))
	for _, k := range answer.Found {
		if k.Kind != searchKind {
			continue
		}
		for _, o := range k.Objects {
			out = append(out, o.Grn)
		}
	}
	return out
}

// askedAbout answers the questions the Evaluations calls made since
// the first count of them, which carry action. Each call is one batch
// of a page.
func (s *enforcementStack) askedAbout(since int, action string) [][]policydecision.Question {
	var out [][]policydecision.Question
	for _, batch := range s.rp.asked.batchesAsked()[since:] {
		if batch[0].Action == action {
			out = append(out, batch)
		}
	}
	return out
}

// TestEnforcement_AListHidesAnObjectTheServiceRefusesBesideOneItAllows
// seeds two objects of each kind a list and the search filter, and
// writes a guardrail forbidding the read of one of them. The two are
// listed before it, which is the control that the guardrail is what
// narrows; after it the list and the search answer the other alone. The
// page is one Evaluations call of both objects, each asked as a probe
// on its own GRN (ADR-0148 item 2).
func TestEnforcement_AListHidesAnObjectTheServiceRefusesBesideOneItAllows(t *testing.T) {
	for _, k := range listedKinds() {
		for _, via := range []string{"list", "search"} {
			t.Run(k.name+" by "+via, func(t *testing.T) {
				s := newEnforcementStack(t, realDecisions)
				shown, hidden := k.seed(t, s, "ops-a"), k.seed(t, s, "ops-b")
				s.rp.deliver(t)
				path, kind := k.path, ""
				if via == "search" {
					path, kind = "/orgs/acme/search?"+url.Values{"q": {"ops"}}.Encode(), k.searchKind
				}
				require.Equal(t, []string{shown.String(), hidden.String()}, s.grns(t, path, kind, s.owner),
					"control: both are listed before the guardrail")

				s.forbid(t, `forbid (principal, action == Iam::Action::"`+k.action[len("iam:"):]+
					`", resource == `+k.entity+`::"`+hidden.String()+`");`)
				before := len(s.rp.asked.batchesAsked())
				assert.Equal(t, []string{shown.String()}, s.grns(t, path, kind, s.owner),
					"the guardrail hides the one it names and lists the other")

				batches := s.askedAbout(before, k.action)
				require.Len(t, batches, 1, "one Evaluations call for the page")
				assert.Equal(t, []policydecision.Question{
					{Subject: s.ownerGRN, Action: k.action, Resource: shown, Probe: true},
					{Subject: s.ownerGRN, Action: k.action, Resource: hidden, Probe: true},
				}, batches[0], "each object is a probe on its own GRN")
			})
		}
	}
}

// TestEnforcement_APolicyListIsAskedOnTheOrganization lists and
// searches a policy of the platform's own store. The service holds no
// entity for it, so each question is put on the policy's organization
// and the policy is listed (ADR-0171 item 7); a question on the policy
// itself would be denied to everyone and list nothing.
func TestEnforcement_APolicyListIsAskedOnTheOrganization(t *testing.T) {
	for _, via := range []string{"/orgs/acme/policies", "/orgs/acme/search?q=held"} {
		t.Run(via, func(t *testing.T) {
			s := newEnforcementStack(t, realDecisions)
			policy, err := grn.Parse(activePolicy(t, s.policies, s.acme, "held",
				statementDoc("held", wire.EffectAllow, org.ActionOrgRead, s.acme.String())))
			require.NoError(t, err)
			kind := ""
			if via != "/orgs/acme/policies" {
				kind = "policy"
			}
			before := len(s.rp.asked.batchesAsked())

			assert.Equal(t, []string{policy.String()}, s.grns(t, via, kind, s.owner))

			batches := s.askedAbout(before, org.ActionPolicyRead)
			require.NotEmpty(t, batches)
			assert.Equal(t, []policydecision.Question{
				{Subject: s.ownerGRN, Action: org.ActionPolicyRead, Resource: s.acme, Probe: true},
			}, batches[len(batches)-1], "the question is on the organization")
		})
	}
}

// failingBatches is the policy service whose Evaluations call fails
// once failing is set and whose single questions are always answered,
// so the organization's own check passes and only the page's filter
// meets the failure.
type failingBatches struct {
	policydecision.Service
	failing *atomic.Bool
}

func (f failingBatches) Evaluations(ctx context.Context, qs []policydecision.Question) ([]policydecision.Decision, error) {
	if f.failing.Load() {
		return nil, errors.New("policy service unreachable")
	}
	return f.Service.Evaluations(ctx, qs)
}

// TestEnforcement_AFailingServiceIsUnavailableNotAnEmptyList fails the
// Evaluations call a page makes while the service answers everything
// else: each list and the search answer 503 'unavailable', not an empty
// list and not the unfiltered one. The same requests before the call
// fails are the control.
func TestEnforcement_AFailingServiceIsUnavailableNotAnEmptyList(t *testing.T) {
	var failing atomic.Bool
	s := newEnforcementStack(t, func(rp *realPolicy) policydecision.Service {
		return failingBatches{Service: rp.asked, failing: &failing}
	})
	for _, k := range listedKinds() {
		k.seed(t, s, "ops-a")
	}
	s.rp.deliver(t)
	for _, k := range listedKinds() {
		t.Run(k.name, func(t *testing.T) {
			failing.Store(false)
			assert.Len(t, s.grns(t, k.path, "", s.owner), 1, "control: the list when the service answers")
			status, body := s.get(t, "/orgs/acme/search?q=ops", s.owner)
			assert.Equal(t, http.StatusOK, status, "control: the search when the service answers: %s", body)

			failing.Store(true)
			status, body = s.get(t, k.path, s.owner)
			assert.Equal(t, http.StatusServiceUnavailable, status, body)
			assert.Contains(t, body, "unavailable")
			status, body = s.get(t, "/orgs/acme/search?q=ops", s.owner)
			assert.Equal(t, http.StatusServiceUnavailable, status, body)
		})
	}
}

// TestEnforcement_PermitWriteIsAnsweredAsTheServiceDecidesAPermitWrite
// asks the permission check for iam:permit.write on the organization
// and writes a permit through the relay, as the owner, an administrator
// and a member, then again after a guardrail forbids the action. The
// check and the write agree each time: only the owner is allowed, and
// the guardrail refuses the owner both.
func TestEnforcement_PermitWriteIsAnsweredAsTheServiceDecidesAPermitWrite(t *testing.T) {
	s := newEnforcementStack(t, realDecisions)
	writer := &policyv1.Subject{Type: "Iam::User", Id: s.ownerGRN.String()}
	write := func(cookie *http.Cookie) int {
		status, got := browse(t, http.MethodPost, s.url+"/orgs/acme/iam-policies", cookie,
			`{"name":"n","body":`+textBody("permit", relayPermit)+`}`)
		if status == http.StatusCreated {
			made := policyID(t, got)
			t.Cleanup(func() {
				_, _ = s.rp.client.DeletePolicy(context.Background(), &policyv1.DeletePolicyRequest{
					Writer: writer, Org: s.acme.String(), Policy: made,
				})
			})
		}
		return status
	}
	holders := []struct {
		name   string
		cookie *http.Cookie
		allow  bool
	}{{"owner", s.owner, true}, {"administrator", s.admin, false}, {"member", s.member, false}}

	for _, h := range holders {
		decision := apigen.DecisionDeny
		status := http.StatusForbidden
		if h.allow {
			decision, status = apigen.DecisionAllow, http.StatusCreated
		}
		assert.Equal(t, decision, s.check(t, h.cookie, org.ActionPermitWrite), "the check for the %s", h.name)
		assert.Equal(t, status, write(h.cookie), "the write by the %s", h.name)
	}

	s.forbid(t, `forbid (principal, action == Iam::Action::"permit.write", resource);`)
	assert.Equal(t, apigen.DecisionDeny, s.check(t, s.owner, org.ActionPermitWrite), "a guardrail forbids the owner the check")
	assert.Equal(t, http.StatusForbidden, write(s.owner), "and the write")
}

// TestEnforcement_APolicyWriteIsAskedOnTheOrganization asks the
// permission check for each policy write that names an existing
// policy, on the organization alone, as an owner and a member. The
// service decides every policy write on the organization, so a policy
// not yet written can be asked about there (ADR-0172): the owner is
// allowed each and the member refused, and a refusal is a decision,
// not the 400 an action not accepted there answers.
func TestEnforcement_APolicyWriteIsAskedOnTheOrganization(t *testing.T) {
	s := newEnforcementStack(t, realDecisions)
	for _, action := range []string{
		org.ActionPolicyAttach, org.ActionPolicyDetach, org.ActionPolicyUpdate, org.ActionPolicyDelete,
	} {
		assert.Equal(t, apigen.DecisionAllow, s.check(t, s.owner, action), "the owner, %s", action)
		assert.Equal(t, apigen.DecisionDeny, s.check(t, s.member, action), "the member, %s", action)
	}
}
