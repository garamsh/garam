package authn

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// SessionKind tells a browser's session from a CLI's (ADR-0120).
type SessionKind string

const (
	SessionBrowser SessionKind = "browser"
	SessionCLI     SessionKind = "cli"
)

// The lifetimes of a CLI session's two tokens (ADR-0120): the access
// token it presents as a bearer, and the refresh token that buys the
// next one and is spent doing so.
const (
	CLIAccessTTL  = time.Hour
	CLIRefreshTTL = 30 * 24 * time.Hour
)

// Session is an authenticated session for a user: a browser's, held as
// the session cookie, or a CLI's, presented as a bearer (ADR-0005,
// ADR-0120).
type Session struct {
	userGRN   grn.GRN
	createdAt time.Time
	expiresAt time.Time
	// id names the session to its user, who lists and revokes it; empty
	// before it is stored.
	id   string
	kind SessionKind
	// name is what a CLI said it runs on, empty where it said nothing.
	name string
	// refreshExpiresAt is when a CLI session's refresh token lapses,
	// zero for a browser's.
	refreshExpiresAt time.Time
}

// NewSession constructs a Session that expires after ttl.
func NewSession(userGRN grn.GRN, ttl time.Duration) *Session {
	now := time.Now().UTC()
	return &Session{userGRN: userGRN, createdAt: now, expiresAt: now.Add(ttl), kind: SessionBrowser}
}

func (s *Session) User() grn.GRN        { return s.userGRN }
func (s *Session) CreatedAt() time.Time { return s.createdAt }
func (s *Session) ExpiresAt() time.Time { return s.expiresAt }
func (s *Session) ID() string           { return s.id }
func (s *Session) Kind() SessionKind    { return s.kind }
func (s *Session) Name() string         { return s.name }

// RefreshExpiresAt is when a CLI session's refresh token lapses, zero
// for a browser's session.
func (s *Session) RefreshExpiresAt() time.Time { return s.refreshExpiresAt }

// Expired reports whether the session is past its expiry at now.
func (s *Session) Expired(now time.Time) bool { return !now.Before(s.expiresAt) }

// NewToken returns a random session token. The raw token is the bearer
// secret stored in the cookie; only its hash is persisted.
func NewToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errorx.Wrap(err, "session token")
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// HashToken derives the persisted form of a raw secret — a session
// token, or the state a login issues. Only the hash reaches the
// database.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CLITokens are the two tokens a CLI session is answered with, shown
// once and stored as their hashes alone.
type CLITokens struct {
	Access           string
	Refresh          string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

// NewCLITokens draws a CLI session's tokens, lived from now.
func NewCLITokens(now time.Time) (CLITokens, error) {
	access, err := NewToken()
	if err != nil {
		return CLITokens{}, err
	}
	refresh, err := NewToken()
	if err != nil {
		return CLITokens{}, err
	}
	now = now.UTC()
	return CLITokens{Access: access, Refresh: refresh,
		AccessExpiresAt: now.Add(CLIAccessTTL), RefreshExpiresAt: now.Add(CLIRefreshTTL)}, nil
}
