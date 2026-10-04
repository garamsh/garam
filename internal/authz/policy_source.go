package authz

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// PolicySource yields the statements an org's active policies grant a
// set of subjects. Check and Explain read through it, so the engine
// never parses a stored document on the hot path.
type PolicySource interface {
	// Statements returns the org-wide statements plus those of the
	// policies attached to any of subjects (ADR-0008). Its answer may
	// predate a write whose invalidation has not arrived yet.
	Statements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]Statement, error)
	// CurrentStatements makes the same selection from what storage
	// holds when it is called, and fails when storage cannot be read.
	CurrentStatements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]Statement, error)
}
