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

// PostgresPasswordCredentialStore implements PasswordCredentialStore
// against PostgreSQL. Only the Argon2id verifier is persisted; the
// password never reaches the database (ADR-0040).
type PostgresPasswordCredentialStore struct {
	q *db.Queries
}

// Compile-time check that PostgresPasswordCredentialStore satisfies
// PasswordCredentialStore.
var _ PasswordCredentialStore = (*PostgresPasswordCredentialStore)(nil)

// NewPostgresPasswordCredentialStore constructs a Postgres-backed
// PasswordCredentialStore.
func NewPostgresPasswordCredentialStore(pool *pgxpool.Pool) *PostgresPasswordCredentialStore {
	return &PostgresPasswordCredentialStore{q: db.New(pool)}
}

// Create records the first verifier for a user.
func (s *PostgresPasswordCredentialStore) Create(ctx context.Context, userGRN grn.GRN, v Verifier) error {
	now := stamp(time.Now().UTC())
	err := s.q.CreatePasswordCredential(ctx, db.CreatePasswordCredentialParams{
		UserGrn:   userGRN.String(),
		Verifier:  string(v),
		CreatedAt: now,
		UpdatedAt: now,
	})
	return errorx.Wrap(err, "password credential store: create")
}

// Get returns the user's verifier, or ErrPasswordCredentialNotFound.
func (s *PostgresPasswordCredentialStore) Get(ctx context.Context, userGRN grn.GRN) (Verifier, error) {
	v, err := s.q.GetPasswordCredential(ctx, userGRN.String())
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrPasswordCredentialNotFound
	}
	if err != nil {
		return "", errorx.Wrap(err, "password credential store: get")
	}
	return Verifier(v), nil
}

// Replace writes a verifier over the one held, which is how a sign-in
// under parameters other than the current policy re-hashes.
func (s *PostgresPasswordCredentialStore) Replace(ctx context.Context, userGRN grn.GRN, v Verifier) error {
	err := s.q.ReplacePasswordVerifier(ctx, db.ReplacePasswordVerifierParams{
		UserGrn:   userGRN.String(),
		Verifier:  string(v),
		UpdatedAt: stamp(time.Now().UTC()),
	})
	return errorx.Wrap(err, "password credential store: replace")
}

// PostgresPasswordAttemptStore implements PasswordAttemptStore against
// PostgreSQL.
type PostgresPasswordAttemptStore struct {
	q *db.Queries
}

// Compile-time check that PostgresPasswordAttemptStore satisfies
// PasswordAttemptStore.
var _ PasswordAttemptStore = (*PostgresPasswordAttemptStore)(nil)

// NewPostgresPasswordAttemptStore constructs a Postgres-backed
// PasswordAttemptStore.
func NewPostgresPasswordAttemptStore(pool *pgxpool.Pool) *PostgresPasswordAttemptStore {
	return &PostgresPasswordAttemptStore{q: db.New(pool)}
}

// Record marks an attempt live for ttl, or returns
// ErrPasswordAttemptRateLimited when MaxLivePasswordAttempts are
// already live. The statement decides: it inserts nothing past the
// bound, so the rows it reports are the whole answer.
func (s *PostgresPasswordAttemptStore) Record(ctx context.Context, attempt string, ttl time.Duration) error {
	recorded, err := s.q.RecordPasswordAttempt(ctx, db.RecordPasswordAttemptParams{
		AttemptID: attempt,
		ExpiresAt: stamp(time.Now().UTC().Add(ttl)),
		MaxLive:   MaxLivePasswordAttempts,
	})
	if err != nil {
		return errorx.Wrap(err, "password attempt store: record")
	}
	if recorded == 0 {
		return ErrPasswordAttemptRateLimited
	}
	return nil
}

// Release frees a recorded attempt's slot. Releasing an unknown attempt
// is not an error: the slot is gone either way.
func (s *PostgresPasswordAttemptStore) Release(ctx context.Context, attempt string) error {
	return errorx.Wrap(s.q.ReleasePasswordAttempt(ctx, attempt), "password attempt store: release")
}
