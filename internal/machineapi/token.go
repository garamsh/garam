package machineapi

import (
	"net/http"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/machine"
)

// tokenRoutes is every route a token reaches (ADR-0100): the
// operations api/machine.yaml lists `machineToken` on. They are the
// router's patterns, so a route added later is outside the set until
// named here, and so is every route a certificate alone reaches today —
// the plan's later steps add them as each accepts a token.
var tokenRoutes = map[string]bool{
	http.MethodGet + " /identity":                           true,
	http.MethodPost + " /agents":                            true,
	http.MethodPost + " /agents/{agent}/certificate":        true,
	http.MethodPost + " /agents/{agent}/provisioning-state": true,
	http.MethodGet + " /definitions":                        true,
	http.MethodPost + " /definitions/{agent}/claim":         true,
	http.MethodPost + " /agents/{agent}/tokens":             true,
	// An agent's own routes (ADR-0100 G1b): what its adapter calls.
	http.MethodPost + " /agents/{agent}/runtime-state":             true,
	http.MethodPost + " /agents/{agent}/claims":                    true,
	http.MethodPost + " /agents/{agent}/claims/{claim}/renewal":    true,
	http.MethodPost + " /agents/{agent}/claims/{claim}/completion": true,
	http.MethodPost + " /agents/{agent}/turn-events":               true,
	http.MethodPost + " /messages":                                 true,
	http.MethodGet + " /messages/{message}":                        true,
	http.MethodPost + " /outbox-messages":                          true,
	// A hosted operator's lifecycle routes under their v2 contract
	// values (ADR-0100 G1c). Initial issuance is not among them: it goes
	// with the certificate path.
	http.MethodPost + " /operation-authorities/introspection":                  true,
	http.MethodGet + " /operation-references/{ref}":                            true,
	http.MethodPost + " /operators/{controller}/introspection":                 true,
	http.MethodPost + " /agents/{agent}/activations":                           true,
	http.MethodPost + " /agents/{agent}/activations/{activation}/deactivation": true,
	http.MethodPost + " /agents/{agent}/execution/introspection":               true,
	http.MethodPost + " /agents/{agent}/credential-recovery":                   true,
	http.MethodGet + " /agents/{agent}/cutover":                                true,
	http.MethodPost + " /agents/{agent}/cutover/freeze":                        true,
	http.MethodPost + " /agents/{agent}/cutover/switch":                        true,
	http.MethodPost + " /agents/{agent}/cutover/rollback":                      true,
	http.MethodPost + " /operators/{controller}/managed-agents":                true,
}

// ConfineTokens refuses a request a token authenticated on any route
// outside tokenRoutes before its handler runs. The handlers behind it
// answer a machine identity without asking how it was proven, and a
// route that reads a certificate's serial or dates — a renewal, the
// execution fence — has nothing to read off a token. It reads the
// route the router matched, so it runs inside the router.
func ConfineTokens(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := machine.IdentityOf(r)
		if ok && identity.Method() == machine.MethodToken && !tokenRoutes[r.Pattern] {
			writeError(w, errorx.New(errorx.KindUnauthenticated, "this operation does not accept a token"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
