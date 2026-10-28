package authz

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// Grant is one statement an evaluation reads and where it came from.
// Policy is the persisted policy holding it, and the zero GRN for a
// statement the caller supplied (ADR-0007). AttachedTo names the
// subjects of the request the policy is attached to; it is empty for a
// policy that applies org-wide and for a caller's statement. Explain
// reports both; Check reads only the statement.
type Grant struct {
	Statement
	Policy     grn.GRN
	AttachedTo []grn.GRN
}

// PolicySource yields the statements an org's active policies grant a
// set of subjects. Check and Explain read the cached Statements;
// CheckCurrent reads CurrentStatements, which reaches storage on every
// call. Neither reads the subject's roles or groups: the caller
// supplies those.
type PolicySource interface {
	// Statements returns the org-wide statements plus those of the
	// policies attached to any of subjects (ADR-0008). Its answer may
	// predate a write whose invalidation has not arrived yet.
	Statements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]Grant, error)
	// CurrentStatements makes the same selection from storage, and
	// fails when storage cannot be read.
	CurrentStatements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]Grant, error)
}
