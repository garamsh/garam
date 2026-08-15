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

// Identity is the authenticated identity asserted by a Provider.
type Identity struct {
	Subject  string
	Nickname string
}
