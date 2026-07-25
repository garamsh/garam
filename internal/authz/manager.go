package authz

import (
	"context"
	"time"

	"github.com/garamsh/gaon/internal/grn"
)

// ChangeAction enumerates the kinds of permission-state changes that
// the Manager records.
type ChangeAction string

const (
	ActionGrant  ChangeAction = "grant"
	ActionRevoke ChangeAction = "revoke"
)

// Change is a single grant/revoke event recorded by [Manager]. It is
// the audit trail for every permission state transition.
type Change struct {
	ID        string
	Timestamp time.Time
	Action    ChangeAction
	Granter   grn.GRN
	Subject   grn.GRN
	Object    grn.GRN
	Role      string
	PolicyID  string
	Reason    string
}

// ChangeFilter narrows a ListChanges query.
type ChangeFilter struct {
	Subject grn.GRN
	Object  grn.GRN
	Since   time.Time
	Until   time.Time
	Limit   int
}

// DecisionTrace records one policy's contribution to a check.
type DecisionTrace struct {
	PolicyID string
	Decision Decision
	Matched  bool
	Reason   string
}

// Explanation is the structured result of [Manager.Explain].
type Explanation struct {
	Subject grn.GRN
	Object  grn.GRN
	Action  string
	Allowed bool
	Traces  []DecisionTrace
}

// Manager owns the lifecycle of grants, revocations, and audit.
type Manager interface {
	Grant(ctx context.Context, c Change) error
	Revoke(ctx context.Context, c Change) error
	ListChanges(ctx context.Context, f ChangeFilter) ([]Change, error)
	Explain(ctx context.Context, subject, object grn.GRN, action string) (Explanation, error)
}
