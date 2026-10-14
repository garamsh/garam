package authn

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/internal/authn/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
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
	return pgxerr.Classify(err, "create session", nil, nil)
}

// Get returns the live session for the raw token, or
// ErrSessionNotFound.
func (s *PostgresSessionStore) Get(ctx context.Context, token string) (*Session, error) {
	row, err := s.q.GetSession(ctx, HashToken(token))
	if err != nil {
		return nil, pgxerr.Classify(err, "get session", ErrSessionNotFound, nil)
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
	return pgxerr.Classify(s.q.DeleteSession(ctx, HashToken(token)), "delete session", nil, nil)
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
func (s *PostgresOAuthStateStore) Issue(ctx context.Context, state string, ttl time.Duration, returnPath string) error {
	issued, err := s.q.IssueOAuthState(ctx, db.IssueOAuthStateParams{
		StateHash:  HashToken(state),
		ExpiresAt:  stamp(time.Now().UTC().Add(ttl)),
		ReturnPath: pgtype.Text{String: returnPath, Valid: returnPath != ""},
		MaxLive:    MaxLiveOAuthStates,
	})
	if err != nil {
		return pgxerr.Classify(err, "issue oauth state", nil, nil)
	}
	if issued == 0 {
		return ErrOAuthStateRateLimited
	}
	return nil
}

// Consume spends a live state and answers its return path, or returns
// ErrOAuthStateNotLive. The delete is the whole decision: two
// callbacks racing on one state, one row deleted, so only one of them
// is told it spent it, and only that one reads the path.
func (s *PostgresOAuthStateStore) Consume(ctx context.Context, state string) (string, error) {
	returnPath, err := s.q.ConsumeOAuthState(ctx, HashToken(state))
	if err != nil {
		return "", pgxerr.Classify(err, "consume oauth state", ErrOAuthStateNotLive, nil)
	}
	return returnPath.String, nil
}

// stamp converts a time.Time to a valid pgtype.Timestamptz.
func stamp(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// PostgresPasswordCredentialStore implements PasswordCredentialStore
// against PostgreSQL. Only the Argon2id verifier is persisted; the
// password never reaches the database (ADR-0041).
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
	return pgxerr.Classify(err, "create password credential", nil, nil)
}

// Get returns the user's verifier, or ErrPasswordCredentialNotFound.
func (s *PostgresPasswordCredentialStore) Get(ctx context.Context, userGRN grn.GRN) (Verifier, error) {
	v, err := s.q.GetPasswordCredential(ctx, userGRN.String())
	if err != nil {
		return "", pgxerr.Classify(err, "get password credential", ErrPasswordCredentialNotFound, nil)
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
	return pgxerr.Classify(err, "replace password verifier", nil, nil)
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
		return pgxerr.Classify(err, "record password attempt", nil, nil)
	}
	if recorded == 0 {
		return ErrPasswordAttemptRateLimited
	}
	return nil
}

// Release frees a recorded attempt's slot. Releasing an unknown attempt
// is not an error: the slot is gone either way.
func (s *PostgresPasswordAttemptStore) Release(ctx context.Context, attempt string) error {
	return pgxerr.Classify(s.q.ReleasePasswordAttempt(ctx, attempt), "release password attempt", nil, nil)
}
