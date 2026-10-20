// Package postgres implements the sign-in domain's stores against
// PostgreSQL via jackc/pgx (ADR-0069): sessions, login states,
// password credentials and attempts, and device authorizations. The
// package holds more than one store, so each is named for what it
// stores.
package postgres

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authn/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/pgxtype"
)

// SessionStore implements SessionStore against PostgreSQL.
// Only token hashes are persisted; the raw bearer token never touches
// the database.
type SessionStore struct {
	q *db.Queries
}

// Compile-time check that SessionStore satisfies SessionStore.
var _ authn.SessionStore = (*SessionStore)(nil)

// NewSessionStore constructs a Postgres-backed SessionStore.
func NewSessionStore(pool *pgxpool.Pool) *SessionStore {
	return &SessionStore{q: db.New(pool)}
}

// Create persists a new session for the raw token, and deletes the
// sessions that have expired.
func (s *SessionStore) Create(ctx context.Context, token string, sess *authn.Session) error {
	err := s.q.CreateSession(ctx, db.CreateSessionParams{
		TokenHash: authn.HashToken(token),
		UserGrn:   sess.User.String(),
		CreatedAt: pgxtype.Timestamptz(sess.CreatedAt),
		ExpiresAt: pgxtype.Timestamptz(sess.ExpiresAt),
	})
	return pgxerr.Classify(err, "create session", nil, nil)
}

// Get returns the live session for the raw token, or
// ErrSessionNotFound.
func (s *SessionStore) Get(ctx context.Context, token string) (*authn.Session, error) {
	row, err := s.q.GetSession(ctx, authn.HashToken(token))
	if err != nil {
		return nil, pgxerr.Classify(err, "get session", authn.ErrSessionNotFound, nil)
	}
	return rowToSession(row)
}

// rowToSession converts a stored session. A user GRN that no longer
// parses is the database's fault, hence internal.
func rowToSession(row db.Session) (*authn.Session, error) {
	g, err := grn.Parse(row.UserGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "session row user grn", errorx.KindInternal)
	}
	return &authn.Session{User: g, CreatedAt: row.CreatedAt.Time, ExpiresAt: row.ExpiresAt.Time,
		ID: uuid.UUID(row.ID.Bytes).String(), Kind: authn.SessionKind(row.Kind), Name: row.Name.String,
		RefreshExpiresAt: row.RefreshExpiresAt.Time}, nil
}

// Refresh spends a live CLI session's refresh token for next. The
// update is the whole decision: two refreshes racing on one token, one
// row updated, so only one of them is answered the session.
func (s *SessionStore) Refresh(ctx context.Context, refresh string, next authn.CLITokens) (*authn.Session, error) {
	row, err := s.q.RefreshCLISession(ctx, db.RefreshCLISessionParams{
		TokenHash:        authn.HashToken(next.Access),
		ExpiresAt:        pgxtype.Timestamptz(next.AccessExpiresAt),
		NextRefreshHash:  pgtype.Text{String: authn.HashToken(next.Refresh), Valid: true},
		RefreshExpiresAt: pgxtype.Timestamptz(next.RefreshExpiresAt),
		RefreshHash:      pgtype.Text{String: authn.HashToken(refresh), Valid: true},
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "refresh cli session", authn.ErrRefreshTokenNotLive, nil)
	}
	return rowToSession(row)
}

// List answers the user's sessions anything is still live in.
func (s *SessionStore) List(ctx context.Context, user grn.GRN) ([]*authn.Session, error) {
	rows, err := s.q.ListUserSessions(ctx, user.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list sessions", nil, nil)
	}
	out := make([]*authn.Session, 0, len(rows))
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
func (s *SessionStore) Revoke(ctx context.Context, user grn.GRN, id string) error {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return authn.ErrSessionNotFound
	}
	deleted, err := s.q.RevokeUserSession(ctx, db.RevokeUserSessionParams{
		UserGrn: user.String(), ID: pgtype.UUID{Bytes: parsed, Valid: true},
	})
	if err != nil {
		return pgxerr.Classify(err, "revoke session", nil, nil)
	}
	if deleted == 0 {
		return authn.ErrSessionNotFound
	}
	return nil
}

// Delete revokes the session for the raw token. Deleting an unknown
// token is not an error so logout is idempotent.
func (s *SessionStore) Delete(ctx context.Context, token string) error {
	return pgxerr.Classify(s.q.DeleteSession(ctx, authn.HashToken(token)), "delete session", nil, nil)
}

// OAuthStateStore implements OAuthStateStore against
// PostgreSQL. Only state hashes are persisted; the state the browser
// carries never touches the database.
type OAuthStateStore struct {
	q *db.Queries
}

// Compile-time check that OAuthStateStore satisfies
// OAuthStateStore.
var _ authn.OAuthStateStore = (*OAuthStateStore)(nil)

// NewOAuthStateStore constructs a Postgres-backed
// OAuthStateStore.
func NewOAuthStateStore(pool *pgxpool.Pool) *OAuthStateStore {
	return &OAuthStateStore{q: db.New(pool)}
}

// Issue records state as live for ttl, or returns
// ErrOAuthStateRateLimited when MaxLiveOAuthStates are already live.
// The statement decides: it inserts nothing past the bound, so the
// rows it reports are the whole answer.
func (s *OAuthStateStore) Issue(ctx context.Context, state string, ttl time.Duration, returnPath string) error {
	issued, err := s.q.IssueOAuthState(ctx, db.IssueOAuthStateParams{
		StateHash:  authn.HashToken(state),
		ExpiresAt:  pgxtype.Timestamptz(time.Now().UTC().Add(ttl)),
		ReturnPath: pgtype.Text{String: returnPath, Valid: returnPath != ""},
		MaxLive:    authn.MaxLiveOAuthStates,
	})
	if err != nil {
		return pgxerr.Classify(err, "issue oauth state", nil, nil)
	}
	if issued == 0 {
		return authn.ErrOAuthStateRateLimited
	}
	return nil
}

// Consume spends a live state and answers its return path, or returns
// ErrOAuthStateNotLive. The delete is the whole decision: two
// callbacks racing on one state, one row deleted, so only one of them
// is told it spent it, and only that one reads the path.
func (s *OAuthStateStore) Consume(ctx context.Context, state string) (string, error) {
	returnPath, err := s.q.ConsumeOAuthState(ctx, authn.HashToken(state))
	if err != nil {
		return "", pgxerr.Classify(err, "consume oauth state", authn.ErrOAuthStateNotLive, nil)
	}
	return returnPath.String, nil
}

// PasswordCredentialStore implements PasswordCredentialStore
// against PostgreSQL. Only the Argon2id verifier is persisted; the
// password never reaches the database (ADR-0041).
type PasswordCredentialStore struct {
	q *db.Queries
}

// Compile-time check that PasswordCredentialStore satisfies
// PasswordCredentialStore.
var _ authn.PasswordCredentialStore = (*PasswordCredentialStore)(nil)

// NewPasswordCredentialStore constructs a Postgres-backed
// PasswordCredentialStore.
func NewPasswordCredentialStore(pool *pgxpool.Pool) *PasswordCredentialStore {
	return &PasswordCredentialStore{q: db.New(pool)}
}

// Create records the first verifier for a user.
func (s *PasswordCredentialStore) Create(ctx context.Context, userGRN grn.GRN, v authn.Verifier) error {
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
func (s *PasswordCredentialStore) Get(ctx context.Context, userGRN grn.GRN) (authn.Verifier, error) {
	v, err := s.q.GetPasswordCredential(ctx, userGRN.String())
	if err != nil {
		return "", pgxerr.Classify(err, "get password credential", authn.ErrPasswordCredentialNotFound, nil)
	}
	return authn.Verifier(v), nil
}

// Replace writes a verifier over the one held, which is how a sign-in
// under parameters other than the current policy re-hashes.
func (s *PasswordCredentialStore) Replace(ctx context.Context, userGRN grn.GRN, v authn.Verifier) error {
	err := s.q.ReplacePasswordVerifier(ctx, db.ReplacePasswordVerifierParams{
		UserGrn:   userGRN.String(),
		Verifier:  string(v),
		UpdatedAt: pgxtype.Timestamptz(time.Now().UTC()),
	})
	return pgxerr.Classify(err, "replace password verifier", nil, nil)
}

// PasswordAttemptStore implements PasswordAttemptStore against
// PostgreSQL.
type PasswordAttemptStore struct {
	q *db.Queries
}

// Compile-time check that PasswordAttemptStore satisfies
// PasswordAttemptStore.
var _ authn.PasswordAttemptStore = (*PasswordAttemptStore)(nil)

// NewPasswordAttemptStore constructs a Postgres-backed
// PasswordAttemptStore.
func NewPasswordAttemptStore(pool *pgxpool.Pool) *PasswordAttemptStore {
	return &PasswordAttemptStore{q: db.New(pool)}
}

// Record marks an attempt live for ttl, or returns
// ErrPasswordAttemptRateLimited when MaxLivePasswordAttempts are
// already live. The statement decides: it inserts nothing past the
// bound, so the rows it reports are the whole answer.
func (s *PasswordAttemptStore) Record(ctx context.Context, attempt string, ttl time.Duration) error {
	recorded, err := s.q.RecordPasswordAttempt(ctx, db.RecordPasswordAttemptParams{
		AttemptID: attempt,
		ExpiresAt: pgxtype.Timestamptz(time.Now().UTC().Add(ttl)),
		MaxLive:   authn.MaxLivePasswordAttempts,
	})
	if err != nil {
		return pgxerr.Classify(err, "record password attempt", nil, nil)
	}
	if recorded == 0 {
		return authn.ErrPasswordAttemptRateLimited
	}
	return nil
}

// Release frees a recorded attempt's slot. Releasing an unknown attempt
// is not an error: the slot is gone either way.
func (s *PasswordAttemptStore) Release(ctx context.Context, attempt string) error {
	return pgxerr.Classify(s.q.ReleasePasswordAttempt(ctx, attempt), "release password attempt", nil, nil)
}

// DeviceStore implements DeviceStore against PostgreSQL. Only
// the codes' hashes are persisted.
type DeviceStore struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// Compile-time check that DeviceStore satisfies DeviceStore.
var _ authn.DeviceStore = (*DeviceStore)(nil)

// NewDeviceStore constructs a Postgres-backed DeviceStore.
func NewDeviceStore(pool *pgxpool.Pool) *DeviceStore {
	return &DeviceStore{pool: pool, q: db.New(pool)}
}

// Issue records grant, or returns ErrDeviceRateLimited past the bound.
// The statement decides, so the rows it reports are the whole answer.
func (s *DeviceStore) Issue(ctx context.Context, grant authn.DeviceGrant, clientName string) error {
	code, ok := authn.NormalizeUserCode(grant.UserCode)
	if !ok {
		return errorx.New(errorx.KindInternal, "a drawn user code does not normalize")
	}
	issued, err := s.q.IssueDeviceAuthorization(ctx, db.IssueDeviceAuthorizationParams{
		DeviceCodeHash:  authn.HashToken(grant.DeviceCode),
		UserCodeHash:    authn.HashToken(code),
		IntervalSeconds: int32(authn.DevicePollInterval / time.Second),
		ClientName:      pgtype.Text{String: clientName, Valid: clientName != ""},
		TtlSeconds:      authn.DeviceCodeTTL.Seconds(),
		MaxLive:         authn.MaxLiveDeviceAuthorizations,
	})
	if err != nil {
		return pgxerr.Classify(err, "issue device authorization", nil, nil)
	}
	if issued == 0 {
		return authn.ErrDeviceRateLimited
	}
	return nil
}

// Decide records user's decision on the live, undecided authorization
// userCode names.
func (s *DeviceStore) Decide(ctx context.Context, userCode string, user grn.GRN, approve bool) error {
	code, ok := authn.NormalizeUserCode(userCode)
	if !ok {
		return authn.ErrDeviceCodeNotPending
	}
	decision := "denied"
	if approve {
		decision = "approved"
	}
	decided, err := s.q.DecideDeviceAuthorization(ctx, db.DecideDeviceAuthorizationParams{
		UserCodeHash: authn.HashToken(code),
		Decision:     pgtype.Text{String: decision, Valid: true},
		UserGrn:      pgtype.Text{String: user.String(), Valid: true},
	})
	if err != nil {
		return pgxerr.Classify(err, "decide device authorization", nil, nil)
	}
	if decided == 0 {
		return authn.ErrDeviceCodeNotPending
	}
	return nil
}

// Poll reads the authorization deviceCode names under its row lock, so
// two polls of one code are answered one after the other: only the
// first to find it approved opens a session, and the second finds it
// gone.
func (s *DeviceStore) Poll(ctx context.Context, deviceCode string, tokens authn.CLITokens) (authn.PollResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return authn.PollResult{}, pgxerr.Classify(err, "poll device authorization", nil, nil)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	hash := authn.HashToken(deviceCode)
	row, err := q.LockDeviceAuthorization(ctx, hash)
	if err != nil {
		return authn.PollResult{}, pgxerr.Classify(err, "poll device authorization", authn.ErrDeviceCodeUnknown, nil)
	}
	interval := time.Duration(row.IntervalSeconds) * time.Second
	result := authn.PollResult{Interval: interval}
	switch {
	case row.Expired:
		result.Outcome = authn.PollExpired
	case row.Decision.String == "denied":
		result.Outcome = authn.PollDenied
	case row.Decision.String == "approved":
		sess, err := s.redeem(ctx, q, row, tokens)
		if err != nil {
			return authn.PollResult{}, err
		}
		result.Outcome, result.Session = authn.PollIssued, sess
	case row.TooEarly.Bool:
		result.Outcome, result.Interval = authn.PollSlowDown, interval+authn.DeviceSlowDownStep
	default:
		result.Outcome = authn.PollPending
	}
	if result.Outcome == authn.PollPending || result.Outcome == authn.PollSlowDown {
		if err := q.TouchDeviceAuthorization(ctx, db.TouchDeviceAuthorizationParams{
			DeviceCodeHash: hash, IntervalSeconds: int32(result.Interval / time.Second),
		}); err != nil {
			return authn.PollResult{}, pgxerr.Classify(err, "poll device authorization", nil, nil)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return authn.PollResult{}, pgxerr.Classify(err, "poll device authorization", nil, nil)
	}
	return result, nil
}

// redeem opens the CLI session an approved authorization grants and
// spends the authorization, inside the poll's transaction.
func (s *DeviceStore) redeem(ctx context.Context, q *db.Queries, row db.LockDeviceAuthorizationRow, tokens authn.CLITokens) (*authn.Session, error) {
	user, err := grn.Parse(row.UserGrn.String)
	if err != nil {
		return nil, errorx.Wrap(err, "device authorization user grn", errorx.KindInternal)
	}
	now := time.Now().UTC()
	if err := q.CreateCLISession(ctx, db.CreateCLISessionParams{
		TokenHash:        authn.HashToken(tokens.Access),
		UserGrn:          user.String(),
		CreatedAt:        pgxtype.Timestamptz(now),
		ExpiresAt:        pgxtype.Timestamptz(tokens.AccessExpiresAt),
		RefreshHash:      pgtype.Text{String: authn.HashToken(tokens.Refresh), Valid: true},
		RefreshExpiresAt: pgxtype.Timestamptz(tokens.RefreshExpiresAt),
		Name:             row.ClientName,
	}); err != nil {
		return nil, pgxerr.Classify(err, "open cli session", nil, nil)
	}
	if err := q.DeleteDeviceAuthorization(ctx, row.DeviceCodeHash); err != nil {
		return nil, pgxerr.Classify(err, "spend device authorization", nil, nil)
	}
	return &authn.Session{User: user, CreatedAt: now, ExpiresAt: tokens.AccessExpiresAt, Kind: authn.SessionCLI,
		Name: row.ClientName.String, RefreshExpiresAt: tokens.RefreshExpiresAt}, nil
}
