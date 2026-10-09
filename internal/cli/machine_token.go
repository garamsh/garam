package cli

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/operator"
)

// identityTokens answers what a token presented on the machine
// listener stands for, from the domain that issued it, which its
// prefix names (ADR-0100). It is read on every request that presents a
// token, so what it reads beside the token — the operator's
// registration — refuses a deleted operator or a re-issued credential
// from the next request.
type identityTokens struct {
	operators operator.TokenStore
}

func (k identityTokens) Resolve(ctx context.Context, token string) (grn.GRN, time.Time, error) {
	if !strings.HasPrefix(token, operator.TokenPrefix) {
		return grn.GRN{}, time.Time{}, errorx.New(errorx.KindUnauthenticated, "machine: not a token garam issues")
	}
	holder, err := k.operators.ResolveToken(ctx, operator.HashToken(token))
	if err != nil {
		if errorx.IsKind(err, errorx.KindUnauthenticated) {
			return grn.GRN{}, time.Time{}, err
		}
		return grn.GRN{}, time.Time{}, errorx.Wrap(err, "machine: token", errorx.KindUnavailable)
	}
	// A token minted for a hosted operator is shown to it, and garam
	// accepts it on no route a token reaches here.
	if holder.Audience != nil {
		return grn.GRN{}, time.Time{}, errorx.New(errorx.KindUnauthenticated,
			"machine: the token is minted for another audience")
	}
	return holder.Operator, holder.ExpiresAt, nil
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
