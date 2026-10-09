// Internal test package: the double below builds the Agents and
// Assignments it answers with out of this package's own fields.
package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
)

// memoryStore is a Store in memory. It keeps the assignment rules the
// service depends on — one operator per agent, an epoch that rises with
// every assignment — and the claim rules beside them: a definition is
// claimed once, and the claim is where its agent comes to exist
// (ADR-0033). A state report is fenced as Postgres fences it: at the
// epoch the agent is assigned at and at no other.
//
// The service reads no template and renames no definition, so those
// methods come from the nil Store embedded here and panic if called.
type memoryStore struct {
	Store

	agents      map[grn.GRN]*Agent
	assignments map[grn.GRN]*Assignment
	definitions map[grn.GRN]*Definition
	// fenced holds the agents whose execution has been activated.
	fenced map[grn.GRN]bool
	// switched holds the agents whose cutover is switched.
	switched map[grn.GRN]bool
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		agents:      map[grn.GRN]*Agent{},
		assignments: map[grn.GRN]*Assignment{},
		definitions: map[grn.GRN]*Definition{},
		fenced:      map[grn.GRN]bool{},
		switched:    map[grn.GRN]bool{},
	}
}

func (s *memoryStore) AdmitLegacyIssuance(_ context.Context, agentGRN grn.GRN) error {
	if _, held := s.agents[agentGRN]; !held {
		return ErrAgentNotFound
	}
	if s.fenced[agentGRN] {
		return ErrIssuedByLifecycle
	}
	if s.switched[agentGRN] {
		return ErrCutOver
	}
	return nil
}

func (s *memoryStore) Create(_ context.Context, a *Agent) error {
	if _, held := s.agents[a.grn]; held {
		return ErrAgentAlreadyExists
	}
	s.agents[a.grn] = a
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
		if a.orgGRN == orgGRN {
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
		epoch = prior.epoch + 1
	}
	assignment := &Assignment{agentGRN: agentGRN, assigneeGRN: operatorGRN, epoch: epoch}
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
		if a, held := s.agents[agentGRN]; held && a.orgGRN == orgGRN {
			assignments = append(assignments, assignment)
		}
	}
	return assignments, nil
}

// keepDefinition stores a definition as one written before garam
// stopped accepting them (ADR-0090).
func (s *memoryStore) keepDefinition(d *Definition) {
	s.definitions[d.agentGRN] = d
}

func (s *memoryStore) ListDefinitionsByOrg(context.Context, grn.GRN) ([]*Definition, error) {
	return nil, errorx.New(errorx.KindUnimplemented, "the operator reads by operator, not by org")
}

func (s *memoryStore) CountByOrg(context.Context, grn.GRN) (OrgCounts, error) {
	return OrgCounts{}, errorx.New(errorx.KindUnimplemented, "the operator reads by operator, not by org")
}

func (s *memoryStore) ListDefinitionsByOperator(_ context.Context, operatorGRN grn.GRN) ([]*DefinitionState, error) {
	states := make([]*DefinitionState, 0, len(s.definitions))
	for _, d := range s.definitions {
		if d.operatorGRN != operatorGRN {
			continue
		}
		states = append(states, NewDefinitionState(d, s.assignments[d.agentGRN]))
	}
	return states, nil
}

func (s *memoryStore) Claim(_ context.Context, agentGRN, operatorGRN grn.GRN) (*Assignment, error) {
	d, held := s.definitions[agentGRN]
	// A definition naming another operator answers as one that does
	// not exist: the claim keeps the read's scoping.
	if !held || d.operatorGRN != operatorGRN {
		return nil, ErrDefinitionNotFound
	}
	if _, claimed := s.assignments[agentGRN]; claimed {
		return nil, ErrDefinitionClaimed
	}
	a, err := New(agentGRN, d.orgGRN)
	if err != nil {
		return nil, err
	}
	s.agents[agentGRN] = a
	assignment := &Assignment{agentGRN: agentGRN, assigneeGRN: operatorGRN, epoch: 1}
	s.assignments[agentGRN] = assignment
	return assignment, nil
}

func (s *memoryStore) ReportProvisioningState(_ context.Context, agentGRN grn.GRN, epoch int, state ProvisioningState) (*Agent, error) {
	a, err := s.reportable(agentGRN, epoch)
	if err != nil {
		return nil, err
	}
	a.provisioning = state
	return a, nil
}

func (s *memoryStore) ReportRuntimeState(_ context.Context, agentGRN grn.GRN, epoch int, state RuntimeState, _ Fencing) (*Agent, error) {
	a, err := s.reportable(agentGRN, epoch)
	if err != nil {
		return nil, err
	}
	a.runtime = state
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
	if !assigned || assignment.epoch != epoch {
		return nil, ErrStaleEpoch
	}
	return a, nil
}

// signer is an Issuer recording every subject it signed for, so a
// refusal is read as a certificate that was not issued rather than as
// an error beside one that was.
type signer struct {
	signed []grn.GRN
	// during, when set, runs while a certificate is being signed.
	during func()
}

func (s *signer) Issue(_ context.Context, subject grn.GRN) (*Certificate, error) {
	s.signed = append(s.signed, subject)
	if s.during != nil {
		s.during()
	}
	return &Certificate{CertificatePEM: []byte(subject.String())}, nil
}

func operatorGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, ServiceTypeOperator, id)
}

// TestService_CreateAssignsTheAgentToTheCallingOperator covers what an
// operator gets back: a GRN garam minted in the operator's own
// organization, held by that operator at an epoch. The caller that is
// not an operator sits beside it, so what refuses is the caller rather
// than the call.
func TestService_CreateAssignsTheAgentToTheCallingOperator(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	svc := mustService(t, store, &signer{})
	operator := operatorGRN("acme", "one")

	assignment, err := svc.Create(ctx, operator)
	require.NoError(t, err)
	assert.Equal(t, operator, assignment.Assignee())
	assert.Equal(t, 1, assignment.Epoch())

	created := assignment.Agent()
	assert.Equal(t, "acme", created.Organization, "the agent belongs to the operator's organization")
	assert.Equal(t, ServiceType, created.ServiceType)
	stored, err := store.Get(ctx, created)
	require.NoError(t, err)
	assert.Equal(t, grn.New(grn.RootOwner, grn.DefaultProject, serviceTypeOrg, "acme"), stored.Organization())
	assert.Equal(t, ProvisioningStatePending, stored.ProvisioningState())

	second, err := svc.Create(ctx, operator)
	require.NoError(t, err)
	assert.NotEqual(t, created, second.Agent(), "garam mints an identifier per agent")

	_, err = svc.Create(ctx, grn.New("acme", grn.DefaultProject, ServiceType, "deadbeefdeadbeef"))
	assert.ErrorIs(t, err, ErrNotAnOperator, "an agent authenticates here and creates nothing")
	assert.Len(t, store.agents, 2, "the refused call stored no agent")
}

// TestService_IssueCertificateAnswersOnlyForAnAgentTheOperatorHolds is
// the authorization story of this surface (ADR-0015). Every refusal is
// asserted beside the fetch that succeeds, because a refusal on its own
// holds equally for a service that refuses everything, and the signer
// records what it signed so a refusal is a certificate that does not
// exist rather than an error beside one that does.
func TestService_IssueCertificateAnswersOnlyForAnAgentTheOperatorHolds(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	issuer := &signer{}
	svc := mustService(t, store, issuer)

	holder := operatorGRN("acme", "one")
	sibling := operatorGRN("acme", "two")
	stranger := operatorGRN("globex", "one")

	held, err := svc.Create(ctx, holder)
	require.NoError(t, err)
	foreign, err := svc.Create(ctx, stranger)
	require.NoError(t, err)

	// An agent of the organization that nobody was assigned: the
	// relation is what answers, not the row's existence.
	unassignedOrg := grn.New(grn.RootOwner, grn.DefaultProject, serviceTypeOrg, "acme")
	unassignedGRN, err := NewGRN(unassignedOrg)
	require.NoError(t, err)
	unassigned, err := New(unassignedGRN, unassignedOrg)
	require.NoError(t, err)
	require.NoError(t, store.Create(ctx, unassigned))

	cert, err := svc.IssueCertificate(ctx, holder, held.Agent())
	require.NoError(t, err)
	assert.Equal(t, []byte(held.Agent().String()), cert.CertificatePEM)

	refused := map[string]struct {
		operator grn.GRN
		agent    grn.GRN
		err      error
	}{
		"another operator of the same organization": {sibling, held.Agent(), ErrNotAssigned},
		"an operator of another organization":       {stranger, held.Agent(), ErrNotAssigned},
		"an agent assigned to nobody":               {holder, unassignedGRN, ErrNotAssigned},
		"an agent that does not exist":              {holder, grn.New("acme", grn.DefaultProject, ServiceType, "0f0f0f0f0f0f0f0f"), ErrNotAssigned},
		"an agent asking for another agent's":       {held.Agent(), foreign.Agent(), ErrNotAnOperator},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			_, err := svc.IssueCertificate(ctx, tt.operator, tt.agent)
			assert.ErrorIs(t, err, tt.err)
		})
	}

	// The operator of the other organization holds its own agent, so
	// the refusals above rest on which agent is asked for rather than
	// on which certificate would be signed.
	_, err = svc.IssueCertificate(ctx, stranger, foreign.Agent())
	require.NoError(t, err)

	// A reassignment moves the row to the next epoch, and the operator
	// it replaced stops being answered while its successor is.
	reassigned, err := store.Assign(ctx, held.Agent(), sibling)
	require.NoError(t, err)
	require.Equal(t, 2, reassigned.Epoch())
	_, err = svc.IssueCertificate(ctx, holder, held.Agent())
	assert.ErrorIs(t, err, ErrNotAssigned, "the operator the reassignment replaced")
	_, err = svc.IssueCertificate(ctx, sibling, held.Agent())
	require.NoError(t, err)

	assert.Equal(t,
		[]grn.GRN{held.Agent(), foreign.Agent(), held.Agent()},
		issuer.signed,
		"nothing was signed for a caller that was refused")
}

// TestService_IssueCertificateIsAnOperatorsRouteForALegacyAgent holds
// the two callers this route no longer answers: an agent asking for
// its own certificate, which renews inside its execution fence
// instead (ADR-0078), and the assigned operator once the agent's
// execution is fenced. The same operator's request for the same agent
// before the cutover is the control.
func TestService_IssueCertificateIsAnOperatorsRouteForALegacyAgent(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	issuer := &signer{}
	svc := mustService(t, store, issuer)

	operator := operatorGRN("acme", "one")
	held, err := svc.Create(ctx, operator)
	require.NoError(t, err)

	_, err = svc.IssueCertificate(ctx, held.Agent(), held.Agent())
	assert.ErrorIs(t, err, ErrNotAnOperator, "an agent renewing its own certificate here")

	_, err = svc.IssueCertificate(ctx, operator, held.Agent())
	require.NoError(t, err, "control: the assigned operator of a legacy agent")

	store.fenced[held.Agent()] = true
	_, err = svc.IssueCertificate(ctx, operator, held.Agent())
	assert.ErrorIs(t, err, ErrIssuedByLifecycle, "the assigned operator once the agent is fenced")

	assert.Equal(t, []grn.GRN{held.Agent()}, issuer.signed,
		"nothing was signed for a caller that was refused")
}

// TestService_IssueCertificateAnswersNothingOnceTheCutoverIsSwitched
// holds the operator's route closed for an agent whose cutover is
// switched, including a switch that commits while the certificate is
// being signed: the read after signing is what decides (ADR-0086). The
// same request with no switch is the control.
func TestService_IssueCertificateAnswersNothingOnceTheCutoverIsSwitched(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	issuer := &signer{}
	svc := mustService(t, store, issuer)
	operator := operatorGRN("acme", "one")
	held, err := svc.Create(ctx, operator)
	require.NoError(t, err)

	got, err := svc.IssueCertificate(ctx, operator, held.Agent())
	require.NoError(t, err, "control: the assigned operator of a legacy agent")
	assert.NotNil(t, got)

	issuer.during = func() { store.switched[held.Agent()] = true }
	got, err = svc.IssueCertificate(ctx, operator, held.Agent())
	assert.ErrorIs(t, err, ErrCutOver, "a switch committing while it signs")
	assert.Nil(t, got)

	issuer.during = nil
	_, err = svc.IssueCertificate(ctx, operator, held.Agent())
	assert.ErrorIs(t, err, ErrCutOver, "once switched")
	assert.Len(t, issuer.signed, 2, "nothing is signed once the switch is read before signing")
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
	svc := mustService(t, store, &signer{})

	holder := operatorGRN("acme", "one")
	sibling := operatorGRN("acme", "two")
	stranger := operatorGRN("globex", "one")

	held, err := svc.Create(ctx, holder)
	require.NoError(t, err)

	require.NoError(t, svc.ReportProvisioningState(ctx, holder, held.Agent(), 1, ProvisioningStateReady))
	stored, err := store.Get(ctx, held.Agent())
	require.NoError(t, err)
	assert.Equal(t, ProvisioningStateReady, stored.ProvisioningState())
	assert.Equal(t, RuntimeStateUnknown, stored.RuntimeState(),
		"the operator's report moved the fact the agent reports about itself")

	refused := map[string]struct {
		caller grn.GRN
		agent  grn.GRN
		err    error
	}{
		"another operator of the same organization": {sibling, held.Agent(), ErrNotAssigned},
		"an operator of another organization":       {stranger, held.Agent(), ErrNotAssigned},
		"an agent that does not exist":              {holder, grn.New("acme", grn.DefaultProject, ServiceType, "0f0f0f0f0f0f0f0f"), ErrNotAssigned},
		"the agent reporting on its own pod":        {held.Agent(), held.Agent(), ErrNotAnOperator},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, svc.ReportProvisioningState(ctx, tt.caller, tt.agent, 1, ProvisioningStateFailed), tt.err)
		})
	}
	stored, err = store.Get(ctx, held.Agent())
	require.NoError(t, err)
	assert.Equal(t, ProvisioningStateReady, stored.ProvisioningState(), "a refused report was recorded")

	// Reassigning the agent to the operator already holding it raises
	// the epoch and changes nothing else, so the earlier epoch is
	// refused by the fence rather than by the operator comparison.
	reassigned, err := store.Assign(ctx, held.Agent(), holder)
	require.NoError(t, err)
	require.Equal(t, 2, reassigned.Epoch())
	assert.ErrorIs(t, svc.ReportProvisioningState(ctx, holder, held.Agent(), 1, ProvisioningStateFailed), ErrStaleEpoch)
	require.NoError(t, svc.ReportProvisioningState(ctx, holder, held.Agent(), 2, ProvisioningStateFailed),
		"the same call at the current epoch")
	stored, err = store.Get(ctx, held.Agent())
	require.NoError(t, err)
	assert.Equal(t, ProvisioningStateFailed, stored.ProvisioningState())
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
	svc := mustService(t, store, &signer{})

	operator := operatorGRN("acme", "one")
	held, err := svc.Create(ctx, operator)
	require.NoError(t, err)
	sibling, err := svc.Create(ctx, operator)
	require.NoError(t, err)

	require.NoError(t, svc.ReportRuntimeState(ctx, held.Agent(), held.Agent(), 1, RuntimeStateConnected, Fencing{Agent: held.Agent()}))
	stored, err := store.Get(ctx, held.Agent())
	require.NoError(t, err)
	assert.Equal(t, RuntimeStateConnected, stored.RuntimeState())
	assert.Equal(t, ProvisioningStatePending, stored.ProvisioningState(),
		"the agent's report moved the fact its operator reports")

	refused := map[string]struct {
		caller grn.GRN
		agent  grn.GRN
		err    error
	}{
		"an agent of the same organization": {sibling.Agent(), held.Agent(), ErrNotTheAgent},
		"the operator that holds the agent": {operator, held.Agent(), ErrNotTheAgent},
		"an agent garam has no record of":   {grn.New("acme", grn.DefaultProject, ServiceType, "0f0f0f0f0f0f0f0f"), grn.New("acme", grn.DefaultProject, ServiceType, "0f0f0f0f0f0f0f0f"), ErrAgentNotFound},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, svc.ReportRuntimeState(ctx, tt.caller, tt.agent, 1, RuntimeStateDisconnected, Fencing{Agent: tt.agent}), tt.err)
		})
	}
	stored, err = store.Get(ctx, held.Agent())
	require.NoError(t, err)
	assert.Equal(t, RuntimeStateConnected, stored.RuntimeState(), "a refused report was recorded")

	// The epoch the agent carries is its assignment's, and a
	// reassignment moves it: the agent is still the only caller this
	// route answers, and it is answered at the current epoch alone.
	reassigned, err := store.Assign(ctx, held.Agent(), operator)
	require.NoError(t, err)
	require.Equal(t, 2, reassigned.Epoch())
	assert.ErrorIs(t, svc.ReportRuntimeState(ctx, held.Agent(), held.Agent(), 1, RuntimeStateDisconnected, Fencing{Agent: held.Agent()}), ErrStaleEpoch)
	require.NoError(t, svc.ReportRuntimeState(ctx, held.Agent(), held.Agent(), 2, RuntimeStateDisconnected, Fencing{Agent: held.Agent()}),
		"the same call at the current epoch")
	stored, err = store.Get(ctx, held.Agent())
	require.NoError(t, err)
	assert.Equal(t, RuntimeStateDisconnected, stored.RuntimeState())
}

// define keeps a definition of an agent operator is to run, as the
// console wrote one before garam stopped accepting them (ADR-0090).
func define(t *testing.T, store *memoryStore, operator grn.GRN, values Values) *Definition {
	t.Helper()
	d, err := NewDefinition(orgGRNOf(operator), operator, "field agent", values,
		grn.New(grn.RootOwner, grn.DefaultProject, "user", "ada"))
	require.NoError(t, err)
	store.keepDefinition(d)
	return d
}

// TestService_DefinitionsAnswersThoseNamingTheCaller covers the read
// ADR-0033 decides: an operator polls exactly the definitions that name
// it, each with its claim state, and the definition's operator field is
// the whole of the scoping. The definitions the answer leaves out sit
// beside it, so what excludes them is the naming rather than a read
// that answers nothing.
func TestService_DefinitionsAnswersThoseNamingTheCaller(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	svc := mustService(t, store, &signer{})

	caller := operatorGRN("acme", "one")
	unclaimed := define(t, store, caller, Values{"model": "haiku"})
	claimed := define(t, store, caller, Values{})
	define(t, store, operatorGRN("acme", "two"), Values{"model": "opus"})
	define(t, store, operatorGRN("globex", "one"), Values{})

	// A claim moves one definition's state and no other's.
	_, err := svc.Claim(ctx, caller, claimed.Agent())
	require.NoError(t, err)

	states, err := svc.Definitions(ctx, caller)
	require.NoError(t, err)
	require.Len(t, states, 2, "the definitions naming the caller and no other's")
	byAgent := map[grn.GRN]*DefinitionState{}
	for _, state := range states {
		byAgent[state.Definition().Agent()] = state
	}
	require.Contains(t, byAgent, unclaimed.Agent())
	assert.Equal(t, Values{"model": "haiku"}, byAgent[unclaimed.Agent()].Definition().Values())
	assert.Nil(t, byAgent[unclaimed.Agent()].Claim(), "an unclaimed definition carries no claim")
	require.Contains(t, byAgent, claimed.Agent())
	claim := byAgent[claimed.Agent()].Claim()
	require.NotNil(t, claim, "a claimed definition carries the claim")
	assert.Equal(t, caller, claim.Assignee())
	assert.Equal(t, 1, claim.Epoch())

	_, err = svc.Definitions(ctx, grn.New("acme", grn.DefaultProject, ServiceType, "deadbeefdeadbeef"))
	assert.ErrorIs(t, err, ErrNotAnOperator, "an agent holds no definitions")
}

// TestService_ClaimAdmitsTheOperatorToTheCertificateRoute covers the
// transition ADR-0033 decides: before the claim the certificate route
// refuses the definition's agent GRN as unassigned — the existing
// behaviour, asserted as the control — and the claim is what admits the
// operator to it. A definition is claimed once, and a definition
// naming another operator answers as one that does not exist, as the
// read scopes it.
func TestService_ClaimAdmitsTheOperatorToTheCertificateRoute(t *testing.T) {
	ctx := context.Background()
	store := newMemoryStore()
	issuer := &signer{}
	svc := mustService(t, store, issuer)

	caller := operatorGRN("acme", "one")
	d := define(t, store, caller, Values{"model": "haiku"})

	// The control: before the claim the route refuses the GRN exactly
	// as it refuses an agent that was never defined.
	_, err := svc.IssueCertificate(ctx, caller, d.Agent())
	require.ErrorIs(t, err, ErrNotAssigned)

	assignment, err := svc.Claim(ctx, caller, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, d.Agent(), assignment.Agent())
	assert.Equal(t, caller, assignment.Assignee())
	assert.Equal(t, 1, assignment.Epoch())

	// The claim is where the agent comes to exist: nothing reported it,
	// and the row is there with nothing reported on it.
	a, err := store.Get(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, ProvisioningStatePending, a.ProvisioningState())
	assert.Equal(t, RuntimeStateUnknown, a.RuntimeState())

	cert, err := svc.IssueCertificate(ctx, caller, d.Agent())
	require.NoError(t, err, "the claim admitted the operator to the route")
	assert.Equal(t, []byte(d.Agent().String()), cert.CertificatePEM)

	refused := map[string]struct {
		operator grn.GRN
		agent    grn.GRN
		err      error
	}{
		"a second claim of the same agent":                    {caller, d.Agent(), ErrDefinitionClaimed},
		"a claim by an operator the definition does not name": {operatorGRN("acme", "two"), d.Agent(), ErrDefinitionNotFound},
		"a claim of a definition that does not exist":         {caller, grn.New("acme", grn.DefaultProject, ServiceType, "0f0f0f0f0f0f0f0f"), ErrDefinitionNotFound},
		"a caller that is not an operator":                    {d.Agent(), d.Agent(), ErrNotAnOperator},
	}
	for name, tt := range refused {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Claim(ctx, tt.operator, tt.agent)
			assert.ErrorIs(t, err, tt.err)
		})
	}

	assert.Equal(t, []grn.GRN{d.Agent()}, issuer.signed,
		"nothing was signed for a claim that was refused")
}

// mustService is the service over store and issuer with the default
// configuration and no token store.
func mustService(t *testing.T, store Store, issuer Issuer) Service {
	t.Helper()
	svc, err := NewService(store, nil, nil, issuer, Config{})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}
