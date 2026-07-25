// Package api implements the OpenAPI ServerInterface generated into
// the top-level api package (imported as apigen). Handlers enforce
// authentication via the session cookie and org-scoped authorization
// through the Evaluator seam, which the authz gRPC client implements
// (ADR-0003); this package never imports the engine.
package api

import (
	"time"

	apigen "github.com/garamsh/gaon/api"
	"github.com/garamsh/gaon/internal/authn"
	"github.com/garamsh/gaon/internal/org"
	"github.com/garamsh/gaon/internal/user"
)

// Config tunes the HTTP layer.
type Config struct {
	// SessionTTL is the lifetime of an issued session.
	SessionTTL time.Duration
	// CookieSecure marks cookies Secure. Disable only for local dev
	// over plain HTTP.
	CookieSecure bool
}

// Server implements apigen.ServerInterface.
type Server struct {
	users     user.Store
	orgs      org.Store
	sessions  authn.SessionStore
	providers map[string]authn.Provider
	evaluator Evaluator
	cfg       Config
}

// Compile-time check that Server satisfies the generated interface.
var _ apigen.ServerInterface = (*Server)(nil)

// NewServer constructs the API server. providers are keyed by their
// Name; unknown provider paths 404 without the providers knowing.
func NewServer(users user.Store, orgs org.Store, sessions authn.SessionStore, providers []authn.Provider, evaluator Evaluator, cfg Config) *Server {
	byName := make(map[string]authn.Provider, len(providers))
	for _, p := range providers {
		byName[p.Name()] = p
	}
	return &Server{users: users, orgs: orgs, sessions: sessions, providers: byName, evaluator: evaluator, cfg: cfg}
}
