// Package policy is cowork's side of the policy service (ADR-0141
// §12): its settings, the caller token it presents under (ADR-0129),
// and a client that asks and feeds the service as one organization's
// cowork. Nothing here is configured unless the address is set.
package policy

import (
	"github.com/garamsh/garam/lib/errorx"
)

// Config is where the policy service is and how cowork proves who it
// is to the machine token endpoint. The zero Config is no policy
// service: cowork asks the platform.
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

// Configured reports whether any setting is set.
func (c Config) Configured() bool {
	return c.Addr != "" || c.CAFile != "" || c.TokenURL != "" || c.KeyFile != ""
}

// Validate refuses a Config that sets some of the four and not all.
func (c Config) Validate() error {
	if !c.Configured() {
		return nil
	}
	if c.Addr == "" || c.CAFile == "" || c.TokenURL == "" || c.KeyFile == "" {
		return errorx.New(errorx.KindInvalidArgument,
			"policy: the address, CA file, token URL and deployment key file are set together or not at all")
	}
	return nil
}
