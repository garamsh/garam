package postgres

import (
	"context"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/org/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	policyfeedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
)

// deleteRecording runs a delete that cascades in a transaction of its
// own, and records the marks that transaction raised before it commits
// for a request that asked (policyfeed.WithRaised). The cascade marks
// each group, project, agent, operator and member it takes with the
// row, which no handler can name before it runs (ADR-0165 item 3). A
// caller that asked for no collection gets the same single DELETE, which
// was one transaction before and is one now; the extra round trips are
// begin and commit, and the record reads nothing.
func (s *Store) deleteRecording(
	ctx context.Context, op string, notFound error, del func(q *db.Queries) (int64, error),
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return pgxerr.Classify(err, op, notFound, nil)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	n, err := del(s.q.WithTx(tx))
	if err := classifyRows(n, err, op, notFound); err != nil {
		return err
	}
	if err := policyfeedpg.Record(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(pgxerr.Classify(err, op, notFound, nil), op)
	}
	return nil
}
