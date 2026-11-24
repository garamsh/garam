package cluster

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// ReportStore keeps what a record's connector tells garam (ADR-0188):
// its latest report, and the time of its latest authenticated request,
// which CredentialStore.ResolveToken records. ErrClusterNotFound where
// the organization holds no record under g.
type ReportStore interface {
	// PlacedDeclarationIDs answers which of ids a declaration of g
	// holds, live or tombstoned (REP-8).
	PlacedDeclarationIDs(ctx context.Context, g grn.GRN, ids []string) (map[string]bool, error)
	// KeepReport replaces g's kept report with r where r was observed
	// after it, and otherwise keeps the one there is (REP-5).
	KeepReport(ctx context.Context, g grn.GRN, r Report) error
	// LatestReport answers what garam holds of g's reports.
	LatestReport(ctx context.Context, g grn.GRN) (KeptReport, error)
}
