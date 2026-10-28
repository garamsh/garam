// Package authn owns authentication: the OAuth provider abstraction
// and the sessions issued after a successful sign-in.
//
// Providers implement Provider: Google (google.go). Adding a provider
// is a new file and a new entry in the provider map at the wiring
// site — call sites depend only on the Provider interface.
package authn

import (
	"fmt"

	"github.com/garamsh/garam/internal/errorx"
)

// ErrSessionNotFound is returned when a session token is unknown or
// expired.
var ErrSessionNotFound = errorx.New(errorx.KindNotFound, "session not found")

// ErrRefreshTokenNotLive is returned when a CLI session's refresh token
// is unknown, lapsed or already spent. One answer covers the three.
var ErrRefreshTokenNotLive = errorx.New(errorx.KindUnauthenticated, "refresh token not live")

// ErrOAuthStateNotLive is returned when a login state was never
// issued, has expired, or has already been consumed. One answer covers
// the three: whoever presented the state is told it buys nothing, and
// not which of the three it is.
var ErrOAuthStateNotLive = errorx.New(errorx.KindUnauthenticated, "oauth state not live")

// ErrOAuthStateRateLimited is returned when a login state is issued
// while MaxLiveOAuthStates are already live.
var ErrOAuthStateRateLimited = errorx.New(errorx.KindResourceExhausted, "oauth state issue rate exceeded")

// MaxLiveOAuthStates bounds the states live at one time, and with them
// the write rate an unauthenticated caller can drive on
// /auth/login/{provider}: a state lives at most one flow lifetime, so
// no caller moves the table past this many writes per lifetime. The
// store enforces it where the write happens — in the database the
// processes share — so the bound holds however many replicas serve the
// endpoint, and keys on no client address a proxy or a spoofed header
// could bend (ADR-0030).
const MaxLiveOAuthStates = 1024

// ErrPasswordCredentialNotFound is returned when a user has no
// password credential. It never reaches a caller of the sign-in
// endpoint, which answers ErrPasswordRejected for a username no
// credential belongs to (ADR-0041).
var ErrPasswordCredentialNotFound = errorx.New(errorx.KindNotFound, "password credential not found")

// ErrPasswordAttemptRateLimited is returned when a password
// verification is recorded while MaxLivePasswordAttempts are already
// live.
var ErrPasswordAttemptRateLimited = errorx.New(errorx.KindResourceExhausted, "password attempt rate exceeded")

// MaxLivePasswordAttempts bounds the password verifications live at one
// time: the store refuses past this many in the statement that writes,
// and nothing is hashed past it (ADR-0041). A verification that succeeds
// releases its own attempt, so only failures accumulate.
const MaxLivePasswordAttempts = 1024

// ErrPasswordRejected is the single answer to a password that does not
// verify and to a username no credential belongs to. Telling the two
// apart would publish the account list, so one error covers both and
// names neither (ADR-0041).
var ErrPasswordRejected = errorx.New(errorx.KindUnauthenticated, "username or password is incorrect")

// ErrPasswordTooShort and ErrPasswordTooLong refuse a password at
// registration. Neither carries what was sent.
var (
	ErrPasswordTooShort = errorx.New(errorx.KindInvalidArgument, fmt.Sprintf("password must be at least %d characters", MinPasswordLength))
	ErrPasswordTooLong  = errorx.New(errorx.KindInvalidArgument, fmt.Sprintf("password must be at most %d bytes", MaxPasswordLength))
)

// Identity is the authenticated identity asserted by a Provider.
type Identity struct {
	Subject  string
	Nickname string
}
