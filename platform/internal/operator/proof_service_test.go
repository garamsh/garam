package operator_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// currentGrant is the GrantStore as a proof reads it: one current grant
// or none, and a read that can fail. The writes are not this service's.
type currentGrant struct {
	operator.GrantStore
	grant *operator.Grant
	err   error
}

func (g currentGrant) CurrentGrant(context.Context, grn.GRN, grn.GRN) (*operator.Grant, error) {
	if g.err != nil {
		return nil, g.err
	}
	if g.grant == nil {
		return nil, operator.ErrGrantNotFound
	}
	return g.grant, nil
}

// placements is the AssignmentReader in memory.
type placements struct {
	at   map[grn.GRN]operator.Assignment
	err  error
	read int
}

func (p *placements) CurrentAssignment(_ context.Context, agent grn.GRN) (operator.Assignment, error) {
	p.read++
	if p.err != nil {
		return operator.Assignment{}, p.err
	}
	a, ok := p.at[agent]
	if !ok {
		return operator.Assignment{}, errorx.New(errorx.KindNotFound, "agent not found")
	}
	return a, nil
}

// policy is the AudienceAuthorizer in memory: it records what it was
// asked and answers err.
type policy struct {
	err    error
	object grn.GRN
	action string
}

func (p *policy) Authorize(_ context.Context, _, _, object grn.GRN, action string) error {
	p.object, p.action = object, action
	return p.err
}

// proofFixture is one organization's hosted operator, the controller
// its grant covers for agent:activate, and an agent placed on it.
type proofFixture struct {
	now        time.Time
	org        grn.GRN
	hosted     grn.GRN
	controller grn.GRN
	agent      grn.GRN
	grant      operator.Grant
	placements *placements
	policy     *policy
}

func newProofFixture() *proofFixture {
	now := time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)
	controller := operatorGRN("acme", "controller")
	agent := agentGRN("acme", "bot")
	return &proofFixture{
		now:        now,
		org:        grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme"),
		hosted:     operatorGRN("acme", "hosted"),
		controller: controller,
		agent:      agent,
		grant: operator.Grant{Controllers: []grn.GRN{controller}, ExpiresAt: now.Add(time.Hour),
			Operations: []operator.Operation{operator.OperationAgentActivate}},
		placements: &placements{at: map[grn.GRN]operator.Assignment{agent: {Assignee: controller, Epoch: "7"}}},
		policy:     &policy{},
	}
}

func (f *proofFixture) prove(store operator.GrantStore, agent *grn.GRN) (*operator.ControllerProof, error) {
	svc := operator.NewProofService(store, f.placements, f.policy,
		operator.Config{Now: func() time.Time { return f.now }})
	return svc.Prove(context.Background(), f.hosted, f.controller, agent)
}

// TestProofService_ProvesAControllerItsGrantCovers holds the session
// proof, which names no agent. The covering grant is the control; each
// refusal differs from it in one thing.
func TestProofService_ProvesAControllerItsGrantCovers(t *testing.T) {
	f := newProofFixture()
	proof, err := f.prove(currentGrant{grant: &f.grant}, nil)
	require.NoError(t, err)
	assert.Equal(t, &operator.ControllerProof{Controller: f.controller, Org: f.org}, proof)
	assert.Equal(t, f.org, f.policy.object, "a session is decided on the organization")
	assert.Equal(t, "agent:activate", f.policy.action)

	ended := f.now
	tests := map[string]struct {
		store  func(g operator.Grant) operator.GrantStore
		policy error
		kind   errorx.Kind
	}{
		"no current grant": {store: func(operator.Grant) operator.GrantStore { return currentGrant{} },
			kind: errorx.KindPermissionDenied},
		"an ended grant": {store: func(g operator.Grant) operator.GrantStore { g.EndedAt = &ended; return currentGrant{grant: &g} },
			kind: errorx.KindPermissionDenied},
		"an expired grant": {store: func(g operator.Grant) operator.GrantStore {
			g.ExpiresAt = f.now
			return currentGrant{grant: &g}
		}, kind: errorx.KindPermissionDenied},
		"a grant without agent:activate": {store: func(g operator.Grant) operator.GrantStore {
			g.Operations = []operator.Operation{operator.OperationAgentCreate, operator.OperationAgentConfigure}
			return currentGrant{grant: &g}
		}, kind: errorx.KindPermissionDenied},
		"a grant for another controller": {store: func(g operator.Grant) operator.GrantStore {
			g.Controllers = []grn.GRN{operatorGRN("acme", "other")}
			return currentGrant{grant: &g}
		}, kind: errorx.KindPermissionDenied},
		"a grant that could not be read": {store: func(operator.Grant) operator.GrantStore {
			return currentGrant{err: errorx.New(errorx.KindInternal, "connection refused")}
		}, kind: errorx.KindUnavailable},
		"a Deny": {store: func(g operator.Grant) operator.GrantStore { return currentGrant{grant: &g} },
			policy: errorx.New(errorx.KindPermissionDenied, "denied"), kind: errorx.KindPermissionDenied},
		"a policy that could not be decided": {store: func(g operator.Grant) operator.GrantStore { return currentGrant{grant: &g} },
			policy: errorx.New(errorx.KindInternal, "engine down"), kind: errorx.KindUnavailable},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newProofFixture()
			f.policy.err = tt.policy
			proof, err := f.prove(tt.store(f.grant), nil)
			require.Error(t, err)
			assert.Nil(t, proof)
			assert.Equal(t, tt.kind, errorx.KindOf(err), "%v", err)
		})
	}
}

// TestProofService_BindsAnAgentToItsCurrentPlacement holds the
// agent-bound proof. The agent placed on the controller is the control:
// it answers the assignment's epoch and is decided on the agent itself.
func TestProofService_BindsAnAgentToItsCurrentPlacement(t *testing.T) {
	f := newProofFixture()
	proof, err := f.prove(currentGrant{grant: &f.grant}, &f.agent)
	require.NoError(t, err)
	assert.Equal(t, &operator.ProvenAgent{Agent: f.agent, Epoch: "7"}, proof.Agent)
	assert.Equal(t, f.agent, f.policy.object, "an agent's lifecycle is decided on the agent")

	other := agentGRN("globex", "bot")
	unplaced := agentGRN("acme", "unplaced")
	tests := map[string]struct {
		agent grn.GRN
		place func(p *placements)
		kind  errorx.Kind
	}{
		"another organization's agent": {agent: other, place: func(p *placements) {
			p.at[other] = operator.Assignment{Assignee: operatorGRN("acme", "controller"), Epoch: "1"}
		}, kind: errorx.KindNotFound},
		"an operator named as the agent": {agent: operatorGRN("acme", "controller"), place: func(*placements) {},
			kind: errorx.KindNotFound},
		"an agent assigned nowhere": {agent: unplaced, place: func(*placements) {}, kind: errorx.KindNotFound},
		"an agent assigned elsewhere": {agent: agentGRN("acme", "bot"), place: func(p *placements) {
			p.at[agentGRN("acme", "bot")] = operator.Assignment{Assignee: operatorGRN("acme", "other"), Epoch: "8"}
		}, kind: errorx.KindPermissionDenied},
		"an assignment that could not be read": {agent: agentGRN("acme", "bot"), place: func(p *placements) {
			p.err = errorx.New(errorx.KindInternal, "connection refused")
		}, kind: errorx.KindUnavailable},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			f := newProofFixture()
			tt.place(f.placements)
			proof, err := f.prove(currentGrant{grant: &f.grant}, &tt.agent)
			require.Error(t, err)
			assert.Nil(t, proof)
			assert.Equal(t, tt.kind, errorx.KindOf(err), "%v", err)
		})
	}

	t.Run("another organization's agent is refused unread", func(t *testing.T) {
		f := newProofFixture()
		_, err := f.prove(currentGrant{grant: &f.grant}, &other)
		require.Error(t, err)
		assert.Zero(t, f.placements.read)
	})
}

// TestParseController_AnswersOnlyAnOperatorOfTheCallersOrganization
// holds the refusal decided before anything is read. The caller's own
// controller is the control.
func TestParseController_AnswersOnlyAnOperatorOfTheCallersOrganization(t *testing.T) {
	hosted := operatorGRN("acme", "hosted")
	controller, err := operator.ParseController(hosted, "grn:acme:default:operator:controller")
	require.NoError(t, err)
	assert.Equal(t, operatorGRN("acme", "controller"), controller)

	for raw, kind := range map[string]errorx.Kind{
		"not a grn":                         errorx.KindInvalidArgument,
		"grn:globex:default:operator:other": errorx.KindNotFound,
		"grn:acme:default:agent:bot":        errorx.KindNotFound,
	} {
		_, err := operator.ParseController(hosted, raw)
		assert.Equal(t, kind, errorx.KindOf(err), raw)
	}
}
