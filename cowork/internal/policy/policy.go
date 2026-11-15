// Package policy is cowork's side of the policy service (ADR-0141
// §12): its settings, the caller token it presents under (ADR-0129),
// and a client that asks and feeds the service as one organization's
// cowork.
package policy

// Config is where the policy service is and how cowork proves who it
// is to the machine token endpoint.
type Config struct {
	// Addr is the policy service's gRPC address.
	Addr string
	// CAFile is the PEM file of the CA the service's certificate, and
	// the token endpoint's, chain to.
	CAFile string
	// TokenURL is the platform's machine token endpoint, where a caller
	// token is asked for.
	TokenURL string
	// KeyFile is the deployment key, an owner-only PEM file read at
	// each exchange.
	KeyFile string
}
