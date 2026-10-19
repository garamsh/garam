package cli

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/operator"
)

// identityTokens answers what a token presented on the machine
// listener stands for, from the domain that issued it, which its
// prefix names (ADR-0100): an operator's from internal/operator, an
// agent's and a supervisor's from internal/agent. It is read on every
// request that presents a token, so what it reads beside the token —
// the operator's registration, the agent's assignment and lineage, or
// the supervisor's registration — refuses a deleted operator, a
// withdrawn key, a re-issued supervisor credential, a reassignment, a
// recovery or a revocation from the next request.
type identityTokens struct {
	operators   operator.TokenStore
	agents      agent.AccessTokenStore
	supervisors agent.SupervisorTokenStore
}

func (k identityTokens) Resolve(ctx context.Context, token string) (machine.Bearer, error) {
	// A value no issuance could have produced is refused before any
	// read, so a caller sending noise costs the database nothing.
	switch {
	case operator.WellFormed(token, operator.TokenPrefix):
		return k.operator(ctx, token)
	case agent.WellFormedAccessToken(token):
		return k.agent(ctx, token)
	case agent.WellFormedSupervisorSecret(token, agent.SupervisorTokenPrefix):
		return k.supervisor(ctx, token)
	}
	return machine.Bearer{}, errorx.New(errorx.KindUnauthenticated, "machine: not a token garam issues")
}

func (k identityTokens) operator(ctx context.Context, token string) (machine.Bearer, error) {
	holder, err := k.operators.ResolveToken(ctx, operator.HashToken(token))
	if err != nil {
		return machine.Bearer{}, unresolved(err)
	}
	if holder.Audience != nil {
		return machine.Bearer{}, minted()
	}
	return machine.Bearer{Subject: holder.Operator, ExpiresAt: holder.ExpiresAt}, nil
}

func (k identityTokens) agent(ctx context.Context, token string) (machine.Bearer, error) {
	holder, err := k.agents.ResolveAccessToken(ctx, agent.HashAccessToken(token))
	if err != nil {
		return machine.Bearer{}, unresolved(err)
	}
	if holder.Audience != nil {
		return machine.Bearer{}, minted()
	}
	return machine.Bearer{Subject: holder.Agent, Serial: holder.Serial,
		IssuedAt: holder.IssuedAt, ExpiresAt: holder.ExpiresAt}, nil
}

// supervisor resolves a standalone host's supervisor token, read
// joined to a registration that has not ended (ADR-0100).
func (k identityTokens) supervisor(ctx context.Context, token string) (machine.Bearer, error) {
	holder, err := k.supervisors.ResolveSupervisorToken(ctx, agent.HashSupervisorSecret(token))
	if err != nil {
		return machine.Bearer{}, unresolved(err)
	}
	return machine.Bearer{Subject: holder.Supervisor, IssuedAt: holder.IssuedAt, ExpiresAt: holder.ExpiresAt}, nil
}

// registeredOperators answers whether an organization holds a record
// of an operator, which an agent token's audience must be (ADR-0100).
type registeredOperators struct {
	operators operator.Store
}

func (r registeredOperators) Registered(ctx context.Context, g grn.GRN) error {
	_, err := r.operators.Get(ctx, g)
	return err
}

// unresolved keeps a refusal as the caller's and makes any other
// failure the listener's.
func unresolved(err error) error {
	if errorx.IsKind(err, errorx.KindUnauthenticated) {
		return err
	}
	return errorx.Wrap(err, "machine: token", errorx.KindUnavailable)
}

// minted refuses a token minted for a hosted operator, which is shown
// to it and accepted by garam on no route a token reaches here.
func minted() error {
	return errorx.New(errorx.KindUnauthenticated, "machine: the token is minted for another audience")
}

// machineRequestsMetric counts the machine listener's requests by how
// each proved its identity: `certificate`, `token`, or `none` for one
// that proved none — an enrollment, an exchange, or a refusal. The
// certificate path is removed once this reads no certificate for long
// enough (ADR-0100, docs/plans/2026-10-09-machine-identity-migration.md).
const machineRequestsMetric = "garam_machine_requests_total"

// newMachineRequestCounter builds the counter and registers it on
// registerer, where one is given.
func newMachineRequestCounter(registerer prometheus.Registerer) (*prometheus.CounterVec, error) {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: machineRequestsMetric,
		Help: "Requests the machine listener served, by how each authenticated.",
	}, []string{"authentication"})
	if registerer == nil {
		return counter, nil
	}
	if err := registerer.Register(counter); err != nil {
		return nil, errorx.Wrap(err, "register machine request counter", errorx.KindInternal)
	}
	return counter, nil
}

// countMachineRequests counts each request the generated router
// matched, by the method its identity was proven by. It runs as that
// router's middleware, so what it counts is the specification's
// operations and nothing beside them.
func countMachineRequests(counter *prometheus.CounterVec) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method := "none"
			if identity, ok := machine.IdentityOf(r); ok {
				method = string(identity.Method())
			}
			counter.WithLabelValues(method).Inc()
			next.ServeHTTP(w, r)
		})
	}
}

// tokenExchangesMetric counts the token exchanges the machine listener
// answered, by how the exchange authenticated and how it ended: what
// tells a supervisor's shared-secret exchange from an operator's
// private_key_jwt one, which garam_machine_requests_total cannot,
// since both present a token afterwards (ADR-0102,
// docs/plans/2026-10-09-machine-identity-migration.md). Neither label
// carries anything the caller sent beyond which prefix its credential
// has.
const tokenExchangesMetric = "garam_machine_token_exchanges_total"

// The exchange methods tokenExchangesMetric distinguishes.
const (
	exchangeSupervisorSharedSecret = "supervisor_shared_secret"
	exchangeOperatorPrivateKeyJWT  = "operator_private_key_jwt"
	exchangeUnrecognised           = "unrecognised"
)

// newTokenExchangeCounter builds the counter and registers it on
// registerer, where one is given.
func newTokenExchangeCounter(registerer prometheus.Registerer) (*prometheus.CounterVec, error) {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: tokenExchangesMetric,
		Help: "Token exchanges the machine listener answered, by exchange method and outcome.",
	}, []string{"method", "outcome"})
	if registerer == nil {
		return counter, nil
	}
	if err := registerer.Register(counter); err != nil {
		return nil, errorx.Wrap(err, "register token exchange counter", errorx.KindInternal)
	}
	return counter, nil
}

// countTokenExchanges counts each request to an exchange route by its
// method and outcome once it is answered. It runs as the generated
// router's middleware, outside the request log's, so the route pattern
// is matched and the status written is the one the client read.
func countTokenExchanges(counter *prometheus.CounterVec) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method, ok := exchangeMethod(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			recorder := &exchangeStatus{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)
			counter.WithLabelValues(method, exchangeOutcome(recorder.status)).Inc()
		})
	}
}

// exchangeMethod names how a request to an exchange route
// authenticates, reading the credential's prefix and nothing more.
func exchangeMethod(r *http.Request) (string, bool) {
	switch r.Pattern {
	case http.MethodPost + " /oauth/token":
		return exchangeOperatorPrivateKeyJWT, true
	case http.MethodPost + " /tokens":
		credential := r.Header.Get("Authorization")
		if strings.HasPrefix(credential, operator.CredentialScheme+" "+agent.SupervisorCredentialPrefix) {
			return exchangeSupervisorSharedSecret, true
		}
		return exchangeUnrecognised, true
	}
	return "", false
}

// evidenceWindow is how often one process writes a caller's evidence
// for one path at most (ADR-0109): a gate read in days loses nothing to
// a minute, and the write it saves is on the request's path.
const evidenceWindow = time.Minute

// recordIssuances notes each token an exchange or a mint issued, under
// the path the route and the credential's prefix name, and under the
// audience path as well where the token names one (ADR-0109). The
// subject is the one the handler resolved and reported, so a refused
// request, which reports none, records nothing.
func recordIssuances(evidence *machine.Evidence) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path, ok := issuancePath(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			r, issued := machine.AwaitIssuance(r)
			next.ServeHTTP(w, r)
			issuance, ok := issued()
			if !ok {
				return
			}
			evidence.Note(r.Context(), machine.Sighting{Caller: issuance.Subject, Path: path, IssuedUntil: issuance.Until})
			if issuance.Audience {
				evidence.Note(r.Context(), machine.Sighting{Caller: issuance.Subject, Path: machine.PathAudienceToken,
					IssuedUntil: issuance.Until})
			}
		})
	}
}

// issuancePath names the evidence path of a request to a route that
// issues a token.
func issuancePath(r *http.Request) (machine.Path, bool) {
	if r.Pattern == http.MethodPost+" /agents/{agent}/tokens" {
		return machine.PathAgentMint, true
	}
	switch method, _ := exchangeMethod(r); method {
	case exchangeOperatorPrivateKeyJWT:
		return machine.PathPrivateKeyJWT, true
	case exchangeSupervisorSharedSecret:
		return machine.PathSharedSecret, true
	}
	return "", false
}

// exchangeOutcome reduces a status to what an inventory reads.
func exchangeOutcome(status int) string {
	switch {
	case status < http.StatusBadRequest:
		return "issued"
	case status == http.StatusTooManyRequests:
		return "limited"
	case status < http.StatusInternalServerError:
		return "refused"
	}
	return "failed"
}

// exchangeStatus records the status an exchange was answered with.
type exchangeStatus struct {
	http.ResponseWriter
	status int
}

func (e *exchangeStatus) WriteHeader(code int) {
	e.status = code
	e.ResponseWriter.WriteHeader(code)
}
