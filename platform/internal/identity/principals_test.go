package identity_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/operator"
)

const (
	introspectURL = "https://machine.garam.test/oauth/introspect"
	tokenURL      = "https://machine.garam.test/oauth/token"
	jtiA          = "Zk9yLWEtdGVzdC0xMjgtYml0cw"
	jtiB          = "Zk9yLWItdGVzdC0xMjgtYml0cw"
)

var (
	coworkID = identity.PrincipalGRN("acme", identity.ServiceCowork)
	policyID = identity.PrincipalGRN("acme", identity.AudiencePolicy)
)

func rsaKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, operator.MinKeyBits)
	require.NoError(t, err)
	return private
}

func publicKeyOf(t *testing.T, private *rsa.PrivateKey) operator.Key {
	t.Helper()
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	return operator.Key{KID: kid, Algorithm: operator.KeyAlgorithm, Public: &private.PublicKey}
}

// keysFile is the service keys file naming each public key as the
// service's.
func keysFile(t *testing.T, sets map[string][]*rsa.PrivateKey) []byte {
	t.Helper()
	file := map[string]jose.JSONWebKeySet{}
	for service, privates := range sets {
		var set jose.JSONWebKeySet
		for _, private := range privates {
			set.Keys = append(set.Keys, jose.JSONWebKey{Key: &private.PublicKey, Algorithm: "RS256", Use: "sig"})
		}
		file[service] = set
	}
	raw, err := json.Marshal(file)
	require.NoError(t, err)
	return raw
}

// assertion signs a client assertion as principal, addressed to to.
func assertion(t *testing.T, private *rsa.PrivateKey, principal grn.GRN, to, jti string) string {
	t.Helper()
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	signing, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	require.NoError(t, err)
	client := principal.String()
	payload, err := json.Marshal(map[string]any{
		"iss": client, "sub": client, "aud": to,
		"iat": noon.Unix(), "exp": noon.Add(time.Minute).Unix(), "jti": jti,
	})
	require.NoError(t, err)
	object, err := signing.Sign(payload)
	require.NoError(t, err)
	compact, err := object.CompactSerialize()
	require.NoError(t, err)
	return compact
}

// admitter records what it was asked to admit and answers err.
type admitter struct {
	admitted []grn.GRN
	err      error
}

func (a *admitter) Admit(_ context.Context, principal grn.GRN, _ operator.VerifiedAssertion) error {
	a.admitted = append(a.admitted, principal)
	return a.err
}

func principalsOf(keys identity.ServiceKeys, a identity.Admitter) *identity.Principals {
	return identity.NewPrincipals(identity.PrincipalsConfig{
		Keys: keys, ExchangeEndpoint: tokenURL, IntrospectEndpoint: introspectURL,
		Admitter: a, Now: func() time.Time { return noon },
	})
}

func TestPrincipals_AuthenticateAServicesPrincipalByItsKeyOnEachRoute(t *testing.T) {
	private := rsaKey(t)
	store := &admitter{}
	principals := principalsOf(identity.ServiceKeys{identity.ServiceCowork: {publicKeyOf(t, private)}}, store)

	got, err := principals.Authenticate(context.Background(), identity.RouteExchange, "10.0.0.1",
		coworkID.String(), assertion(t, private, coworkID, tokenURL, jtiA))
	require.NoError(t, err)
	assert.Equal(t, identity.Principal{GRN: coworkID, Service: identity.ServiceCowork}, got)

	got, err = principals.Authenticate(context.Background(), identity.RouteIntrospect, "10.0.0.1",
		coworkID.String(), assertion(t, private, coworkID, introspectURL, jtiB))
	require.NoError(t, err)
	assert.Equal(t, identity.ServiceCowork, got.Service)
	assert.Equal(t, []grn.GRN{coworkID, coworkID}, store.admitted, "each assertion is admitted through the store")
}

func TestPrincipals_RefuseWhatIsNotTheServicesOwnAssertion(t *testing.T) {
	private, other := rsaKey(t), rsaKey(t)
	store := &admitter{}
	principals := principalsOf(identity.ServiceKeys{
		identity.ServiceCowork:  {publicKeyOf(t, private)},
		identity.AudiencePolicy: {publicKeyOf(t, other)},
	}, store)

	for name, c := range map[string]struct {
		route             identity.Route
		client, assertion string
	}{
		"a key of another service": {identity.RouteExchange, coworkID.String(), assertion(t, other, coworkID, tokenURL, "a")},
		"the other route's URL":    {identity.RouteIntrospect, coworkID.String(), assertion(t, private, coworkID, tokenURL, "a")},
		"a service with no key":    {identity.RouteExchange, identity.PrincipalGRN("acme", identity.AudienceCodehub).String(), assertion(t, private, identity.PrincipalGRN("acme", identity.AudienceCodehub), tokenURL, "a")},
		"the root-owned form": {identity.RouteIntrospect, identity.PrincipalGRN("root", identity.AudiencePolicy).String(),
			assertion(t, other, identity.PrincipalGRN("root", identity.AudiencePolicy), introspectURL, "a")},
		"a client that is no GRN": {identity.RouteExchange, "cowork", assertion(t, private, coworkID, tokenURL, "a")},
		"not an assertion":        {identity.RouteExchange, coworkID.String(), "x.y.z"},
	} {
		_, err := principals.Authenticate(context.Background(), c.route, "10.0.0.1", c.client, c.assertion)
		assert.ErrorIs(t, err, operator.ErrClientInvalid, name)
	}
	assert.Empty(t, store.admitted, "nothing that failed to verify reaches the store")
}

func TestPrincipals_AnswerWhatTheStoreRefuses(t *testing.T) {
	private := rsaKey(t)
	store := &admitter{err: operator.ErrClientInvalid}
	principals := principalsOf(identity.ServiceKeys{identity.ServiceCowork: {publicKeyOf(t, private)}}, store)

	_, err := principals.Authenticate(context.Background(), identity.RouteExchange, "10.0.0.1",
		coworkID.String(), assertion(t, private, coworkID, tokenURL, jtiA))
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "a replay, a deleted principal or an untimely assertion")
	assert.Equal(t, []grn.GRN{coworkID}, store.admitted)
}

func TestPrincipals_BoundEachSourceOnEachRoute(t *testing.T) {
	private := rsaKey(t)
	principals := principalsOf(identity.ServiceKeys{identity.ServiceCowork: {publicKeyOf(t, private)}}, &admitter{})
	ask := func(route identity.Route, source string) error {
		to := map[identity.Route]string{identity.RouteExchange: tokenURL, identity.RouteIntrospect: introspectURL}[route]
		_, err := principals.Authenticate(context.Background(), route, source, coworkID.String(), assertion(t, private, coworkID, to, jtiA))
		return err
	}

	for range operator.ExchangesPerSource {
		require.NoError(t, ask(identity.RouteIntrospect, "10.0.0.1"))
	}
	var exceeded *operator.Exceeded
	assert.ErrorAs(t, ask(identity.RouteIntrospect, "10.0.0.1"), &exceeded, "past the bound the source is refused")
	assert.NoError(t, ask(identity.RouteIntrospect, "10.0.0.2"), "another source is not held back")
	assert.NoError(t, ask(identity.RouteExchange, "10.0.0.1"), "nor is the same source on the other route")
}

func TestParseServiceKeys(t *testing.T) {
	a, b, c := rsaKey(t), rsaKey(t), rsaKey(t)

	keys, err := identity.ParseServiceKeys(keysFile(t, map[string][]*rsa.PrivateKey{identity.ServiceCowork: {a, b}}))
	require.NoError(t, err)
	assert.Equal(t, []operator.Key{publicKeyOf(t, a), publicKeyOf(t, b)}, keys[identity.ServiceCowork],
		"the kid is the key's thumbprint, at most two keys")

	for name, raw := range map[string][]byte{
		"not JSON":                    []byte("x"),
		"a service off the list":      keysFile(t, map[string][]*rsa.PrivateKey{identity.AudiencePolicy: {a}}),
		"three keys of one service":   keysFile(t, map[string][]*rsa.PrivateKey{identity.ServiceCowork: {a, b, c}}),
		"a private key":               []byte(`{"cowork":{"keys":[` + privateJWK(t, a) + `]}}`),
		"a key that is not RSA":       []byte(`{"cowork":{"keys":[{"kty":"oct","k":"AAAA"}]}}`),
		"a set that is not an object": []byte(`{"cowork":[]}`),
	} {
		_, err := identity.ParseServiceKeys(raw)
		assert.Error(t, err, name)
	}
}

func privateJWK(t *testing.T, private *rsa.PrivateKey) string {
	t.Helper()
	raw, err := json.Marshal(jose.JSONWebKey{Key: private, Algorithm: "RS256", Use: "sig"})
	require.NoError(t, err)
	return string(raw)
}

func TestIsFirstParty_TheClosedListHoldsCowork(t *testing.T) {
	assert.Equal(t, []string{"cowork"}, identity.FirstParty())
	assert.True(t, identity.IsFirstParty("cowork"))
	assert.False(t, identity.IsFirstParty("policy"))
	list := identity.FirstParty()
	list[0] = "changed"
	assert.Equal(t, []string{"cowork"}, identity.FirstParty(), "the list is a copy")
}

func TestIsPrincipalClient(t *testing.T) {
	assert.True(t, identity.IsPrincipalClient(coworkID.String()))
	assert.True(t, identity.IsPrincipalClient("grn:root/iam/service/policy"), "which authentication, not whether it is live")
	assert.False(t, identity.IsPrincipalClient("grn:acme/iam/operator/ci"))
	assert.False(t, identity.IsPrincipalClient("cowork"))
}

func TestIssueCaller_SignsThePrincipalsTokenForPolicyWithNoSession(t *testing.T) {
	issuer, key, _ := fixture(t)

	got, err := issuer.IssueCaller(coworkID)
	require.NoError(t, err)
	claims, err := key.Verify(got.Token, noon)
	require.NoError(t, err)
	assert.Equal(t, issuerName, claims.Issuer)
	assert.Equal(t, coworkID.String(), claims.Subject)
	assert.Equal(t, identity.AudiencePolicy, claims.Audience)
	assert.Equal(t, 300*time.Second, claims.Lifetime)
	assert.Equal(t, 300, got.ExpiresIn)
	assert.Empty(t, claims.SessionID)
	assert.NotEmpty(t, claims.JTI)

	_, active, err := issuer.Introspect(context.Background(), identity.AudiencePolicy, got.Token)
	require.NoError(t, err)
	assert.False(t, active, "introspection answers for a session, and a principal's token has none")
}
