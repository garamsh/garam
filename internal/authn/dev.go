package authn

import (
	"context"
	"net/url"

	"github.com/garamsh/garam/internal/errorx"
)

// DevConfig configures the development-only provider.
type DevConfig struct {
	// RedirectURL is the absolute callback URL the login flow returns
	// to, typically the web origin's /api rewrite, e.g.
	// http://localhost:3000/api/auth/callback/dev.
	RedirectURL string
}

// devCode is the constant authorization code the dev provider issues
// and accepts; the OIDC state check already binds the flow, so on
// localhost the code proves nothing.
const devCode = "dev"

type devProvider struct {
	callback *url.URL
}

// NewDevProvider constructs the development-only Provider (ADR-0010).
// It completes the flow locally — no Google, no password — and must
// only be wired behind GARAM_DEV_AUTH.
func NewDevProvider(cfg DevConfig) (Provider, error) {
	u, err := url.Parse(cfg.RedirectURL)
	if err != nil || !u.IsAbs() {
		return nil, errorx.New(errorx.KindInvalidArgument, "dev redirect URL must be absolute")
	}
	return &devProvider{callback: u}, nil
}

// Name returns the provider key used in API paths.
func (d *devProvider) Name() string { return "dev" }

// AuthCodeURL returns the provider's own callback URL with the code
// and state filled in: the dev provider is its own authorization
// endpoint, so the round trip is a single redirect.
func (d *devProvider) AuthCodeURL(state, _ string) string {
	u := *d.callback
	q := u.Query()
	q.Set("code", devCode)
	q.Set("state", state)
	u.RawQuery = q.Encode()
	return u.String()
}

// Exchange accepts the code the provider itself issued and asserts the
// fixed dev identity. The verifier is unused: there is no upstream
// token endpoint to present it to.
func (d *devProvider) Exchange(_ context.Context, code, _ string) (*Identity, error) {
	if code != devCode {
		return nil, errorx.New(errorx.KindUnauthenticated, "invalid dev code")
	}
	return &Identity{Subject: "dev", Nickname: "Dev"}, nil
}
