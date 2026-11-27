package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/bot/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// Compile-time check that Store keeps reports.
var _ bot.ReportStore = (*Store)(nil)

// PlacedDeclarationIDs answers which of ids a declaration of g holds.
func (s *Store) PlacedDeclarationIDs(ctx context.Context, g grn.GRN, ids []string) (map[string]bool, error) {
	placed := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return placed, nil
	}
	held, err := s.q.ListPlacedDeclarationIDs(ctx, db.ListPlacedDeclarationIDsParams{InstanceGrn: g.String(), Ids: ids})
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
func (s *Store) KeepReport(ctx context.Context, g grn.GRN, r bot.Report) error {
	report, err := json.Marshal(r)
	if err != nil {
		return errorx.Wrap(err, "encode report", errorx.KindInternal)
	}
	_, err = s.q.KeepReport(ctx, db.KeepReportParams{
		InstanceGrn: g.String(), ObservedAt: pgxtype.Timestamptz(r.ObservedAt), Report: report,
	})
	// A record deleted meanwhile breaks the foreign key.
	return pgxerr.Classify(err, "keep report", app.ErrInstanceNotFound, nil)
}

// LatestReport reads g's kept report.
func (s *Store) LatestReport(ctx context.Context, g grn.GRN) (bot.KeptReport, error) {
	row, err := s.q.GetReport(ctx, g.String())
	if errors.Is(err, pgx.ErrNoRows) {
		return bot.KeptReport{}, app.ErrInstanceNotFound
	}
	if err != nil {
		return bot.KeptReport{}, pgxerr.Classify(err, "get report", nil, nil)
	}
	kept := bot.KeptReport{Instance: g}
	if row.Report != nil {
		var r bot.Report
		if err := json.Unmarshal(row.Report, &r); err != nil {
			return bot.KeptReport{}, errorx.Wrap(err, "decode report", errorx.KindDataLoss)
		}
		kept.Report = &r
	}
	return kept, nil
}
