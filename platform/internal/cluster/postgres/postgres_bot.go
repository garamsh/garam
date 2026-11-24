package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/cluster/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
)

// RecordBotToken stores a bot's token hash for g, in one transaction
// holding g's row and the declaration that places the bot.
func (s *Store) RecordBotToken(ctx context.Context, g grn.GRN, id, hash string, ttl time.Duration) (time.Time, error) {
	var expiresAt time.Time
	err := s.inTx(ctx, g, cluster.ErrBotNotPlaced, func(q *db.Queries, _ time.Time) error {
		if _, err := q.HoldPlacingDeclaration(ctx, db.HoldPlacingDeclarationParams{ID: id, ClusterGrn: g.String()}); err != nil {
			return pgxerr.Classify(err, "hold placing declaration", cluster.ErrBotNotPlaced, nil)
		}
		if err := q.SweepBotTokens(ctx, g.String()); err != nil {
			return pgxerr.Classify(err, "sweep bot tokens", nil, nil)
		}
		at, err := q.InsertBotToken(ctx, db.InsertBotTokenParams{
			TokenHash: hash, ClusterGrn: g.String(), DeclarationID: id, Scope: []string{},
			Ttl: pgtype.Interval{Microseconds: ttl.Microseconds(), Valid: true},
		})
		if err != nil {
			return pgxerr.Classify(err, "insert bot token", nil, nil)
		}
		expiresAt = at.Time.UTC()
		return nil
	})
	return expiresAt, err
}
