package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
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
	// ErrActivationEnded, ErrGenerationUsed or ErrCredentialFenced.
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
	// RecordRenewal admits presented as the agent's own write, then
	// records successor in the lineage presented belongs to. The legacy
	// cohort records nothing.
	RecordRenewal(ctx context.Context, agentGRN grn.GRN, token *TokenHash, presented, successor Credential) error
	// GetExecution answers the agent's execution as it stands.
	GetExecution(ctx context.Context, agentGRN grn.GRN) (*Execution, error)
	// CredentialCurrent answers whether c may act for the agent now,
	// without a token and without taking the execution lock.
	CredentialCurrent(ctx context.Context, agentGRN grn.GRN, c Credential) (bool, error)
}
