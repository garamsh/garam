package authn

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/internal/authn/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/pgxtype"
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
		CreatedAt: pgxtype.Timestamptz(sess.createdAt),
		ExpiresAt: pgxtype.Timestamptz(sess.expiresAt),
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
	return rowToSession(row)
}

// rowToSession converts a stored session. A user GRN that no longer
// parses is the database's fault, hence internal.
func rowToSession(row db.Session) (*Session, error) {
	g, err := grn.Parse(row.UserGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "session row user grn", errorx.KindInternal)
	}
	return &Session{userGRN: g, createdAt: row.CreatedAt.Time, expiresAt: row.ExpiresAt.Time,
		id: uuid.UUID(row.ID.Bytes).String(), kind: SessionKind(row.Kind), name: row.Name.String,
		refreshExpiresAt: row.RefreshExpiresAt.Time}, nil
}

// Refresh spends a live CLI session's refresh token for next. The
// update is the whole decision: two refreshes racing on one token, one
// row updated, so only one of them is answered the session.
func (s *PostgresSessionStore) Refresh(ctx context.Context, refresh string, next CLITokens) (*Session, error) {
	row, err := s.q.RefreshCLISession(ctx, db.RefreshCLISessionParams{
		TokenHash:        HashToken(next.Access),
		ExpiresAt:        pgxtype.Timestamptz(next.AccessExpiresAt),
		NextRefreshHash:  pgtype.Text{String: HashToken(next.Refresh), Valid: true},
		RefreshExpiresAt: pgxtype.Timestamptz(next.RefreshExpiresAt),
		RefreshHash:      pgtype.Text{String: HashToken(refresh), Valid: true},
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "refresh cli session", ErrRefreshTokenNotLive, nil)
	}
	return rowToSession(row)
}

// List answers the user's sessions anything is still live in.
func (s *PostgresSessionStore) List(ctx context.Context, user grn.GRN) ([]*Session, error) {
	rows, err := s.q.ListUserSessions(ctx, user.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list sessions", nil, nil)
	}
	out := make([]*Session, 0, len(rows))
	for _, row := range rows {
		sess, err := rowToSession(row)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, nil
}

// Revoke deletes the user's session id. An id that does not parse is
// one the user holds no session by.
func (s *PostgresSessionStore) Revoke(ctx context.Context, user grn.GRN, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return ErrSessionNotFound
	}
	deleted, err := s.q.RevokeUserSession(ctx, db.RevokeUserSessionParams{
		UserGrn: user.String(), ID: pgtype.UUID{Bytes: parsed, Valid: true},
	})
	if err != nil {
		return pgxerr.Classify(err, "revoke session", nil, nil)
	}
	if deleted == 0 {
		return ErrSessionNotFound
	}
	return nil
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
		ExpiresAt:  pgxtype.Timestamptz(time.Now().UTC().Add(ttl)),
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
	now := pgxtype.Timestamptz(time.Now().UTC())
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
		UpdatedAt: pgxtype.Timestamptz(time.Now().UTC()),
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
		ExpiresAt: pgxtype.Timestamptz(time.Now().UTC().Add(ttl)),
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

// PostgresDeviceStore implements DeviceStore against PostgreSQL. Only
// the codes' hashes are persisted.
type PostgresDeviceStore struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// Compile-time check that PostgresDeviceStore satisfies DeviceStore.
var _ DeviceStore = (*PostgresDeviceStore)(nil)

// NewPostgresDeviceStore constructs a Postgres-backed DeviceStore.
func NewPostgresDeviceStore(pool *pgxpool.Pool) *PostgresDeviceStore {
	return &PostgresDeviceStore{pool: pool, q: db.New(pool)}
}

// Issue records grant, or returns ErrDeviceRateLimited past the bound.
// The statement decides, so the rows it reports are the whole answer.
func (s *PostgresDeviceStore) Issue(ctx context.Context, grant DeviceGrant, clientName string) error {
	code, ok := NormalizeUserCode(grant.UserCode)
	if !ok {
		return errorx.New(errorx.KindInternal, "a drawn user code does not normalize")
	}
	issued, err := s.q.IssueDeviceAuthorization(ctx, db.IssueDeviceAuthorizationParams{
		DeviceCodeHash:  HashToken(grant.DeviceCode),
		UserCodeHash:    HashToken(code),
		IntervalSeconds: int32(DevicePollInterval / time.Second),
		ClientName:      pgtype.Text{String: clientName, Valid: clientName != ""},
		TtlSeconds:      DeviceCodeTTL.Seconds(),
		MaxLive:         MaxLiveDeviceAuthorizations,
	})
	if err != nil {
		return pgxerr.Classify(err, "issue device authorization", nil, nil)
	}
	if issued == 0 {
		return ErrDeviceRateLimited
	}
	return nil
}

// Decide records user's decision on the live, undecided authorization
// userCode names.
func (s *PostgresDeviceStore) Decide(ctx context.Context, userCode string, user grn.GRN, approve bool) error {
	code, ok := NormalizeUserCode(userCode)
	if !ok {
		return ErrDeviceCodeNotPending
	}
	decision := "denied"
	if approve {
		decision = "approved"
	}
	decided, err := s.q.DecideDeviceAuthorization(ctx, db.DecideDeviceAuthorizationParams{
		UserCodeHash: HashToken(code),
		Decision:     pgtype.Text{String: decision, Valid: true},
		UserGrn:      pgtype.Text{String: user.String(), Valid: true},
	})
	if err != nil {
		return pgxerr.Classify(err, "decide device authorization", nil, nil)
	}
	if decided == 0 {
		return ErrDeviceCodeNotPending
	}
	return nil
}

// Poll reads the authorization deviceCode names under its row lock, so
// two polls of one code are answered one after the other: only the
// first to find it approved opens a session, and the second finds it
// gone.
func (s *PostgresDeviceStore) Poll(ctx context.Context, deviceCode string, tokens CLITokens) (PollResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PollResult{}, pgxerr.Classify(err, "poll device authorization", nil, nil)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	hash := HashToken(deviceCode)
	row, err := q.LockDeviceAuthorization(ctx, hash)
	if err != nil {
		return PollResult{}, pgxerr.Classify(err, "poll device authorization", ErrDeviceCodeUnknown, nil)
	}
	interval := time.Duration(row.IntervalSeconds) * time.Second
	result := PollResult{Interval: interval}
	switch {
	case row.Expired:
		result.Outcome = PollExpired
	case row.Decision.String == "denied":
		result.Outcome = PollDenied
	case row.Decision.String == "approved":
		sess, err := s.redeem(ctx, q, row, tokens)
		if err != nil {
			return PollResult{}, err
		}
		result.Outcome, result.Session = PollIssued, sess
	case row.TooEarly.Bool:
		result.Outcome, result.Interval = PollSlowDown, interval+DeviceSlowDownStep
	default:
		result.Outcome = PollPending
	}
	if result.Outcome == PollPending || result.Outcome == PollSlowDown {
		if err := q.TouchDeviceAuthorization(ctx, db.TouchDeviceAuthorizationParams{
			DeviceCodeHash: hash, IntervalSeconds: int32(result.Interval / time.Second),
		}); err != nil {
			return PollResult{}, pgxerr.Classify(err, "poll device authorization", nil, nil)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return PollResult{}, pgxerr.Classify(err, "poll device authorization", nil, nil)
	}
	return result, nil
}

// redeem opens the CLI session an approved authorization grants and
// spends the authorization, inside the poll's transaction.
func (s *PostgresDeviceStore) redeem(ctx context.Context, q *db.Queries, row db.LockDeviceAuthorizationRow, tokens CLITokens) (*Session, error) {
	user, err := grn.Parse(row.UserGrn.String)
	if err != nil {
		return nil, errorx.Wrap(err, "device authorization user grn", errorx.KindInternal)
	}
	now := time.Now().UTC()
	if err := q.CreateCLISession(ctx, db.CreateCLISessionParams{
		TokenHash:        HashToken(tokens.Access),
		UserGrn:          user.String(),
		CreatedAt:        pgxtype.Timestamptz(now),
		ExpiresAt:        pgxtype.Timestamptz(tokens.AccessExpiresAt),
		RefreshHash:      pgtype.Text{String: HashToken(tokens.Refresh), Valid: true},
		RefreshExpiresAt: pgxtype.Timestamptz(tokens.RefreshExpiresAt),
		Name:             row.ClientName,
	}); err != nil {
		return nil, pgxerr.Classify(err, "open cli session", nil, nil)
	}
	if err := q.DeleteDeviceAuthorization(ctx, row.DeviceCodeHash); err != nil {
		return nil, pgxerr.Classify(err, "spend device authorization", nil, nil)
	}
	return &Session{userGRN: user, createdAt: now, expiresAt: tokens.AccessExpiresAt, kind: SessionCLI,
		name: row.ClientName.String, refreshExpiresAt: tokens.RefreshExpiresAt}, nil
}
