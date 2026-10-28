// Internal test package: the double below builds the Agents and
// Assignments it answers with out of this package's own fields.
package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
)

// memoryStore is a Store in memory. It keeps the assignment rules the
// service depends on — one operator per agent, an epoch that rises with
// every assignment — and fences a state report as Postgres fences it:
// at the epoch the agent is assigned at and at no other.
//
// What the service does not call comes from the nil Store embedded
// here and panics if called.
type memoryStore struct {
	Store

	agents      map[grn.GRN]*Agent
	assignments map[grn.GRN]*Assignment
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		agents:      map[grn.GRN]*Agent{},
		assignments: map[grn.GRN]*Assignment{},
	}
}

func (s *memoryStore) Create(_ context.Context, a *Agent) error {
	if _, held := s.agents[a.GRN]; held {
		return ErrAgentAlreadyExists
	}
	s.agents[a.GRN] = a
	return nil
}

func (s *memoryStore) Get(_ context.Context, g grn.GRN) (*Agent, error) {
	a, held := s.agents[g]
	if !held {
		return nil, ErrAgentNotFound
	}
	return a, nil
}

func (s *memoryStore) ListByOrg(_ context.Context, orgGRN grn.GRN, _ listfilter.Filter) ([]*Agent, error) {
	agents := make([]*Agent, 0, len(s.agents))
	for _, a := range s.agents {
		if a.Organization == orgGRN {
			agents = append(agents, a)
		}
	}
	return agents, nil
}

func (s *memoryStore) Assign(_ context.Context, agentGRN, operatorGRN grn.GRN) (*Assignment, error) {
	if _, held := s.agents[agentGRN]; !held {
		return nil, ErrAgentNotFound
	}
	epoch := 1
	if prior, held := s.assignments[agentGRN]; held {
		epoch = prior.Epoch + 1
	}
	assignment := &Assignment{Agent: agentGRN, Assignee: operatorGRN, Epoch: epoch}
	s.assignments[agentGRN] = assignment
	return assignment, nil
}

func (s *memoryStore) GetAssignment(_ context.Context, agentGRN grn.GRN) (*Assignment, error) {
	assignment, held := s.assignments[agentGRN]
	if !held {
		return nil, ErrAssignmentNotFound
	}
	return assignment, nil
}

func (s *memoryStore) ListAssignmentsByOrg(_ context.Context, orgGRN grn.GRN) ([]*Assignment, error) {
	assignments := make([]*Assignment, 0, len(s.assignments))
	for agentGRN, assignment := range s.assignments {
		if a, held := s.agents[agentGRN]; held && a.Organization == orgGRN {
			assignments = append(assignments, assignment)
		}
	}
	return assignments, nil
}

func (s *memoryStore) ReportProvisioningState(_ context.Context, agentGRN grn.GRN, epoch int, state ProvisioningState) (*Agent, error) {
	a, err := s.reportable(agentGRN, epoch)
	if err != nil {
		return nil, err
	}
	a.ProvisioningState = state
	return a, nil
}

func (s *memoryStore) ReportRuntimeState(_ context.Context, agentGRN grn.GRN, epoch int, state RuntimeState, _ Fencing) (*Agent, error) {
	a, err := s.reportable(agentGRN, epoch)
	if err != nil {
		return nil, err
	}
	a.RuntimeState = state
	return a, nil
}

// reportable is the fence both reports pass: the agent exists, and the
// epoch carried is the one its assignment is on. An agent no operator
// holds has no current epoch, so every report about it is stale.
func (s *memoryStore) reportable(agentGRN grn.GRN, epoch int) (*Agent, error) {
	a, held := s.agents[agentGRN]
	if !held {
		return nil, ErrAgentNotFound
	}
	assignment, assigned := s.assignments[agentGRN]
	if !assigned || assignment.Epoch != epoch {
		return nil, ErrStaleEpoch
	}
	return a, nil
}

// place creates an agent in operator's organization and assigns it to
// operator at the first epoch, as a creation does.
func place(t *testing.T, store *memoryStore, operator grn.GRN) *Assignment {
	t.Helper()
	orgGRN := grn.Org(operator.Organization)
	g, err := NewGRN(orgGRN)
	require.NoError(t, err)
	a, err := New(g, orgGRN)
	require.NoError(t, err)
	require.NoError(t, store.Create(context.Background(), a))
	assignment, err := store.Assign(context.Background(), g, operator)
	require.NoError(t, err)
	return assignment
}

func operatorGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, ServiceTypeOperator, id)
}

// TestService_ReportProvisioningStateIsAcceptedFromTheHolderAtTheCurrentEpoch
// is the operator's half of the two facts ADR-0012 keeps. Every
// refusal sits beside the report that lands, because a refusal on its
// own holds equally for a service that records nothing, and the stored
// agent is what says whether a call moved anything.
//
// The epoch fence is exercised on its own: the holder is reassigned to
// itself, which raises the epoch without changing who holds the agent,
// so what refuses the earlier epoch is the epoch and not the operator.
func TestService_ReportProvisioningStateIsAcceptedFromTheHolderAtTheCurrentEpoch(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	svc := mustService(t, store)

	holder := operatorGRN("acme", "one")
	sibling := operatorGRN("acme", "two")
	stranger := operatorGRN("globex", "one")

	held := place(t, store, holder)

	require.NoError(t, svc.ReportProvisioningState(ctx, holder, held.Agent, 1, ProvisioningStateReady))
	stored, err := store.Get(ctx, held.Agent)
	require.NoError(t, err)
	assert.Equal(t, ProvisioningStateReady, stored.ProvisioningState)
	assert.Equal(t, RuntimeStateUnknown, stored.RuntimeState,
		"the operator's report moved the fact the agent reports about itself")

	refused := map[string]struct {
		caller grn.GRN
		agent  grn.GRN
		err    error
	}{
		"another operator of the same organization": {sibling, held.Agent, ErrNotAssigned},
		"an operator of another organization":       {stranger, held.Agent, ErrNotAssigned},
		"an agent that does not exist":              {holder, grn.New("acme", grn.DefaultProject, ServiceType, "0f0f0f0f0f0f0f0f"), ErrNotAssigned},
		"the agent reporting on its own pod":        {held.Agent, held.Agent, ErrNotAnOperator},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, svc.ReportProvisioningState(ctx, tt.caller, tt.agent, 1, ProvisioningStateFailed), tt.err)
		})
	}
	stored, err = store.Get(ctx, held.Agent)
	require.NoError(t, err)
	assert.Equal(t, ProvisioningStateReady, stored.ProvisioningState, "a refused report was recorded")

	// Reassigning the agent to the operator already holding it raises
	// the epoch and changes nothing else, so the earlier epoch is
	// refused by the fence rather than by the operator comparison.
	reassigned, err := store.Assign(ctx, held.Agent, holder)
	require.NoError(t, err)
	require.Equal(t, 2, reassigned.Epoch)
	assert.ErrorIs(t, svc.ReportProvisioningState(ctx, holder, held.Agent, 1, ProvisioningStateFailed), ErrStaleEpoch)
	require.NoError(t, svc.ReportProvisioningState(ctx, holder, held.Agent, 2, ProvisioningStateFailed),
		"the same call at the current epoch")
	stored, err = store.Get(ctx, held.Agent)
	require.NoError(t, err)
	assert.Equal(t, ProvisioningStateFailed, stored.ProvisioningState)
}

// TestService_ReportRuntimeStateIsAcceptedOnlyFromTheAgentItself is the
// agent's half of the same two facts. What bounds it is the identity
// the connection proved, so the refusals beside the report that lands
// are callers this route would answer if the caller were not compared
// to the subject at all — and the epoch fence is shown on its own, by
// an epoch the assignment has moved past.
func TestService_ReportRuntimeStateIsAcceptedOnlyFromTheAgentItself(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	svc := mustService(t, store)

	operator := operatorGRN("acme", "one")
	held := place(t, store, operator)
	sibling := place(t, store, operator)

	require.NoError(t, svc.ReportRuntimeState(ctx, held.Agent, held.Agent, 1, RuntimeStateConnected, Fencing{Agent: held.Agent}))
	stored, err := store.Get(ctx, held.Agent)
	require.NoError(t, err)
	assert.Equal(t, RuntimeStateConnected, stored.RuntimeState)
	assert.Equal(t, ProvisioningStatePending, stored.ProvisioningState,
		"the agent's report moved the fact its operator reports")

	refused := map[string]struct {
		caller grn.GRN
		agent  grn.GRN
		err    error
	}{
		"an agent of the same organization": {sibling.Agent, held.Agent, ErrNotTheAgent},
		"the operator that holds the agent": {operator, held.Agent, ErrNotTheAgent},
		"an agent garam has no record of":   {grn.New("acme", grn.DefaultProject, ServiceType, "0f0f0f0f0f0f0f0f"), grn.New("acme", grn.DefaultProject, ServiceType, "0f0f0f0f0f0f0f0f"), ErrAgentNotFound},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, svc.ReportRuntimeState(ctx, tt.caller, tt.agent, 1, RuntimeStateDisconnected, Fencing{Agent: tt.agent}), tt.err)
		})
	}
	stored, err = store.Get(ctx, held.Agent)
	require.NoError(t, err)
	assert.Equal(t, RuntimeStateConnected, stored.RuntimeState, "a refused report was recorded")

	// The epoch the agent carries is its assignment's, and a
	// reassignment moves it: the agent is still the only caller this
	// route answers, and it is answered at the current epoch alone.
	reassigned, err := store.Assign(ctx, held.Agent, operator)
	require.NoError(t, err)
	require.Equal(t, 2, reassigned.Epoch)
	assert.ErrorIs(t, svc.ReportRuntimeState(ctx, held.Agent, held.Agent, 1, RuntimeStateDisconnected, Fencing{Agent: held.Agent}), ErrStaleEpoch)
	require.NoError(t, svc.ReportRuntimeState(ctx, held.Agent, held.Agent, 2, RuntimeStateDisconnected, Fencing{Agent: held.Agent}),
		"the same call at the current epoch")
	stored, err = store.Get(ctx, held.Agent)
	require.NoError(t, err)
	assert.Equal(t, RuntimeStateDisconnected, stored.RuntimeState)
}

// mustService is the service over store with the default
// configuration and no token store.
func mustService(t *testing.T, store Store) Service {
	t.Helper()
	svc, err := NewService(store, nil, nil, Config{})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}
