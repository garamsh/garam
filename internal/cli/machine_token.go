package cli

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/operator"
)

// identityTokens answers what a token presented on the machine
// listener stands for, from the domain that issued it, which its
// prefix names (ADR-0100): an operator's from internal/operator, an
// agent's from internal/agent. It is read on every request that
// presents a token, so what it reads beside the token — the operator's
// registration, or the agent's assignment and lineage — refuses a
// deleted operator, a re-issued credential, a reassignment or a
// recovery from the next request.
type identityTokens struct {
	operators operator.TokenStore
	agents    agent.AccessTokenStore
}

func (k identityTokens) Resolve(ctx context.Context, token string) (machine.Bearer, error) {
	// A value no issuance could have produced is refused before any
	// read, so a caller sending noise costs the database nothing.
	switch {
	case operator.WellFormed(token, operator.TokenPrefix):
		return k.operator(ctx, token)
	case agent.WellFormedAccessToken(token):
		return k.agent(ctx, token)
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
