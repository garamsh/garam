package identity_test

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/operator"
)

const (
	introspectURL = "https://machine.garam.test/oauth/introspect"
	tokenURL      = "https://machine.garam.test/oauth/token"
	jtiA          = "Zk9yLWEtdGVzdC0xMjgtYml0cw"
)

func rsaKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, operator.MinKeyBits)
	require.NoError(t, err)
	return private
}

// keysFile is the service keys file naming public as audience's key.
func keysFile(t *testing.T, audience string, public *rsa.PublicKey) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]jose.JSONWebKeySet{
		audience: {Keys: []jose.JSONWebKey{{Key: public, Algorithm: "RS256", Use: "sig"}}},
	})
	require.NoError(t, err)
	return raw
}

// assertion signs a client assertion as the service of audience,
// addressed to the URL named by to.
func assertion(t *testing.T, private *rsa.PrivateKey, audience, jti, to string) string {
	t.Helper()
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	signing, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	require.NoError(t, err)
	caller := identity.CallerGRN(audience).String()
	payload, err := json.Marshal(map[string]any{
		"iss": caller, "sub": caller, "aud": to,
		"iat": noon.Unix(), "exp": noon.Add(time.Minute).Unix(), "jti": jti,
	})
	require.NoError(t, err)
	object, err := signing.Sign(payload)
	require.NoError(t, err)
	compact, err := object.CompactSerialize()
	require.NoError(t, err)
	return compact
}

func TestCallers_AuthenticateAServiceByItsOwnKeyOnce(t *testing.T) {
	private := rsaKey(t)
	callers, err := identity.ParseCallers(keysFile(t, "policy", &private.PublicKey), introspectURL, func() time.Time { return noon })
	require.NoError(t, err)
	client := identity.CallerGRN("policy").String()

	audience, err := callers.Authenticate(client, assertion(t, private, "policy", jtiA, introspectURL))
	require.NoError(t, err)
	assert.Equal(t, "policy", audience)

	_, err = callers.Authenticate(client, assertion(t, private, "policy", jtiA, introspectURL))
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "an assertion is spent once")
}

func TestCallers_RefuseWhatIsNotTheServicesOwnAssertion(t *testing.T) {
	private, other := rsaKey(t), rsaKey(t)
	callers, err := identity.ParseCallers(keysFile(t, "policy", &private.PublicKey), introspectURL, func() time.Time { return noon })
	require.NoError(t, err)
	policy := identity.CallerGRN("policy").String()

	for name, c := range map[string]struct{ client, assertion string }{
		"another key":             {policy, assertion(t, other, "policy", jtiA, introspectURL)},
		"another audience's id":   {identity.CallerGRN("codehub").String(), assertion(t, private, "codehub", jtiA, introspectURL)},
		"the token endpoint":      {policy, assertion(t, private, "policy", jtiA, tokenURL)},
		"an unknown audience":     {identity.CallerGRN("cowork").String(), assertion(t, private, "cowork", jtiA, introspectURL)},
		"a client that is no GRN": {"policy", assertion(t, private, "policy", jtiA, introspectURL)},
		"not an assertion":        {policy, "x.y.z"},
	} {
		_, err := callers.Authenticate(c.client, c.assertion)
		assert.ErrorIs(t, err, operator.ErrClientInvalid, name)
	}
}

func TestParseCallers_RefusesWhatIsNotAKeySetOfAServedAudience(t *testing.T) {
	private := rsaKey(t)
	for name, raw := range map[string][]byte{
		"not JSON":            []byte("x"),
		"an unknown audience": keysFile(t, "cowork", &private.PublicKey),
	} {
		_, err := identity.ParseCallers(raw, introspectURL, nil)
		assert.Error(t, err, name)
	}
}
