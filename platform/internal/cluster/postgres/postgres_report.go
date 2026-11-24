package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/cluster/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// Compile-time check that Store keeps reports.
var _ cluster.ReportStore = (*Store)(nil)

// PlacedDeclarationIDs answers which of ids a declaration of g holds.
func (s *Store) PlacedDeclarationIDs(ctx context.Context, g grn.GRN, ids []string) (map[string]bool, error) {
	placed := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return placed, nil
	}
	held, err := s.q.ListPlacedDeclarationIDs(ctx, db.ListPlacedDeclarationIDsParams{ClusterGrn: g.String(), Ids: ids})
	if err != nil {
		return nil, pgxerr.Classify(err, "list placed declaration ids", nil, nil)
	}
	for _, id := range held {
		placed[id] = true
	}
	return placed, nil
}

// KeepReport replaces g's kept report with r where r was observed
// later.
func (s *Store) KeepReport(ctx context.Context, g grn.GRN, r cluster.Report) error {
	report, err := json.Marshal(r)
	if err != nil {
		return errorx.Wrap(err, "encode report", errorx.KindInternal)
	}
	_, err = s.q.KeepClusterReport(ctx, db.KeepClusterReportParams{
		ClusterGrn: g.String(), ObservedAt: pgxtype.Timestamptz(r.ObservedAt), Report: report,
	})
	// A record deleted meanwhile breaks the foreign key.
	return pgxerr.Classify(err, "keep cluster report", cluster.ErrClusterNotFound, nil)
}

// LatestReport reads g's latest contact and kept report.
func (s *Store) LatestReport(ctx context.Context, g grn.GRN) (cluster.KeptReport, error) {
	row, err := s.q.GetClusterReport(ctx, g.String())
	if errors.Is(err, pgx.ErrNoRows) {
		return cluster.KeptReport{}, cluster.ErrClusterNotFound
	}
	if err != nil {
		return cluster.KeptReport{}, pgxerr.Classify(err, "get cluster report", nil, nil)
	}
	kept := cluster.KeptReport{Cluster: g}
	if row.LastContactAt.Valid {
		at := row.LastContactAt.Time.UTC()
		kept.LastContactAt = &at
	}
	if row.Report != nil {
		var r cluster.Report
		if err := json.Unmarshal(row.Report, &r); err != nil {
			return cluster.KeptReport{}, errorx.Wrap(err, "decode cluster report", errorx.KindDataLoss)
		}
		kept.Report = &r
	}
	return kept, nil
}
