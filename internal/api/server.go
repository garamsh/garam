// Package api implements the OpenAPI ServerInterface generated into
// the top-level api package (imported as apigen). Handlers enforce
// authentication via the session cookie and org-scoped authorization
// through the Evaluator seam, which the authz gRPC client implements
// (ADR-0003); this package never imports the engine.
package api

import (
	"time"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
	"github.com/garamsh/garam/internal/watch"
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
	users       user.Store
	orgs        org.Store
	agents      agent.Store
	operators   operator.Store
	consents    message.ConsentStore
	watches     watch.Service
	sessions    authn.SessionStore
	states      authn.OAuthStateStore
	credentials authn.PasswordCredentialStore
	attempts    authn.PasswordAttemptStore
	providers   map[string]authn.Provider
	evaluator   Evaluator
	policies    PolicyManager
	authorities AuthorityReader
	authority   operator.AuthorityService
	cfg         Config
}

// Compile-time check that Server satisfies the generated interface.
var _ apigen.ServerInterface = (*Server)(nil)

// NewServer constructs the API server. providers are keyed by their
// Name; unknown provider paths 404 without the providers knowing.
func NewServer(users user.Store, orgs org.Store, agents agent.Store, operators operator.Store, consents message.ConsentStore, watches watch.Service, sessions authn.SessionStore, states authn.OAuthStateStore, credentials authn.PasswordCredentialStore, attempts authn.PasswordAttemptStore, providers []authn.Provider, evaluator Evaluator, policies PolicyManager, authorities AuthorityReader, authority operator.AuthorityService, cfg Config) *Server {
	byName := make(map[string]authn.Provider, len(providers))
	for _, p := range providers {
		byName[p.Name()] = p
	}
	return &Server{users: users, orgs: orgs, agents: agents, operators: operators, consents: consents, watches: watches, sessions: sessions, states: states, credentials: credentials, attempts: attempts, providers: byName, evaluator: evaluator, policies: policies, authorities: authorities, authority: authority, cfg: cfg}
}
