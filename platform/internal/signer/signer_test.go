package signer_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/signer"
)

func rsaPEM(t *testing.T, bits int, pkcs8 bool) []byte {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, bits)
	require.NoError(t, err)
	if !pkcs8 {
		return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(private)})
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func newKey(t *testing.T) *signer.Key {
	t.Helper()
	key, err := signer.ParseKey(rsaPEM(t, 2048, true))
	require.NoError(t, err)
	return key
}

var issuedAt = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

func claims() signer.Claims {
	return signer.Claims{
		Issuer: "https://garam.test", Subject: "platform", Audience: "policy",
		IssuedAt: issuedAt, Lifetime: time.Minute,
	}
}

// members decodes one segment of a compact token into its JSON members.
func members(t *testing.T, token string, segment int) map[string]any {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(strings.Split(token, ".")[segment])
	require.NoError(t, err)
	out := map[string]any{}
	require.NoError(t, json.Unmarshal(raw, &out))
	return out
}

func TestSign_TokenFollowsTheProfile(t *testing.T) {
	key := newKey(t)
	token, err := key.Sign(claims())
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"alg": "RS256", "kid": key.ID(), "typ": "JWT"},
		members(t, token, 0), "protected header is alg, kid and typ alone")
	assert.Equal(t, map[string]any{
		"iss": "https://garam.test", "sub": "platform", "aud": "policy",
		"iat": float64(issuedAt.Unix()), "exp": float64(issuedAt.Add(time.Minute).Unix()),
	}, members(t, token, 1), "aud is one string and dates are integers")
}

func TestJWKS_VerifiesWhatSignSigned(t *testing.T) {
	key := newKey(t)
	token, err := key.Sign(claims())
	require.NoError(t, err)
	published, err := key.JWKS()
	require.NoError(t, err)

	var set map[string][]map[string]any
	require.NoError(t, json.Unmarshal(published, &set))
	require.Len(t, set["keys"], 1)
	jwk := set["keys"][0]
	assert.ElementsMatch(t, []string{"kty", "use", "alg", "kid", "n", "e"}, keysOf(jwk),
		"a member the profile does not name refuses the file, a private one above all")
	assert.Equal(t, key.ID(), jwk["kid"])
	assert.Equal(t, "RSA", jwk["kty"])
	assert.Equal(t, "sig", jwk["use"])
	assert.Equal(t, "RS256", jwk["alg"])

	var parsed jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(published, &parsed))
	signed, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256})
	require.NoError(t, err)
	found := parsed.Key(signed.Signatures[0].Header.KeyID)
	require.Len(t, found, 1)
	_, err = signed.Verify(found[0].Key)
	require.NoError(t, err)
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestSign_RefusesClaimsOutsideTheProfile(t *testing.T) {
	key := newKey(t)
	_, err := key.Sign(claims())
	require.NoError(t, err, "control: the profile's own claims sign")

	tests := map[string]func(*signer.Claims){
		"lifetime past 300 seconds": func(c *signer.Claims) { c.Lifetime = signer.MaxLifetime + time.Second },
		"zero lifetime":             func(c *signer.Claims) { c.Lifetime = 0 },
		"sub-second lifetime":       func(c *signer.Claims) { c.Lifetime = 500 * time.Millisecond },
		"no issuer":                 func(c *signer.Claims) { c.Issuer = "" },
		"no subject":                func(c *signer.Claims) { c.Subject = "" },
		"no audience":               func(c *signer.Claims) { c.Audience = "" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			c := claims()
			mutate(&c)
			_, err := key.Sign(c)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
	_, err = key.Sign(func() signer.Claims { c := claims(); c.Lifetime = signer.MaxLifetime; return c }())
	assert.NoError(t, err, "control: the longest lifetime the profile allows signs")
}

func TestParseKey_AcceptsRSAOfTheProfileAndRefusesTheRest(t *testing.T) {
	for name, pemBytes := range map[string][]byte{
		"PKCS#8 2048": rsaPEM(t, 2048, true),
		"PKCS#1 2048": rsaPEM(t, 2048, false),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := signer.ParseKey(pemBytes)
			require.NoError(t, err)
		})
	}

	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	ecDER, err := x509.MarshalPKCS8PrivateKey(ec)
	require.NoError(t, err)
	refused := map[string][]byte{
		"1024 bits":                    rsaPEM(t, 1024, true),
		"not PEM":                      []byte("not a key"),
		"EC key":                       pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: ecDER}),
		"a PEM that is no private key": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1}}),
	}
	for name, pemBytes := range refused {
		t.Run(name, func(t *testing.T) {
			_, err := signer.ParseKey(pemBytes)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

func TestParseKey_NamesTheSameKeyTheSameWay(t *testing.T) {
	pemBytes := rsaPEM(t, 2048, true)
	first, err := signer.ParseKey(pemBytes)
	require.NoError(t, err)
	second, err := signer.ParseKey(pemBytes)
	require.NoError(t, err)
	assert.Equal(t, first.ID(), second.ID())
	assert.NotEqual(t, first.ID(), newKey(t).ID(), "control: another key has another id")
}

func TestLoadKey_ReadsOnlyAnOwnerOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.pem")
	require.NoError(t, os.WriteFile(path, rsaPEM(t, 2048, true), 0o600))
	require.NoError(t, os.Chmod(path, 0o600))
	_, err := signer.LoadKey(path)
	require.NoError(t, err, "control: an owner-only file loads")

	require.NoError(t, os.Chmod(path, 0o640))
	_, err = signer.LoadKey(path)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

func mustSign(t *testing.T, key *signer.Key, c signer.Claims) string {
	t.Helper()
	token, err := key.Sign(c)
	require.NoError(t, err)
	return token
}

func TestSign_CarriesTheJTIAndSessionOnlyWhereSet(t *testing.T) {
	key := newKey(t)
	c := claims()
	c.JTI, c.SessionID = "tok-1", "sid-1"

	body := members(t, mustSign(t, key, c), 1)
	assert.Equal(t, "tok-1", body["jti"])
	assert.Equal(t, "sid-1", body["sid"])
	assert.NotContains(t, members(t, mustSign(t, key, claims()), 1), "jti")
	assert.NotContains(t, members(t, mustSign(t, key, claims()), 1), "sid")
}

func TestVerify_ReturnsWhatSignSigned(t *testing.T) {
	key := newKey(t)
	c := claims()
	c.JTI, c.SessionID = "tok-1", "sid-1"

	got, err := key.Verify(mustSign(t, key, c), issuedAt.Add(time.Second))
	require.NoError(t, err)
	assert.Equal(t, c.Issuer, got.Issuer)
	assert.Equal(t, c.Subject, got.Subject)
	assert.Equal(t, c.Audience, got.Audience)
	assert.True(t, c.IssuedAt.Equal(got.IssuedAt))
	assert.Equal(t, c.Lifetime, got.Lifetime)
	assert.Equal(t, "tok-1", got.JTI)
	assert.Equal(t, "sid-1", got.SessionID)
}

func TestVerify_RefusesWhatThisKeyDidNotSignOrIsOutOfTime(t *testing.T) {
	key := newKey(t)
	token := mustSign(t, key, claims())
	parts := strings.Split(token, ".")
	forged := parts[0] + "." + base64.RawURLEncoding.EncodeToString(
		[]byte(`{"iss":"https://garam.test","sub":"root","aud":"policy","iat":1,"exp":9999999999}`)) + "." + parts[2]

	for name, c := range map[string]struct {
		token string
		now   time.Time
	}{
		"another key's token": {mustSign(t, newKey(t), claims()), issuedAt},
		"a forged payload":    {forged, issuedAt},
		"an expired token":    {token, issuedAt.Add(time.Minute)},
		"a future token":      {token, issuedAt.Add(-time.Second)},
		"not a token":         {"x.y.z", issuedAt},
	} {
		_, err := key.Verify(c.token, c.now)
		assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), name)
	}
}
