package agent

import (
	"context"
	"time"

	"github.com/garamsh/garam/platform/internal/grn"
)

// LifecycleOperation is what a lifecycle authority asks to do to an
// agent's execution (ADR-0078).
type LifecycleOperation string

const (
	// OperationActivate admits, replaces, deactivates or inspects an
	// agent's execution.
	OperationActivate LifecycleOperation = "agent:activate"
	// OperationRecover replaces an agent's credential lineage.
	OperationRecover LifecycleOperation = "agent:recover"
)

// Grant is the authority an authorized lifecycle act rests on, as the
// store rechecks it under the execution lock: a hosted operator's
// delegation, or a supervisor's registration, which holds until the
// certificate garam last issued that supervisor expires.
type Grant struct {
	ID        string
	ExpiresAt time.Time
}

// LifecycleAuthorizer answers whether caller may perform op on the
// agent, whose current assignee is assignee, from authority read fresh
// for this call. A hosted operator needs its registration, a live
// grant covering op for assignee, and persisted policy in which an
// explicit Deny wins. A supervisor needs its current registration for
// this agent, to be the assignee itself, op to be agent:activate, and
// the same policy (ADR-0082).
// A refusal is KindPermissionDenied; authority it could not read is
// KindUnavailable, and refuses as well.
type LifecycleAuthorizer interface {
	Authorize(ctx context.Context, caller, agentGRN, assignee grn.GRN, op LifecycleOperation) (Grant, error)
}
