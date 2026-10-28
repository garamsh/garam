// Package api implements the OpenAPI ServerInterface generated into
// the top-level api package (imported as apigen). Handlers enforce
// authentication via the session cookie and org-scoped authorization
// through the Evaluator seam, which the authz gRPC client implements
// (ADR-0003); this package never imports the engine.
package api

import (
	"time"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/authn"
	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/user"
	"github.com/garamsh/garam/platform/internal/watch"
)

// Config tunes the HTTP layer.
type Config struct {
	// SessionTTL is the lifetime of an issued session.
	SessionTTL time.Duration
	// CookieSecure marks cookies Secure. Disable only for local dev
	// over plain HTTP.
	CookieSecure bool
	// TokenURL is the machine listener's token endpoint URL, which
	// operator.ParseTokenEndpoint read: what an issued key's assertions
	// name as their audience (ADR-0102). Empty issues no key.
	TokenURL string
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
	standalone  agent.StandaloneService
	cfg         Config
	// keysPerOperator and keysPerProcess bound the keys this process
	// issues (ADR-0102).
	keysPerOperator *operator.Limiter
	keysPerProcess  *operator.Limiter
	// credentialAudit reads machine credential events (ADR-0114); nil
	// answers that read unavailable.
	credentialAudit CredentialAudit
	// devices keeps device sign-ins (ADR-0120); nil answers the device
	// grant unavailable.
	devices authn.DeviceStore
}

// Compile-time check that Server satisfies the generated interface.
var _ apigen.ServerInterface = (*Server)(nil)

// NewServer constructs the API server. providers are keyed by their
// Name; unknown provider paths 404 without the providers knowing.
func NewServer(users user.Store, orgs org.Store, agents agent.Store, operators operator.Store, consents message.ConsentStore, watches watch.Service, sessions authn.SessionStore, states authn.OAuthStateStore, credentials authn.PasswordCredentialStore, attempts authn.PasswordAttemptStore, providers []authn.Provider, evaluator Evaluator, policies PolicyManager, authorities AuthorityReader, authority operator.AuthorityService, standalone agent.StandaloneService, cfg Config) *Server {
	byName := make(map[string]authn.Provider, len(providers))
	for _, p := range providers {
		byName[p.Name()] = p
	}
	return &Server{users: users, orgs: orgs, agents: agents, operators: operators, consents: consents, watches: watches, sessions: sessions, states: states, credentials: credentials, attempts: attempts, providers: byName, evaluator: evaluator, policies: policies, authorities: authorities, authority: authority, standalone: standalone, cfg: cfg,
		keysPerOperator: operator.NewLimiter(operator.LimiterConfig{Limit: operator.KeyIssuancesPerOperator,
			Window: operator.KeyIssuanceWindow, MaxKeys: operator.TrackedClients}),
		keysPerProcess: operator.NewLimiter(operator.LimiterConfig{Limit: operator.KeyIssuancesPerProcess,
			Window: operator.KeyIssuanceWindow, MaxKeys: 1}),
	}
}
