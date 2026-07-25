package authn

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/gaon/internal/authn/db"
	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
)

// PostgresSessionStore implements SessionStore against PostgreSQL.
// Only token hashes are persisted; the raw bearer token never touches
// the database.
type PostgresSessionStore struct {
	q *db.Queries
}

// Compile-time check that PostgresSessionStore satisfies SessionStore.
var _ SessionStore = (*PostgresSessionStore)(nil)

// NewPostgresSessionStore constructs a Postgres-backed SessionStore.
func NewPostgresSessionStore(pool *pgxpool.Pool) *PostgresSessionStore {
	return &PostgresSessionStore{q: db.New(pool)}
}

// Create persists a new session for the raw token.
func (s *PostgresSessionStore) Create(ctx context.Context, token string, sess *Session) error {
	err := s.q.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: HashToken(token),
		UserGrn:   sess.userGRN.String(),
		CreatedAt: stamp(sess.createdAt),
		ExpiresAt: stamp(sess.expiresAt),
	})
	return errorx.Wrap(err, "session store: create")
}

// Get returns the live session for the raw token, or
// ErrSessionNotFound.
func (s *PostgresSessionStore) Get(ctx context.Context, token string) (*Session, error) {
	row, err := s.q.GetSession(ctx, HashToken(token))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, errorx.Wrap(err, "session store: get")
	}
	g, err := grn.Parse(row.UserGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "session row user grn", errorx.KindInternal)
	}
	return &Session{userGRN: g, createdAt: row.CreatedAt.Time, expiresAt: row.ExpiresAt.Time}, nil
}

// Delete revokes the session for the raw token. Deleting an unknown
// token is not an error so logout is idempotent.
func (s *PostgresSessionStore) Delete(ctx context.Context, token string) error {
	return errorx.Wrap(s.q.DeleteSession(ctx, HashToken(token)), "session store: delete")
}

// stamp converts a time.Time to a valid pgtype.Timestamptz.
func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
