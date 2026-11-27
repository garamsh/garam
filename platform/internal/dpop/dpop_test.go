package dpop_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/garamsh/garam/platform/internal/dpop"
	"github.com/garamsh/garam/platform/internal/dpop/dpoptest"
)

const tokenURL = "https://machine.garam.test/oauth/token"

// clock is the test's time, which a test moves.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }

func fixture(t *testing.T) (*dpop.Verifier, *clock) {
	t.Helper()
	c := &clock{at: time.Unix(1_800_000_000, 0)}
	return dpop.NewVerifier(dpoptest.NewStore(c.now), c.now), c
}

func newKey(t *testing.T) *dpoptest.Key { return dpoptest.NewKey(t) }

func sign(t *testing.T, key *dpoptest.Key, header, claims map[string]any) string {
	t.Helper()
	return key.Sign(t, header, claims)
}

func proofHeader(t *testing.T, key *dpoptest.Key) map[string]any {
	t.Helper()
	return key.Header(t)
}

// proofClaims are a well-made proof's claims for method at htu, at
// c's time, with a fresh jti.
func proofClaims(c *clock, method, htu string) map[string]any {
	return dpoptest.Claims(method, htu, c.at)
}

func thumbprint(t *testing.T, key *dpoptest.Key) string {
	t.Helper()
	return key.Thumbprint(t)
}

func nonce(t *testing.T, v *dpop.Verifier) string {
	t.Helper()
	n, err := v.Nonce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// The token endpoint's round trip (RFC 9449 §8): a proof without a
// nonce, or with one garam did not issue or that expired, is answered
// use_dpop_nonce, and the same key's proof with the nonce garam
// answered is accepted and names the key's thumbprint.
func TestVerify_TheNonceRoundTrip(t *testing.T) {
	v, c := fixture(t)
	key := newKey(t)
	endpoint := dpop.Request{Method: http.MethodPost, URL: tokenURL, Nonce: true}

	bare := sign(t, key, proofHeader(t, key), proofClaims(c, http.MethodPost, tokenURL))
	if _, err := v.Verify(context.Background(), bare, endpoint); !errors.Is(err, dpop.ErrNonceRequired) {
		t.Fatalf("a proof without a nonce: %v, want ErrNonceRequired", err)
	}
	forged := proofClaims(c, http.MethodPost, tokenURL)
	forged["nonce"] = "not-garams"
	if _, err := v.Verify(context.Background(), sign(t, key, proofHeader(t, key), forged), endpoint); !errors.Is(err, dpop.ErrNonceRequired) {
		t.Fatalf("a nonce garam did not issue: %v, want ErrNonceRequired", err)
	}

	issued := nonce(t, v)
	retried := proofClaims(c, http.MethodPost, tokenURL)
	retried["nonce"] = issued
	jkt, err := v.Verify(context.Background(), sign(t, key, proofHeader(t, key), retried), endpoint)
	if err != nil {
		t.Fatalf("the retry with garam's nonce: %v", err)
	}
	if jkt != thumbprint(t, key) {
		t.Fatalf("jkt %q, want the key's thumbprint %q", jkt, thumbprint(t, key))
	}

	c.at = c.at.Add(dpop.NonceTTL)
	late := proofClaims(c, http.MethodPost, tokenURL)
	late["nonce"] = issued
	if _, err := v.Verify(context.Background(), sign(t, key, proofHeader(t, key), late), endpoint); !errors.Is(err, dpop.ErrNonceRequired) {
		t.Fatalf("an expired nonce: %v, want ErrNonceRequired", err)
	}
}

// A proof is used once: the same proof sent again is refused, though
// everything else about it still holds.
func TestVerify_RefusesAReplayedProof(t *testing.T) {
	v, c := fixture(t)
	key := newKey(t)
	claims := proofClaims(c, http.MethodPost, tokenURL)
	claims["nonce"] = nonce(t, v)
	proof := sign(t, key, proofHeader(t, key), claims)
	req := dpop.Request{Method: http.MethodPost, URL: tokenURL, Nonce: true}
	if _, err := v.Verify(context.Background(), proof, req); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if _, err := v.Verify(context.Background(), proof, req); !errors.Is(err, dpop.ErrProofInvalid) {
		t.Fatalf("replay: %v, want ErrProofInvalid", err)
	}
}

// A proof names the one request it was made for: another method, or
// another URL, is refused; the query and the case of the scheme and
// host are not what names it.
func TestVerify_RefusesAWrongHTMOrHTU(t *testing.T) {
	v, c := fixture(t)
	key := newKey(t)
	req := dpop.Request{Method: http.MethodPost, URL: tokenURL}
	for name, claims := range map[string]map[string]any{
		"htm":                  proofClaims(c, http.MethodGet, tokenURL),
		"htu path":             proofClaims(c, http.MethodPost, "https://machine.garam.test/oauth/introspect"),
		"htu host":             proofClaims(c, http.MethodPost, "https://other.garam.test/oauth/token"),
		"htu scheme":           proofClaims(c, http.MethodPost, "http://machine.garam.test/oauth/token"),
		"htu relative":         proofClaims(c, http.MethodPost, "/oauth/token"),
		"htu trailing slash":   proofClaims(c, http.MethodPost, tokenURL+"/"),
		"method case is exact": proofClaims(c, "post", tokenURL),
	} {
		if _, err := v.Verify(context.Background(), sign(t, key, proofHeader(t, key), claims), req); !errors.Is(err, dpop.ErrProofInvalid) {
			t.Errorf("%s: %v, want ErrProofInvalid", name, err)
		}
	}
	for name, htu := range map[string]string{
		"query and fragment": tokenURL + "?a=b#c",
		"scheme and host":    "HTTPS://Machine.Garam.Test/oauth/token",
	} {
		if _, err := v.Verify(context.Background(), sign(t, key, proofHeader(t, key), proofClaims(c, http.MethodPost, htu)), req); err != nil {
			t.Errorf("%s: %v, want accepted", name, err)
		}
	}
}

// A proof is refused when its iat is outside the window, when it
// carries no jti, or when its header is not a DPoP proof's: another
// typ, another alg, no key, or a key with its private part.
func TestVerify_RefusesAMalformedProof(t *testing.T) {
	v, c := fixture(t)
	key := newKey(t)
	req := dpop.Request{Method: http.MethodPost, URL: tokenURL}
	stale := proofClaims(c, http.MethodPost, tokenURL)
	stale["iat"] = c.at.Add(-dpop.Window - time.Second).Unix()
	early := proofClaims(c, http.MethodPost, tokenURL)
	early["iat"] = c.at.Add(dpop.Window + time.Second).Unix()
	noJTI := proofClaims(c, http.MethodPost, tokenURL)
	delete(noJTI, "jti")
	noIAT := proofClaims(c, http.MethodPost, tokenURL)
	delete(noIAT, "iat")
	typ := proofHeader(t, key)
	typ["typ"] = "JWT"
	alg := proofHeader(t, key)
	alg["alg"] = "ES384"
	noKey := proofHeader(t, key)
	delete(noKey, "jwk")
	private := proofHeader(t, key)
	private["jwk"] = key.PrivateJWK(t)
	other := newKey(t)
	foreign := proofHeader(t, other)

	for name, proof := range map[string]string{
		"stale iat":       sign(t, key, proofHeader(t, key), stale),
		"early iat":       sign(t, key, proofHeader(t, key), early),
		"no jti":          sign(t, key, proofHeader(t, key), noJTI),
		"no iat":          sign(t, key, proofHeader(t, key), noIAT),
		"typ":             sign(t, key, typ, proofClaims(c, http.MethodPost, tokenURL)),
		"alg":             sign(t, key, alg, proofClaims(c, http.MethodPost, tokenURL)),
		"no jwk":          sign(t, key, noKey, proofClaims(c, http.MethodPost, tokenURL)),
		"private jwk":     sign(t, key, private, proofClaims(c, http.MethodPost, tokenURL)),
		"signed by other": sign(t, key, foreign, proofClaims(c, http.MethodPost, tokenURL)),
		"not a JWS":       "a.b",
		"empty":           "",
	} {
		if _, err := v.Verify(context.Background(), proof, req); !errors.Is(err, dpop.ErrProofInvalid) {
			t.Errorf("%s: %v, want ErrProofInvalid", name, err)
		}
	}
}

const token = "garam_bt_stolen"

// resource is a resource server's request presenting a token under
// scheme with proof, if any.
func resource(scheme, proof string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "https://machine.garam.test/bots/self?x=1", nil)
	r.Header.Set("Authorization", scheme+" "+token)
	if proof != "" {
		r.Header.Set(dpop.Header, proof)
	}
	return r
}

const served = "https://machine.garam.test/bots/self"

func boundClaims(c *clock) map[string]any {
	claims := proofClaims(c, http.MethodGet, served)
	claims["ath"] = dpop.TokenHash(token)
	return claims
}

// A resource server accepts a bound token presented as DPoP with a
// proof by the key it is bound to, for its request and its token, and
// asks for no nonce.
func TestBound_AcceptsTheBoundKeysProof(t *testing.T) {
	v, c := fixture(t)
	key := newKey(t)
	r := resource("DPoP", sign(t, key, proofHeader(t, key), boundClaims(c)))
	presented, err := dpop.Presented(r.Header.Get("Authorization"))
	if err != nil || presented != token {
		t.Fatalf("Presented = %q, %v", presented, err)
	}
	if err := v.Bound(context.Background(), r, served, presented, thumbprint(t, key)); err != nil {
		t.Fatalf("Bound: %v", err)
	}
}

// A stolen token is useless without its key: the thief's own key's
// proof, however well made, is refused, as is the token with no proof
// at all.
func TestBound_RefusesAStolenTokenWithoutTheKey(t *testing.T) {
	v, c := fixture(t)
	bound, thief := newKey(t), newKey(t)
	r := resource("DPoP", sign(t, thief, proofHeader(t, thief), boundClaims(c)))
	err := v.Bound(context.Background(), r, served, token, thumbprint(t, bound))
	if !errors.Is(err, dpop.ErrProofInvalid) {
		t.Fatalf("the thief's key: %v, want ErrProofInvalid", err)
	}
	if got := dpop.Challenge(err); got != `DPoP algs="ES256", error="invalid_dpop_proof"` {
		t.Fatalf("challenge %q", got)
	}
	if err := v.Bound(context.Background(), resource("DPoP", ""), served, token, thumbprint(t, bound)); !errors.Is(err, dpop.ErrProofInvalid) {
		t.Fatalf("no proof: %v, want ErrProofInvalid", err)
	}
}

// A proof whose ath is another token's, or that carries none, does
// not present this token.
func TestBound_RefusesAProofForAnotherToken(t *testing.T) {
	v, c := fixture(t)
	key := newKey(t)
	other := boundClaims(c)
	other["ath"] = dpop.TokenHash("garam_bt_other")
	none := boundClaims(c)
	delete(none, "ath")
	for name, claims := range map[string]map[string]any{"another ath": other, "no ath": none} {
		r := resource("DPoP", sign(t, key, proofHeader(t, key), claims))
		if err := v.Bound(context.Background(), r, served, token, thumbprint(t, key)); !errors.Is(err, dpop.ErrProofInvalid) {
			t.Errorf("%s: %v, want ErrProofInvalid", name, err)
		}
	}
}

// A bound token presented as a bearer token is refused before any
// proof is read, whatever the request carries beside it.
func TestPresented_RefusesBearer(t *testing.T) {
	for _, authorization := range []string{"Bearer " + token, "bearer " + token, token, "DPoP ", ""} {
		_, err := dpop.Presented(authorization)
		if !errors.Is(err, dpop.ErrSchemeRefused) {
			t.Errorf("%q: %v, want ErrSchemeRefused", authorization, err)
		}
		if got := dpop.Challenge(err); got != `DPoP algs="ES256", error="invalid_token"` {
			t.Errorf("%q: challenge %q", authorization, got)
		}
	}
}

// A request carries one proof: two DPoP headers are refused.
func TestProof_RefusesTwoProofs(t *testing.T) {
	h := http.Header{}
	h.Add(dpop.Header, "a")
	h.Add(dpop.Header, "b")
	if _, err := dpop.Proof(h); !errors.Is(err, dpop.ErrProofInvalid) {
		t.Fatalf("two proofs: %v, want ErrProofInvalid", err)
	}
}
