// Internal test package: the service reads assignments through its
// store, and an Assignment is constructed by this package alone — a
// double declared outside it has none to answer with.
package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// memoryStore is a Store in memory. It keeps the assignment rules the
// service depends on — one operator per agent, an epoch that rises with
// every assignment — and reports states nowhere: the operator surface
// writes none.
type memoryStore struct {
	agents      map[grn.GRN]*Agent
	assignments map[grn.GRN]*Assignment
}

func newMemoryStore() *memoryStore {
	return &memoryStore{agents: map[grn.GRN]*Agent{}, assignments: map[grn.GRN]*Assignment{}}
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

func (s *memoryStore) Assign(_ context.Context, agentGRN, operatorGRN grn.GRN) (*Assignment, error) {
	if _, held := s.agents[agentGRN]; !held {
		return nil, ErrAgentNotFound
	}
	epoch := 1
	if prior, held := s.assignments[agentGRN]; held {
		epoch = prior.epoch + 1
	}
	assignment := &Assignment{agentGRN: agentGRN, operatorGRN: operatorGRN, epoch: epoch}
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

func (s *memoryStore) ReportProvisioningState(context.Context, grn.GRN, int, ProvisioningState) (*Agent, error) {
	return nil, errorx.New(errorx.KindUnimplemented, "no state is reported here")
}

func (s *memoryStore) ReportRuntimeState(context.Context, grn.GRN, int, RuntimeState) (*Agent, error) {
	return nil, errorx.New(errorx.KindUnimplemented, "no state is reported here")
}

// signer is an Issuer recording every subject it signed for, so a
// refusal is read as a certificate that was not issued rather than as
// an error beside one that was.
type signer struct{ signed []grn.GRN }

func (s *signer) Issue(_ context.Context, subject grn.GRN) (*Certificate, error) {
	s.signed = append(s.signed, subject)
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
	svc := NewService(store, &signer{})
	operator := operatorGRN("acme", "one")

	assignment, err := svc.Create(ctx, operator)
	require.NoError(t, err)
	assert.Equal(t, operator, assignment.Operator())
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
	svc := NewService(store, issuer)

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
		"a caller that is not an operator":          {held.Agent(), held.Agent(), ErrNotAnOperator},
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
