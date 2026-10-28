package authn_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/authn"
	"github.com/garamsh/garam/platform/internal/errorx"
)

// fakeIssuer is a minimal OIDC issuer: discovery, JWKS, and a token
// endpoint that returns a signed ID token.
type fakeIssuer struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	claims map[string]any
	// tamper breaks the ID token signature before returning it.
	tamper bool
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	f := &fakeIssuer{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"issuer":                                f.srv.URL,
			"authorization_endpoint":                f.srv.URL + "/auth",
			"token_endpoint":                        f.srv.URL + "/token",
			"jwks_uri":                              f.srv.URL + "/keys",
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		writeTestJSON(t, w, map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"kid": "test",
				"use": "sig",
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(key.PublicKey.N.Bytes()),
				"e":   "AQAB",
			}},
		})
	})
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		require.NotEmpty(t, r.Form.Get("code"), "authorization code")
		require.NotEmpty(t, r.Form.Get("code_verifier"), "PKCE verifier")
		writeTestJSON(t, w, map[string]any{
			"access_token": "at",
			"token_type":   "Bearer",
			"id_token":     f.idToken(t),
		})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// idToken signs the configured claims as a RS256 JWT.
func (f *fakeIssuer) idToken(t *testing.T) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","kid":"test","typ":"JWT"}`))
	payload, err := json.Marshal(f.claims)
	require.NoError(t, err)
	body := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(body))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
	require.NoError(t, err)
	if f.tamper {
		sig[0] ^= 0xff
	}
	return body + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(v))
}

func TestGoogleProvider_Exchange(t *testing.T) {
	issuer := newFakeIssuer(t)
	ctx := context.Background()
	p, err := authn.NewGoogleProvider(ctx, authn.GoogleConfig{
		ClientID:     "client-id",
		ClientSecret: "secret",
		RedirectURL:  "http://localhost:8080/auth/callback/google",
		Issuer:       issuer.srv.URL,
	})
	require.NoError(t, err)
	assert.Equal(t, "google", p.Name())

	issuer.claims = map[string]any{
		"iss":   issuer.srv.URL,
		"sub":   "117421510527410544321",
		"aud":   "client-id",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
		"name":  "Ada Lovelace",
		"email": "ada@example.com",
	}
	identity, err := p.Exchange(ctx, "code", "verifier")
	require.NoError(t, err)
	assert.Equal(t, "117421510527410544321", identity.Subject)
	assert.Equal(t, "Ada Lovelace", identity.Nickname)
}

func TestGoogleProvider_ExchangeBadSignature(t *testing.T) {
	issuer := newFakeIssuer(t)
	issuer.tamper = true
	ctx := context.Background()
	p, err := authn.NewGoogleProvider(ctx, authn.GoogleConfig{
		ClientID: "client-id",
		Issuer:   issuer.srv.URL,
	})
	require.NoError(t, err)
	issuer.claims = map[string]any{
		"iss": issuer.srv.URL,
		"sub": "1",
		"aud": "client-id",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	_, err = p.Exchange(ctx, "code", "verifier")
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), fmt.Sprintf("kind: %v", err))
}

func TestGoogleProvider_AuthCodeURL(t *testing.T) {
	issuer := newFakeIssuer(t)
	p, err := authn.NewGoogleProvider(context.Background(), authn.GoogleConfig{
		ClientID:    "client-id",
		RedirectURL: "http://localhost:8080/auth/callback/google",
		Issuer:      issuer.srv.URL,
	})
	require.NoError(t, err)
	url := p.AuthCodeURL("state-1", "verifier-1")
	assert.Contains(t, url, "state=state-1")
	assert.Contains(t, url, "code_challenge=")
	assert.Contains(t, url, "code_challenge_method=S256")
	assert.Contains(t, url, "client_id=client-id")
}

func TestNewGoogleProvider_DiscoveryFailure(t *testing.T) {
	_, err := authn.NewGoogleProvider(context.Background(), authn.GoogleConfig{
		ClientID: "client-id",
		Issuer:   "http://127.0.0.1:1",
	})
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
}
