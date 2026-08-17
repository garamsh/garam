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

// Session is an authenticated browser session for a user.
type Session struct {
	userGRN   grn.GRN
	createdAt time.Time
	expiresAt time.Time
}

// NewSession constructs a Session that expires after ttl.
func NewSession(userGRN grn.GRN, ttl time.Duration) *Session {
	now := time.Now().UTC()
	return &Session{userGRN: userGRN, createdAt: now, expiresAt: now.Add(ttl)}
}

func (s *Session) User() grn.GRN        { return s.userGRN }
func (s *Session) CreatedAt() time.Time { return s.createdAt }
func (s *Session) ExpiresAt() time.Time { return s.expiresAt }

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
