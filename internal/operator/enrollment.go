package operator

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// EnrollmentTTL is how long a minted token stays spendable. It is
// copied from a screen into a terminal in one sitting, so the window is
// the length of that sitting and not of the working day: a token nobody
// used is a token that was never going to be used.
//
// It is a constant rather than configuration. What a deployment would
// tune it for is the same act every time, and a longer window buys the
// person nothing they cannot get by registering again.
const EnrollmentTTL = 10 * time.Minute

// enrollmentTokenBytes sizes the secret. The same width as an
// invitation's (ADR-0006): unguessable is unguessable, and one number
// for the project's bearer secrets is one number to reason about.
const enrollmentTokenBytes = 24

// Enrollment is permission to ask for one certificate, once.
//
// It is not a credential to the operator's identity and cannot be made
// into one. Presenting it obtains a certificate over a key the
// presenter generated and garam never sees; it authenticates no other
// call, and it is gone the moment it is spent (ADR-0063).
//
// The token exists in this value alone. What reaches the database is
// [HashToken] of it, so a row read out of a dump enrols nothing.
type Enrollment struct {
	token     string
	operator  grn.GRN
	mintedBy  grn.GRN
	mintedAt  time.Time
	expiresAt time.Time
}

// NewEnrollment mints a token for the operator, drawing the secret it
// carries. mintedBy is the member that registered the operator.
func NewEnrollment(operatorGRN, mintedBy grn.GRN) (*Enrollment, error) {
	if operatorGRN.ServiceType != ServiceType {
		return nil, ErrNotAnOperator
	}
	var b [enrollmentTokenBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, errorx.Wrap(err, "enrollment token")
	}
	now := time.Now().UTC()
	return &Enrollment{
		token:     hex.EncodeToString(b[:]),
		operator:  operatorGRN,
		mintedBy:  mintedBy,
		mintedAt:  now,
		expiresAt: now.Add(EnrollmentTTL),
	}, nil
}

// Token returns the secret the person copies. It is answered once, by
// the call that minted it, and garam cannot answer it again.
func (e *Enrollment) Token() string { return e.token }

// Operator returns the GRN the token enrols, which is the only identity
// spending it can obtain a certificate for.
func (e *Enrollment) Operator() grn.GRN { return e.operator }

// MintedBy returns the member that minted the token.
func (e *Enrollment) MintedBy() grn.GRN { return e.mintedBy }

// MintedAt returns when the token was minted.
func (e *Enrollment) MintedAt() time.Time { return e.mintedAt }

// ExpiresAt returns when the token stops being spendable.
func (e *Enrollment) ExpiresAt() time.Time { return e.expiresAt }

// TokenHash returns the persisted form of the token.
func (e *Enrollment) TokenHash() string { return HashToken(e.token) }

// HashToken derives the persisted form of an enrollment token. Only the
// hash reaches the database, as it does for a session and for the state
// a login issues.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
