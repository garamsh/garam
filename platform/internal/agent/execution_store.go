package agent

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// ExecutionStore persists which generation of an agent may act and
// under which credential lineage (ADR-0078, ADR-0080). Every method
// that changes the execution takes the agent's execution row FOR
// UPDATE in one short local transaction and calls nothing outside it,
// and rechecks there the assignee, the epoch and the expiries the
// authority established beforehand.
type ExecutionStore interface {
	// Activate applies req. A first request answers created; a retry
	// of the same request while its activation is current answers it
	// again with the token replaced by req.TokenHash and a higher
	// TokenVersion. It refuses with ErrStaleEpoch, ErrNotAssigned,
	// ErrAuthorityExpired, ErrActivationStale, ErrActivationConflict,
	// ErrActivationEnded, ErrGenerationUsed, ErrCredentialFenced or,
	// for an agent fenced from birth that no activation has succeeded
	// for, ErrActivationIntentRequired when req carries no reference.
	Activate(ctx context.Context, req ActivationRequest) (a *Activation, created bool, err error)
	// Deactivate ends the agent's activation id when it is the current
	// one and answers true; an activation already ended is left as it
	// is and answers false, so a replay never reaches its successor. It
	// refuses with ErrActivationNotFound, ErrNotAssigned or
	// ErrAuthorityExpired.
	Deactivate(ctx context.Context, agentGRN grn.GRN, id ActivationID, authority Lifecycle) (bool, error)
	// Recover moves the agent to req.Lineage, recording its first
	// certificate and ending the current activation. A retry of the
	// same request answers the recovery it recorded; another body or a
	// reference already used refuses with ErrRecoveryConflict.
	Recover(ctx context.Context, req RecoveryRequest) (*Recovery, error)
	// GetRecovery answers the recovery recorded under requestID, or
	// nil, so a retry is answered before anything is signed again.
	GetRecovery(ctx context.Context, agentGRN grn.GRN, requestID RequestID) (*Recovery, error)
	// GetExecution answers the agent's execution as it stands.
	GetExecution(ctx context.Context, agentGRN grn.GRN) (*Execution, error)
	// Introspect answers the agent's assignment, its execution and
	// whether c may act now, from one snapshot and without a lock.
	Introspect(ctx context.Context, agentGRN grn.GRN, c Credential) (*Snapshot, error)
	// CredentialCurrent answers whether c may act for the agent now,
	// without a token and without taking the execution lock.
	CredentialCurrent(ctx context.Context, agentGRN grn.GRN, c Credential) (bool, error)
	// CreateManaged admits c.Agent onto c.Controller, fenced in
	// c.Lineage, as IdentityAdmission describes, and writes c in the same
	// transaction, and answers c as stored with created true. Where an
	// earlier creation already holds c's reference it writes nothing
	// and answers that one with created false; where one holds c's
	// request identifier under another reference it refuses with
	// ErrCreationConflict.
	CreateManaged(ctx context.Context, c ManagedCreation) (*ManagedCreation, bool, error)
	// GetManagedCreation answers the creation of agentGRN, or nil for
	// an agent no managed creation made.
	GetManagedCreation(ctx context.Context, agentGRN grn.GRN) (*ManagedCreation, error)
	// GetManagedCreationByReference answers the creation ref
	// authorized, or nil.
	GetManagedCreationByReference(ctx context.Context, ref string) (*ManagedCreation, error)
	// GetReplacement answers what the agent-enrollment.v1 replacement
	// ref authorized admitted, or nil where it admitted nothing
	// (ADR-0107).
	GetReplacement(ctx context.Context, ref string) (*ManagedCreation, error)
}
