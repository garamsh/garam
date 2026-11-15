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

// TestCodehubPrincipal_ExchangeTakesTheIssuerIdentifierAsItsOnlyAudience
// holds RFC 10070 §4's form as the only one over Postgres: an assertion
// whose `aud` is the issuer identifier alone is exchanged under typ
// `client-authentication+jwt` and under `JWT`. The token URL, the form
// an operator's exchange keeps, is `invalid_client` under either typ,
// as are one naming both audiences and another audience. The issuer
// form's success is the control.
func TestCodehubPrincipal_ExchangeTakesTheIssuerIdentifierAsItsOnlyAudience(t *testing.T) {
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

	for name, typ := range map[string]string{"typed": operator.TypeClientAuth, "untyped": operator.TypeJWT} {
		status, body := ask(typ, issuerURL)
		require.Equal(t, http.StatusOK, status, "the control: the issuer identifier, "+name+": "+body)
	}
	for name, c := range map[string]struct {
		typ string
		aud any
	}{
		"the token URL under JWT":         {operator.TypeJWT, tokenEndpoint},
		"the token URL under the new typ": {operator.TypeClientAuth, tokenEndpoint},
		"both audiences":                  {operator.TypeClientAuth, []string{issuerURL, tokenEndpoint}},
		"another audience":                {operator.TypeClientAuth, issuerURL + "/other"},
	} {
		status, body := ask(c.typ, c.aud)
		assert.Equal(t, http.StatusBadRequest, status, name+": "+body)
		assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body), name)
	}
}
