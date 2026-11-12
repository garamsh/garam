package integration_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/operator"
)

// TestCodehubPrincipal_ExchangeTakesTheIssuerIdentifierAsAudience holds
// RFC 10070 §4's form over Postgres: an assertion whose `aud` is the
// issuer identifier alone is exchanged under typ
// `client-authentication+jwt` and under `JWT`, beside the token-URL
// form. One naming both audiences, the token URL under the new typ and
// another audience are `invalid_client`. The first request is the
// control.
func TestCodehubPrincipal_ExchangeTakesTheIssuerIdentifierAsAudience(t *testing.T) {
	s := newIssuerStack(t)
	tokenEndpoint := s.machine + "/oauth/token"
	ask := func(typ string, aud any) (int, string) {
		return post(t, tokenEndpoint, url.Values{
			"grant_type": {operator.GrantTypeClientCredentials}, "client_id": {s.codehubID.String()},
			"client_assertion":      {principalAssertionAs(t, s.codehub, s.codehubID, typ, aud, "")},
			"client_assertion_type": {operator.ClientAssertionTypeJWT},
			"audience":              {identity.AudiencePolicy},
		})
	}

	status, body := ask("JWT", tokenEndpoint)
	require.Equal(t, http.StatusOK, status, "the control: the token-URL form: "+body)
	for name, typ := range map[string]string{"typed": "client-authentication+jwt", "untyped": "JWT"} {
		status, body = ask(typ, issuerURL)
		assert.Equal(t, http.StatusOK, status, "the issuer identifier, "+name+": "+body)
	}
	for name, c := range map[string]struct {
		typ string
		aud any
	}{
		"both audiences":                  {"client-authentication+jwt", []string{issuerURL, tokenEndpoint}},
		"the token URL under the new typ": {"client-authentication+jwt", tokenEndpoint},
		"another audience":                {"client-authentication+jwt", issuerURL + "/other"},
	} {
		status, body = ask(c.typ, c.aud)
		assert.Equal(t, http.StatusBadRequest, status, name+": "+body)
		assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body), name)
	}
}
