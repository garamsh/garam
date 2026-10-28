package operator_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

const endpoint = "https://machine.garam.test/oauth/token"

var (
	client   = grn.New("acme", grn.DefaultProject, operator.ServiceType, "controller")
	stranger = grn.New("acme", grn.DefaultProject, operator.ServiceType, "control")
)

var (
	signingOnce sync.Once
	signingKey  *rsa.PrivateKey
	otherKey    *rsa.PrivateKey
)

// keys answers two 2048-bit keys, generated once for the package: the
// smallest size the profile verifies, so the cases stay fast.
func keys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	t.Helper()
	signingOnce.Do(func() {
		var err error
		signingKey, err = rsa.GenerateKey(rand.Reader, operator.MinKeyBits)
		require.NoError(t, err)
		otherKey, err = rsa.GenerateKey(rand.Reader, operator.MinKeyBits)
		require.NoError(t, err)
	})
	return signingKey, otherKey
}

// recorded is the Key a store would answer for private's public half,
// registered to owner.
func recorded(t *testing.T, owner grn.GRN, private *rsa.PrivateKey) operator.Key {
	t.Helper()
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	return operator.Key{ID: 7, Operator: owner, KID: kid, Algorithm: operator.KeyAlgorithm,
		Public: &private.PublicKey, State: operator.KeyStateActive}
}

// claims is a valid claim set for client, issued now.
func claims(now time.Time) map[string]any {
	return map[string]any{
		"iss": client.String(),
		"sub": client.String(),
		"aud": endpoint,
		"iat": now.Unix(),
		"exp": now.Add(time.Minute).Unix(),
		"jti": "Zk9yLWEtdGVzdC0xMjgtYml0cw",
	}
}

func b64(raw string) string { return base64.RawURLEncoding.EncodeToString([]byte(raw)) }

// signRaw signs header and payload exactly as written, RS256, so a
// case can put in them what no JOSE library would write.
func signRaw(t *testing.T, private *rsa.PrivateKey, header, payload string) string {
	t.Helper()
	input := b64(header) + "." + b64(payload)
	digest := sha256.Sum256([]byte(input))
	sig, err := rsa.SignPKCS1v15(rand.Reader, private, crypto.SHA256, digest[:])
	require.NoError(t, err)
	return input + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// headerFor is the protected header the profile requires for private.
func headerFor(t *testing.T, private *rsa.PrivateKey) string {
	t.Helper()
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	return `{"alg":"RS256","kid":"` + kid + `","typ":"JWT"}`
}

func encode(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

// verify parses and verifies compact as an assertion of client against
// key, answering whatever refused it.
func verify(compact string, key operator.Key) (operator.VerifiedAssertion, error) {
	parsed, err := operator.ParseClientAssertion(compact)
	if err != nil {
		return operator.VerifiedAssertion{}, err
	}
	return parsed.Verify(key, client, endpoint)
}

// TestClientAssertion_AStockJOSESignatureOverTheIssuedPEMVerifies is
// the interoperability claim (ADR-0102): the PEM garam issues parses
// with the standard library as PKCS#8, and go-jose — a stock JOSE
// implementation, not this package — signs an assertion this profile
// verifies. A JWT the same key signs with another algorithm is the
// control on the pin: it is refused.
func TestClientAssertion_AStockJOSESignatureOverTheIssuedPEMVerifies(t *testing.T) {
	issued, err := operator.NewIssuedKey(client, grn.New("acme", grn.DefaultProject, "user", "ada"))
	require.NoError(t, err)
	block, rest := pem.Decode(issued.PrivatePEM())
	require.NotNil(t, block)
	assert.Empty(t, rest)
	assert.Equal(t, "PRIVATE KEY", block.Type, "PKCS#8, not PKCS#1's RSA PRIVATE KEY")
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	private, ok := parsed.(*rsa.PrivateKey)
	require.True(t, ok)
	assert.Equal(t, operator.IssuedKeyBits, private.N.BitLen())

	jwk, err := issued.PublicJWK()
	require.NoError(t, err)
	assert.NotContains(t, jwk, `"d"`, "the stored JWK carries no private member")
	public, err := operator.ParsePublicJWK(jwk, issued.KID())
	require.NoError(t, err)
	key := operator.Key{ID: 1, Operator: client, KID: issued.KID(), Algorithm: operator.KeyAlgorithm, Public: public}

	sign := func(alg jose.SignatureAlgorithm) string {
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: alg, Key: private},
			(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", issued.KID()))
		require.NoError(t, err)
		signed, err := signer.Sign([]byte(encode(t, claims(time.Now()))))
		require.NoError(t, err)
		compact, err := signed.CompactSerialize()
		require.NoError(t, err)
		return compact
	}

	verified, err := verify(sign(jose.RS256), key)
	require.NoError(t, err)
	assert.Equal(t, client, verified.Operator)
	assert.Equal(t, int64(1), verified.KeyID)

	_, err = verify(sign(jose.PS256), key)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "the same key under PS256 is refused")
}

// TestClientAssertion_RefusesWhatTheProfileExcludes holds every
// refusal of ParseClientAssertion and Verify to one control: the same
// key, header and claims with nothing changed verifies, so what
// refuses each case is the change it makes.
func TestClientAssertion_RefusesWhatTheProfileExcludes(t *testing.T) {
	private, other := keys(t)
	key := recorded(t, client, private)
	now := time.Now()
	header := headerFor(t, private)
	with := func(change func(map[string]any)) string {
		c := claims(now)
		change(c)
		return encode(t, c)
	}
	valid := encode(t, claims(now))

	_, err := verify(signRaw(t, private, header, valid), key)
	require.NoError(t, err, "the control verifies")

	kid := key.KID
	tests := []struct {
		name    string
		compact string
		key     operator.Key
	}{
		{"alg none", b64(`{"alg":"none","kid":"`+kid+`","typ":"JWT"}`) + "." + b64(valid) + ".", key},
		{"a MAC algorithm", signRaw(t, private, `{"alg":"HS256","kid":"`+kid+`","typ":"JWT"}`, valid), key},
		{"another RSA algorithm", signRaw(t, private, `{"alg":"RS512","kid":"`+kid+`","typ":"JWT"}`, valid), key},
		{"no typ", signRaw(t, private, `{"alg":"RS256","kid":"`+kid+`"}`, valid), key},
		{"a message typ", signRaw(t, private, `{"alg":"RS256","kid":"`+kid+`","typ":"garam-message+jwt"}`, valid), key},
		{"no kid", signRaw(t, private, `{"alg":"RS256","typ":"JWT"}`, valid), key},
		{"an embedded jwk", signRaw(t, private, `{"alg":"RS256","kid":"`+kid+`","typ":"JWT","jwk":{"kty":"RSA"}}`, valid), key},
		{"a jku", signRaw(t, private, `{"alg":"RS256","kid":"`+kid+`","typ":"JWT","jku":"https://evil.test"}`, valid), key},
		{"an x5c", signRaw(t, private, `{"alg":"RS256","kid":"`+kid+`","typ":"JWT","x5c":["AAAA"]}`, valid), key},
		{"a critical header", signRaw(t, private, `{"alg":"RS256","kid":"`+kid+`","typ":"JWT","crit":["exp"]}`, valid), key},
		{"a repeated header member", signRaw(t, private, `{"alg":"RS256","kid":"`+kid+`","typ":"JWT","alg":"RS256"}`, valid), key},
		{"a repeated claim", signRaw(t, private, header, strings.TrimSuffix(valid, "}")+`,"sub":"`+client.String()+`"}`), key},
		{"a repeated member in a nested claim", signRaw(t, private, header,
			strings.TrimSuffix(valid, "}")+`,"extra":{"a":1,"a":2}}`), key},
		{"a payload that is not an object", signRaw(t, private, header, `["not","an","object"]`), key},
		{"trailing data after the payload", signRaw(t, private, header, valid+`{}`), key},
		{"a signature by another key", signRaw(t, other, header, valid), key},
		{"a key of another operator", signRaw(t, private, header, valid), recorded(t, stranger, private)},
		{"a kid naming another key", signRaw(t, private, headerFor(t, other), valid), key},
		{"iss another operator", signRaw(t, private, header, with(func(c map[string]any) { c["iss"] = stranger.String() })), key},
		{"sub another operator", signRaw(t, private, header, with(func(c map[string]any) { c["sub"] = stranger.String() })), key},
		{"aud an array", signRaw(t, private, header, with(func(c map[string]any) { c["aud"] = []string{endpoint} })), key},
		{"aud the origin alone", signRaw(t, private, header, with(func(c map[string]any) { c["aud"] = "https://machine.garam.test" })), key},
		{"aud with a trailing slash", signRaw(t, private, header, with(func(c map[string]any) { c["aud"] = endpoint + "/" })), key},
		{"no exp", signRaw(t, private, header, with(func(c map[string]any) { delete(c, "exp") })), key},
		{"a lifetime past 60 seconds", signRaw(t, private, header, with(func(c map[string]any) { c["exp"] = now.Add(61 * time.Second).Unix() })), key},
		{"exp before iat", signRaw(t, private, header, with(func(c map[string]any) { c["exp"] = now.Add(-time.Second).Unix() })), key},
		{"a fractional iat", signRaw(t, private, header, strings.Replace(valid, `"iat":`+strconv.FormatInt(now.Unix(), 10), `"iat":`+strconv.FormatInt(now.Unix(), 10)+".5", 1)), key},
		{"a string exp", signRaw(t, private, header, with(func(c map[string]any) { c["exp"] = strconv.FormatInt(now.Add(time.Minute).Unix(), 10) })), key},
		{"nbf after exp", signRaw(t, private, header, with(func(c map[string]any) { c["nbf"] = now.Add(2 * time.Minute).Unix() })), key},
		{"no jti", signRaw(t, private, header, with(func(c map[string]any) { delete(c, "jti") })), key},
		{"a jti under 128 bits", signRaw(t, private, header, with(func(c map[string]any) { c["jti"] = "short" })), key},
		{"a jti past 128 characters", signRaw(t, private, header, with(func(c map[string]any) { c["jti"] = strings.Repeat("a", 129) })), key},
		{"a jti outside base64url", signRaw(t, private, header, with(func(c map[string]any) { c["jti"] = strings.Repeat("a/", 12) })), key},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := verify(tc.compact, tc.key)
			assert.ErrorIs(t, err, operator.ErrClientInvalid)
		})
	}

	for name, compact := range map[string]string{
		"whitespace inside": strings.Replace(signRaw(t, private, header, valid), ".", ". ", 1),
		"padding":           signRaw(t, private, header, valid) + "=",
		"two segments":      b64(header) + "." + b64(valid),
		"past the bound":    strings.Repeat("a", operator.MaxAssertionLength) + ".b.c",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := operator.ParseClientAssertion(compact)
			assert.ErrorIs(t, err, operator.ErrClientInvalid)
		})
	}
}

// TestVerifiedAssertion_TimelyAllowsThirtySecondsOfSkew holds the time
// bounds at the database's clock (ADR-0102): each date just inside the
// skew is accepted, the same date a second past it is refused.
func TestVerifiedAssertion_TimelyAllowsThirtySecondsOfSkew(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	at := func(iat, exp time.Duration, nbf *time.Duration) operator.VerifiedAssertion {
		v := operator.VerifiedAssertion{IssuedAt: now.Add(iat), ExpiresAt: now.Add(exp)}
		if nbf != nil {
			n := now.Add(*nbf)
			v.NotBefore = &n
		}
		return v
	}
	skew := operator.AssertionClockSkew
	inside, past := skew, skew+time.Second
	tests := []struct {
		name   string
		v      operator.VerifiedAssertion
		timely bool
	}{
		{"issued now", at(0, time.Minute, nil), true},
		{"issued at the skew ahead", at(inside, inside+time.Minute, nil), true},
		{"issued past the skew ahead", at(past, past+time.Minute, nil), false},
		{"expired within the skew", at(-time.Minute, -skew+time.Second, nil), true},
		{"expired at the skew", at(-time.Minute, -skew, nil), false},
		{"nbf at the skew ahead", at(0, time.Minute, &inside), true},
		{"nbf past the skew ahead", at(0, time.Minute, &past), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.timely, tc.v.Timely(now))
		})
	}
	v := at(0, time.Minute, nil)
	assert.Equal(t, now.Add(time.Minute+skew), v.ReplayUntil(), "a jti is kept until exp plus the skew")
}

// TestParseTokenEndpoint_TakesAnAbsoluteHTTPSURLAsWritten holds what
// GARAM_MACHINE_TOKEN_URL may be (ADR-0102): empty, or an absolute
// https URL with no user information, query or fragment, answered byte
// for byte as written.
func TestParseTokenEndpoint_TakesAnAbsoluteHTTPSURLAsWritten(t *testing.T) {
	got, err := operator.ParseTokenEndpoint(endpoint)
	require.NoError(t, err)
	assert.Equal(t, endpoint, got)
	got, err = operator.ParseTokenEndpoint("")
	require.NoError(t, err)
	assert.Empty(t, got, "unset serves no exchange")

	for _, raw := range []string{
		"http://machine.garam.test/oauth/token",
		"/oauth/token",
		"https:///oauth/token",
		"https://user@machine.garam.test/oauth/token",
		"https://machine.garam.test/oauth/token?x=1",
		"https://machine.garam.test/oauth/token?",
		"https://machine.garam.test/oauth/token#frag",
		"https://machine.garam.test/oauth/token#",
	} {
		_, err := operator.ParseTokenEndpoint(raw)
		assert.Error(t, err, raw)
	}
}
