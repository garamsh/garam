package agent

import (
	"context"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// TokenEnrollmentContractVersion names the contract the managed
// creation route serves, carried in the Garam-Contract-Version header.
// Creation opens the agent's lineage, and its assignee then mints the
// agent's tokens in it (ADR-0100, ADR-0118).
const TokenEnrollmentContractVersion = "managed-enrollment.v2"

// Managed enrollment sentinels (ADR-0077, ADR-0078).
var (
	// ErrUnsupportedTokenEnrollmentContract refuses a request carrying
	// no contract version, or another.
	ErrUnsupportedTokenEnrollmentContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: a request presenting a token is served under "+TokenEnrollmentContractVersion)
	// ErrCreationConflict refuses a creation whose reference binds
	// another controller or request identifier, a request identifier
	// an earlier creation holds under another reference, and the retry
	// of a creation whose assignment has moved since.
	ErrCreationConflict = errorx.New(errorx.KindAlreadyExists, "managed creation conflicts with an earlier one")
)

// ManagedCreationInput is a managed creation as received: the
// controller the new agent is assigned to, the request identifier and
// the member's agent:create reference both are bound by, and the name
// the agent is given, nil for none (ADR-0119).
type ManagedCreationInput struct {
	Controller   grn.GRN
	RequestID    RequestID
	OperationRef string
	Name         *Name
}

// ManagedCreation is an agent the hosted control service created: its
// identity, its first assignment and the reference that authorized
// it. Lineage is the credential lineage it was born into, in which its
// controller mints its tokens.
type ManagedCreation struct {
	Agent        grn.GRN
	Audience     grn.GRN
	Controller   grn.GRN
	RequestID    RequestID
	OperationRef string
	Epoch        int
	Lineage      Lineage
	// Name is the name the creation asked for, nil for none; a rename
	// since changes the agent and not this.
	Name *Name
}

// CreateManaged registers an agent and its first assignment under the
// caller's agent:create reference, rechecked as current before any
// earlier creation is answered. Its execution is fenced from birth in
// a lineage of its own.
func (s *executionService) CreateManaged(ctx context.Context, caller grn.GRN, in ManagedCreationInput) (*ManagedCreation, bool, error) {
	b, err := authorizeCreation(ctx, s.operations, caller, in.Controller, in.RequestID, in.OperationRef, ErrCreationConflict)
	if err != nil {
		return nil, false, err
	}
	creation, err := s.executions.GetManagedCreationByReference(ctx, in.OperationRef)
	if err != nil {
		return nil, false, err
	}
	created := false
	if creation == nil {
		agentGRN, err := NewGRN(b.Org)
		if err != nil {
			return nil, false, err
		}
		creation, created, err = s.executions.CreateManaged(ctx, ManagedCreation{
			Agent: agentGRN, Audience: caller, Controller: in.Controller, RequestID: in.RequestID,
			OperationRef: in.OperationRef, Lineage: NewLineage(), Name: in.Name,
		})
		if err != nil {
			return nil, false, err
		}
	}
	if !created {
		// A retry asks for what the creation asked for, its name
		// included.
		if !sameName(creation.Name, in.Name) {
			return nil, false, ErrCreationConflict
		}
		// An earlier answer is the same only while the agent is still
		// where that creation put it.
		assignment, err := s.store.GetAssignment(ctx, creation.Agent)
		if err != nil {
			return nil, false, err
		}
		if !creation.placedOn(assignment) {
			return nil, false, ErrCreationConflict
		}
	}
	return creation, created, nil
}

// creationOf answers the managed creation of agentGRN when b, read
// back for caller, is that creation's own agent:create reference. A
// reference of another agent's creation, even on the same controller,
// covers nothing here.
func (s *executionService) creationOf(ctx context.Context, b *OperationBinding, caller, agentGRN grn.GRN) (*ManagedCreation, error) {
	if b.Kind != OperationKindCreate || b.Audience != caller || b.Org != grn.Org(agentGRN.Organization) {
		return nil, ErrReferenceMismatch
	}
	creation, err := s.executions.GetManagedCreation(ctx, agentGRN)
	if err != nil {
		return nil, err
	}
	if creation == nil || creation.OperationRef != b.Ref {
		return nil, ErrReferenceMismatch
	}
	return creation, nil
}

// placedOn reports whether the agent is still where its creation put
// it: the same controller at the same epoch.
func (c *ManagedCreation) placedOn(assignment *Assignment) bool {
	return assignment.Assignee == c.Controller && assignment.Epoch == c.Epoch
}
