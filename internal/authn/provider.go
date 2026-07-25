package authn

import (
	"context"
)

// Provider is an OIDC authentication provider. Implementations run the
// authorization code flow with PKCE: AuthCodeURL starts the flow,
// Exchange completes it and returns the verified Identity.
type Provider interface {
	// Name is the provider key used in API paths, e.g. "google".
	Name() string
	// AuthCodeURL returns the provider authorization URL for the given
	// OIDC state and PKCE code verifier; the provider derives the S256
	// challenge.
	AuthCodeURL(state, verifier string) string
	// Exchange trades an authorization code for the verified Identity.
	// verifier is the PKCE code verifier generated alongside state.
	Exchange(ctx context.Context, code, verifier string) (*Identity, error)
}
