package identity

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/operator"
)

// ErrPrincipalsOff is what a request that needs the issuer and the
// service keys is refused with where a process was given either none.
var ErrPrincipalsOff = errorx.New(errorx.KindUnavailable, "identity: the issuer or the service keys are not configured")

// Route is an endpoint of the machine listener that authenticates a
// service principal. Each has its own per-source bound. An assertion
// names the issuer and no route, so what keeps one from being admitted
// on both is its jti, spent once per principal in the database.
type Route int

const (
	// RouteExchange is the token endpoint, where a principal asks for
	// a token of the policy service's audience.
	RouteExchange Route = iota
	// RouteIntrospect is the introspection endpoint.
	RouteIntrospect
)

// PrincipalsConfig is what [NewPrincipals] is built from.
type PrincipalsConfig struct {
	// Keys is asked for each service's public keys on every
	// authentication, so a change to them takes no restart.
	Keys KeySource
	// Issuer is the issuer identifier (GARAM_IDENTITY_ISSUER), the one
	// audience RFC 10070 §4 gives an assertion on either route; empty
	// accepts none.
	Issuer string
	// Admitter holds the principal's record and the jtis spent.
	Admitter Admitter
	// Now supplies the time the per-source bound counts in; zero takes
	// time.Now. The assertion's own time is read from the database.
	Now func() time.Time
}

// Principals authenticates a service principal by `private_key_jwt`
// (ADR-0129 item 2) and answers which principal it proved. It reuses
// the client assertion of ADR-0102 unchanged: the same parse, the same
// verification, the same lifetime and skew, each assertion's jti spent
// once in the database, and the per-source bound of the operator
// exchange. The keys are the service's own and the record is the
// organization's: an assertion signed by a key of the service for a
// principal that no longer exists is refused.
type Principals struct {
	keys      KeySource
	issuer    string
	admitter  Admitter
	perSource map[Route]*operator.Limiter
}

// NewPrincipals builds the authenticator. A route has its own bound,
// so a flood of one does not spend the other's.
func NewPrincipals(cfg PrincipalsConfig) *Principals {
	limiter := func() *operator.Limiter {
		return operator.NewLimiter(operator.LimiterConfig{
			Limit: operator.ExchangesPerSource, Window: operator.ExchangeWindow,
			MaxKeys: operator.TrackedClients, Now: cfg.Now,
		})
	}
	return &Principals{
		keys:      cfg.Keys,
		issuer:    cfg.Issuer,
		admitter:  cfg.Admitter,
		perSource: map[Route]*operator.Limiter{RouteExchange: limiter(), RouteIntrospect: limiter()},
	}
}

// Authenticate returns the principal clientID proved itself as on
// route, [operator.ErrClientInvalid], or [ErrOrganizationUnknown].
// source is the peer's address, which the per-source bound counts
// before anything else is read, and past it the answer is an
// [*operator.Exceeded]. A bad signature, a service with no key, a wrong
// identity, audience or time, a principal gone from an organization
// that exists and a replayed assertion are one refusal. A principal of
// an organization that is not held is told apart only once the
// signature verified (see [Admitter]). Keys that cannot be read are
// [ErrPrincipalsOff]'s kind, unavailable, and not a bad client.
func (p *Principals) Authenticate(ctx context.Context, route Route, source, clientID, assertion string) (Principal, error) {
	if err := p.perSource[route].Check(source); err != nil {
		return Principal{}, err
	}
	keys, err := p.keys.ServiceKeys()
	if err != nil {
		return Principal{}, err
	}
	principal, ok := parsePrincipal(clientID, func(service string) bool { return len(keys[service]) > 0 })
	if !ok {
		return Principal{}, operator.ErrClientInvalid
	}
	parsed, err := operator.ParseServiceAssertion(assertion)
	if err != nil {
		return Principal{}, operator.ErrClientInvalid
	}
	for _, key := range keys[principal.Service] {
		if key.KID != parsed.KID() {
			continue
		}
		key.Operator = principal.GRN
		verified, err := parsed.VerifyService(key, principal.GRN, p.issuer)
		if err != nil {
			return Principal{}, operator.ErrClientInvalid
		}
		if err := p.admitter.Admit(ctx, principal.GRN, verified); err != nil {
			return Principal{}, err
		}
		return principal, nil
	}
	return Principal{}, operator.ErrClientInvalid
}
