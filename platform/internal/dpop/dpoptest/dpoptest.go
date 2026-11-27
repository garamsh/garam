// Package dpoptest signs DPoP proofs (RFC 9449) as a bot's process
// does, for tests of what verifies them (ADR-0191). No binary imports
// it.
package dpoptest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/garamsh/garam/platform/internal/dpop"
)

// Key is a bot's process's ES256 key.
type Key struct {
	private *ecdsa.PrivateKey
}

// NewKey makes a fresh P-256 key.
func NewKey(t testing.TB) *Key {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &Key{private: private}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// PublicJWK is the key's public JWK, as a proof's header carries it.
func (k *Key) PublicJWK(t testing.TB) map[string]any {
	t.Helper()
	raw, err := k.private.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"kty": "EC", "crv": "P-256", "x": b64(raw[1:33]), "y": b64(raw[33:])}
}

// PrivateJWK is the key's JWK with its private member, which a proof's
// header must never carry.
func (k *Key) PrivateJWK(t testing.TB) map[string]any {
	t.Helper()
	d, err := k.private.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	jwk := k.PublicJWK(t)
	jwk["d"] = b64(d)
	return jwk
}

// Thumbprint is the key's RFC 7638 thumbprint, the cnf.jkt of a token
// bound to it.
func (k *Key) Thumbprint(t testing.TB) string {
	t.Helper()
	jkt, err := dpop.Thumbprint(&k.private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return jkt
}

// Header is a well-made proof's JOSE header for the key.
func (k *Key) Header(t testing.TB) map[string]any {
	t.Helper()
	return map[string]any{"typ": dpop.ProofType, "alg": "ES256", "jwk": k.PublicJWK(t)}
}

// Claims are a well-made proof's claims for method at htu, issued at
// at, with a fresh jti.
func Claims(method, htu string, at time.Time) map[string]any {
	return map[string]any{"htm": method, "htu": htu, "iat": at.Unix(), "jti": rand.Text()}
}

// Sign renders header and claims as a compact JWS signed by the key
// with ES256, whatever the header says; a test bends either map to
// make a proof garam refuses.
func (k *Key) Sign(t testing.TB, header, claims map[string]any) string {
	t.Helper()
	h, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	c, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	input := b64(h) + "." + b64(c)
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, k.private, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return input + "." + b64(signature)
}

// Proof is a well-made proof for method at htu, now, carrying nonce
// and the ath of token where either is not empty.
func (k *Key) Proof(t testing.TB, method, htu, nonce, token string) string {
	t.Helper()
	claims := Claims(method, htu, time.Now())
	if nonce != "" {
		claims["nonce"] = nonce
	}
	if token != "" {
		claims["ath"] = dpop.TokenHash(token)
	}
	return k.Sign(t, k.Header(t), claims)
}

// Store is a [dpop.Store] in memory, read against now. It counts what
// it holds, so a test can see what a request wrote.
type Store struct {
	now    func() time.Time
	mu     sync.Mutex
	jtis   map[string]time.Time
	nonces map[int64]string
}

var _ dpop.Store = (*Store)(nil)

// NewStore makes an empty Store; a nil now is time.Now.
func NewStore(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{now: now, jtis: map[string]time.Time{}, nonces: map[int64]string{}}
}

// SpendJTI records jti for jkt unless it is spent and unexpired, and
// sweeps the expired.
func (s *Store) SpendJTI(_ context.Context, jkt, jti string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, until := range s.jtis {
		if !until.After(s.now()) {
			delete(s.jtis, k)
		}
	}
	key := jkt + " " + jti
	if _, ok := s.jtis[key]; ok {
		return false, nil
	}
	s.jtis[key] = s.now().Add(ttl)
	return true, nil
}

func (s *Store) periodOf(period time.Duration) int64 {
	return s.now().Unix() / int64(period/time.Second)
}

// CurrentNonce answers the current period's nonce, keeping candidate
// where it has none and sweeping the periods before the previous.
func (s *Store) CurrentNonce(_ context.Context, candidate string, period time.Duration) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.periodOf(period)
	if nonce, ok := s.nonces[current]; ok {
		return nonce, nil
	}
	for p := range s.nonces {
		if p < current-1 {
			delete(s.nonces, p)
		}
	}
	s.nonces[current] = candidate
	return candidate, nil
}

// NonceLive answers whether nonce is the current or previous period's.
func (s *Store) NonceLive(_ context.Context, nonce string, period time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if nonce == "" {
		return false, nil
	}
	current := s.periodOf(period)
	return s.nonces[current] == nonce || s.nonces[current-1] == nonce, nil
}

// Held answers how many jtis and nonces the store holds.
func (s *Store) Held() (jtis, nonces int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.jtis), len(s.nonces)
}
