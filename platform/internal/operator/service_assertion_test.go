package operator_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/operator"
)

const issuerIdentifier = "https://garam.test/api/identity"

// serviceAssertion signs claims for client under a header whose typ is
// typ, with aud as given: a string, or a list for the case naming two.
func serviceAssertion(t *testing.T, typ string, aud any) string {
	t.Helper()
	private, _ := keys(t)
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	c := claims(time.Now())
	c["aud"] = aud
	return signRaw(t, private, `{"alg":"RS256","kid":"`+kid+`","typ":"`+typ+`"}`, encode(t, c))
}

// verifyService parses and verifies compact as a service principal's
// assertion against the issuer identifier.
func verifyService(t *testing.T, compact string) error {
	t.Helper()
	private, _ := keys(t)
	parsed, err := operator.ParseServiceAssertion(compact)
	if err != nil {
		return err
	}
	_, err = parsed.VerifyService(recorded(t, client, private), client, issuerIdentifier)
	return err
}

// TestServiceAssertion_TakesTheIssuerIdentifierAsItsOnlyAudience holds
// RFC 10070 §4's form as the only one. The first two are accepted, each
// a control for the refusals: the issuer identifier under either typ.
// Refused are the token URL under JWT, the form an operator's exchange
// keeps, and under the new typ, both audiences together, the issuer as
// one of a list, and another audience.
func TestServiceAssertion_TakesTheIssuerIdentifierAsItsOnlyAudience(t *testing.T) {
	for name, c := range map[string]struct {
		typ      string
		aud      any
		accepted bool
	}{
		"the issuer under client-authentication+jwt": {operator.TypeClientAuth, issuerIdentifier, true},
		"the issuer under JWT":                       {operator.TypeJWT, issuerIdentifier, true},
		"the token URL under JWT":                    {operator.TypeJWT, endpoint, false},
		"both audiences together":                    {operator.TypeClientAuth, []string{issuerIdentifier, endpoint}, false},
		"both audiences together under JWT":          {operator.TypeJWT, []string{endpoint, issuerIdentifier}, false},
		"the issuer as one of a list":                {operator.TypeClientAuth, []string{issuerIdentifier}, false},
		"another audience":                           {operator.TypeClientAuth, "https://garam.test/api/other", false},
		"the issuer with a trailing slash":           {operator.TypeClientAuth, issuerIdentifier + "/", false},
		"the token URL under the new typ":            {operator.TypeClientAuth, endpoint, false},
		"a typ that is neither":                      {"at+jwt", issuerIdentifier, false},
	} {
		err := verifyService(t, serviceAssertion(t, c.typ, c.aud))
		if c.accepted {
			assert.NoError(t, err, name)
		} else {
			assert.ErrorIs(t, err, operator.ErrClientInvalid, name)
		}
	}
}

// TestServiceAssertion_RefusesTheTokenURLFormAnOperatorKeeps holds that
// the assertion naming the token URL under JWT verifies as an
// operator's and is refused as a service principal's. Two controls sit
// beside the refusal: the operator's verification of the same bytes,
// and the issuer form accepted as a service principal's.
func TestServiceAssertion_RefusesTheTokenURLFormAnOperatorKeeps(t *testing.T) {
	private, _ := keys(t)
	key := recorded(t, client, private)

	urlForm := serviceAssertion(t, operator.TypeJWT, endpoint)
	asOperator, err := operator.ParseClientAssertion(urlForm)
	require.NoError(t, err)
	_, err = asOperator.Verify(key, client, endpoint)
	require.NoError(t, err, "the control: the operator's exchange keeps the URL form")
	asService, err := operator.ParseServiceAssertion(urlForm)
	require.NoError(t, err)
	_, err = asService.VerifyService(key, client, issuerIdentifier)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "the URL form, as a service principal's")

	issuerForm, err := operator.ParseServiceAssertion(serviceAssertion(t, operator.TypeClientAuth, issuerIdentifier))
	require.NoError(t, err)
	_, err = issuerForm.VerifyService(key, client, issuerIdentifier)
	assert.NoError(t, err, "the control: the issuer form, as a service principal's")
}

// TestServiceAssertion_AcceptsNothingWhereTheIssuerIsUnset holds that a
// deployment with no issuer identifier accepts no service assertion,
// rather than matching an empty audience.
func TestServiceAssertion_AcceptsNothingWhereTheIssuerIsUnset(t *testing.T) {
	private, _ := keys(t)
	key := recorded(t, client, private)
	for name, aud := range map[string]string{
		"an empty audience": "",
		"the issuer":        issuerIdentifier,
		"the token URL":     endpoint,
	} {
		parsed, err := operator.ParseServiceAssertion(serviceAssertion(t, operator.TypeClientAuth, aud))
		require.NoError(t, err, name)
		_, err = parsed.VerifyService(key, client, "")
		assert.ErrorIs(t, err, operator.ErrClientInvalid, name)
	}
}

// TestClientAssertion_AnOperatorsAssertionTakesNeitherNewForm holds that
// the operator exchange is unchanged: the new typ is refused at parse,
// and the issuer identifier is not an audience of Verify. The same
// bytes a service principal's parse accepts are the control.
func TestClientAssertion_AnOperatorsAssertionTakesNeitherNewForm(t *testing.T) {
	typed := serviceAssertion(t, operator.TypeClientAuth, issuerIdentifier)
	_, err := operator.ParseServiceAssertion(typed)
	require.NoError(t, err, "the control: a service principal's parse accepts it")
	_, err = operator.ParseClientAssertion(typed)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "the new typ")

	private, _ := keys(t)
	plain := serviceAssertion(t, operator.TypeJWT, issuerIdentifier)
	parsed, err := operator.ParseClientAssertion(plain)
	require.NoError(t, err)
	_, err = parsed.Verify(recorded(t, client, private), client, issuerIdentifier)
	require.NoError(t, err, "the control: Verify's audience is whatever endpoint it is given")
	_, err = parsed.Verify(recorded(t, client, private), client, endpoint)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "the issuer identifier is not the operator's audience")
}
