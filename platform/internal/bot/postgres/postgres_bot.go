package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/bot/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
)

// RecordBotToken stores a bot's token hash for g, under the key the
// actor's token was exchanged under, in one transaction holding g's
// row.
func (s *Store) RecordBotToken(ctx context.Context, g grn.GRN, id, actorHash, hash string, ttl time.Duration) (time.Time, error) {
	var expiresAt time.Time
	err := s.inTx(ctx, g, bot.ErrBotNotPlaced, func(q *db.Queries, _ time.Time) error {
		if _, err := q.PlacingDeclarationExists(ctx, db.PlacingDeclarationExistsParams{
			ID: id, InstanceGrn: g.String(), Kind: bot.BotKind,
		}); err != nil {
			return pgxerr.Classify(err, "read placing declaration", bot.ErrBotNotPlaced, nil)
		}
		if err := q.SweepBotTokens(ctx, g.String()); err != nil {
			return pgxerr.Classify(err, "sweep bot tokens", nil, nil)
		}
		at, err := q.InsertBotToken(ctx, db.InsertBotTokenParams{
			TokenHash: hash, DeclarationID: id, Scope: []string{}, ActorHash: actorHash, InstanceGrn: g.String(),
			Ttl: pgtype.Interval{Microseconds: ttl.Microseconds(), Valid: true},
		})
		if err != nil {
			return pgxerr.Classify(err, "insert bot token", app.ErrTokenInvalid, nil)
		}
		expiresAt = at.Time.UTC()
		return nil
	})
	return expiresAt, err
}

// RevokeInstallationBotTokens deletes the bot tokens of every instance
// of installation in one statement.
func (s *Store) RevokeInstallationBotTokens(ctx context.Context, installation grn.GRN) error {
	if err := s.q.DeleteInstallationBotTokens(ctx, installation.String()); err != nil {
		return pgxerr.Classify(err, "revoke installation bot tokens", nil, nil)
	}
	return nil
}
