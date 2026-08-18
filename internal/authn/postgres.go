package authn

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/internal/authn/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
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

// Create persists a new session for the raw token, and deletes the
// sessions that have expired.
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

// PostgresOAuthStateStore implements OAuthStateStore against
// PostgreSQL. Only state hashes are persisted; the state the browser
// carries never touches the database.
type PostgresOAuthStateStore struct {
	q *db.Queries
}

// Compile-time check that PostgresOAuthStateStore satisfies
// OAuthStateStore.
var _ OAuthStateStore = (*PostgresOAuthStateStore)(nil)

// NewPostgresOAuthStateStore constructs a Postgres-backed
// OAuthStateStore.
func NewPostgresOAuthStateStore(pool *pgxpool.Pool) *PostgresOAuthStateStore {
	return &PostgresOAuthStateStore{q: db.New(pool)}
}

// Issue records state as live for ttl, or returns
// ErrOAuthStateRateLimited when MaxLiveOAuthStates are already live.
// The statement decides: it inserts nothing past the bound, so the
// rows it reports are the whole answer.
func (s *PostgresOAuthStateStore) Issue(ctx context.Context, state string, ttl time.Duration) error {
	issued, err := s.q.IssueOAuthState(ctx, db.IssueOAuthStateParams{
		StateHash: HashToken(state),
		ExpiresAt: stamp(time.Now().UTC().Add(ttl)),
		MaxLive:   MaxLiveOAuthStates,
	})
	if err != nil {
		return errorx.Wrap(err, "oauth state store: issue")
	}
	if issued == 0 {
		return ErrOAuthStateRateLimited
	}
	return nil
}

// Consume spends a live state, or returns ErrOAuthStateNotLive. The
// delete is the whole decision: two callbacks racing on one state, one
// row deleted, so only one of them is told it spent it.
func (s *PostgresOAuthStateStore) Consume(ctx context.Context, state string) error {
	spent, err := s.q.ConsumeOAuthState(ctx, HashToken(state))
	if err != nil {
		return errorx.Wrap(err, "oauth state store: consume")
	}
	if spent == 0 {
		return ErrOAuthStateNotLive
	}
	return nil
}

// stamp converts a time.Time to a valid pgtype.Timestamptz.
func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}
