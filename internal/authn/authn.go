// Package authn owns authentication: the OAuth provider abstraction
// and the sessions issued after a successful sign-in.
//
// Providers implement Provider: Google (google.go) and the
// development-only dev provider (dev.go, ADR-0010). Adding a provider
// is a new file and a new entry in the provider map at the wiring
// site — call sites depend only on the Provider interface.
package authn

import (
	"github.com/garamsh/garam/internal/errorx"
)

// ErrSessionNotFound is returned when a session token is unknown or
// expired.
var ErrSessionNotFound = errorx.New(errorx.KindNotFound, "session not found")

// ErrOAuthStateNotLive is returned when a login state was never
// issued, has expired, or has already been consumed. One answer covers
// the three: whoever presented the state is told it buys nothing, and
// not which of the three it is.
var ErrOAuthStateNotLive = errorx.New(errorx.KindUnauthenticated, "oauth state not live")

// Identity is the authenticated identity asserted by a Provider.
type Identity struct {
	Subject  string
	Nickname string
}
