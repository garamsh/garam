package integration_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/message/transit"
)

// jwtAuth is a JWT auth method of the test's own on the dev OpenBao,
// trusting one key the test holds, with a role that grants a signing
// key's policy for a short lease. It stands in for gaon's issuer: gaon
// does not yet serve the discovery document OpenBao would read, so the
// method is given the public key directly (ADR-0152).
type jwtAuth struct {
	mount string
	role  string
	key   *ecdsa.PrivateKey
}

const (
	identityIssuer   = "https://gaon.test"
	identityAudience = "garam-openbao"
	// leaseSeconds is the role's token lease: short enough that a test
	// reaches renewal and then the first lease's end in seconds.
	leaseSeconds = 3
)

func newJWTAuth(t *testing.T, b *openBao, signing signingKey) jwtAuth {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	mount := "jwt-" + strings.TrimPrefix(signing.mount, "garam-")
	b.call(t, b.root, http.MethodPost, "sys/auth/"+mount, map[string]string{"type": "jwt"})
	t.Cleanup(func() { b.call(t, b.root, http.MethodDelete, "sys/auth/"+mount, nil) })
	b.call(t, b.root, http.MethodPost, "auth/"+mount+"/config", map[string]any{
		"jwt_validation_pubkeys": []string{string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))},
		"bound_issuer":           identityIssuer,
	})
	b.call(t, b.root, http.MethodPost, "auth/"+mount+"/role/garam", map[string]any{
		"role_type":       "jwt",
		"bound_audiences": []string{identityAudience},
		"user_claim":      "sub",
		// The default policy is what lets a token renew itself; a role
		// without it makes every lease end in a login (ADR-0152).
		"token_policies": []string{signing.mount, "default"},
		"token_ttl":      leaseSeconds,
		"token_max_ttl":  "5m",
	})
	return jwtAuth{mount: mount, role: "garam", key: key}
}

// identityFile writes an ES256 workload identity token naming audience,
// owner-only, as gaon's would be written.
func (a jwtAuth) identityFile(t *testing.T, audience string) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: a.key}, nil)
	require.NoError(t, err)
	now := time.Now()
	claims, err := json.Marshal(map[string]any{
		"iss": identityIssuer, "sub": "gaon:serviceaccount:garam", "aud": []string{audience},
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	signed, err := signer.Sign(claims)
	require.NoError(t, err)
	compact, err := signed.CompactSerialize()
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "identity")
	require.NoError(t, os.WriteFile(path, []byte(compact), 0o600))
	return path
}

// openBaoTap forwards to the dev OpenBao over TLS and records the paths
// the custodian calls, and the token a login answered, so a test sees
// which exchange happened and can revoke what was granted. It is served
// under its own certificate, which the custodian's CA file names.
type openBaoTap struct {
	address string
	caFile  string

	mu      sync.Mutex
	paths   []string
	granted string
}

func newOpenBaoTap(t *testing.T, b *openBao) *openBaoTap {
	t.Helper()
	target, err := url.Parse(b.addr)
	require.NoError(t, err)
	tap := &openBaoTap{}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = b.client.Transport
	proxy.ModifyResponse = func(resp *http.Response) error {
		if !strings.HasSuffix(resp.Request.URL.Path, "/login") {
			return nil
		}
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		var answer struct {
			Auth struct {
				ClientToken string `json:"client_token"`
			} `json:"auth"`
		}
		if json.Unmarshal(raw, &answer) == nil && answer.Auth.ClientToken != "" {
			tap.mu.Lock()
			tap.granted = answer.Auth.ClientToken
			tap.mu.Unlock()
		}
		return nil
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tap.mu.Lock()
		tap.paths = append(tap.paths, r.URL.Path)
		tap.mu.Unlock()
		r.Host = target.Host
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	tap.address = srv.URL
	tap.caFile = filepath.Join(t.TempDir(), "tap-ca.pem")
	require.NoError(t, os.WriteFile(tap.caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	return tap
}

// count is how many calls reached path.
func (p *openBaoTap) count(path string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, seen := range p.paths {
		if seen == path {
			n++
		}
	}
	return n
}

func (p *openBaoTap) token() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.granted
}

// custodianLoggingIn answers the custodian garam runs for signing,
// logging in to auth with identityFile through the tap.
func (k signingKey) custodianLoggingIn(t *testing.T, tap *openBaoTap, auth jwtAuth, identityFile string) message.Custodian {
	t.Helper()
	c, err := transit.New(transit.Settings{
		Address: tap.address, CAFile: tap.caFile, IdentityFile: identityFile,
		AuthMount: auth.mount, AuthRole: auth.role, Mount: k.mount, Key: "messages",
	})
	require.NoError(t, err)
	return c
}

// signOnce makes a version of the Transit key and signs with it: the
// two calls garam makes.
func signOnce(ctx context.Context, c message.Custodian) error {
	held, err := c.Create(ctx)
	if err != nil {
		return err
	}
	signer, err := c.Open(ctx, &message.SigningKey{ID: "identity-test", PublicKey: held.PublicKey, Held: held.Held})
	if err != nil {
		return err
	}
	_, err = signer.Sign(ctx, []byte("input"))
	return err
}

// TestTransitSigning_AnIdentityFileLogsInRenewsAndLogsInAgain holds
// ADR-0152 against a real OpenBao: garam logs in with the identity
// file and signs, renews the token it was given before its lease ends
// so the same token signs past the first lease, and logs in again once
// the token is revoked.
func TestTransitSigning_AnIdentityFileLogsInRenewsAndLogsInAgain(t *testing.T) {
	b := newOpenBao(t)
	signing := newSigningKey(t, b)
	auth := newJWTAuth(t, b, signing)
	tap := newOpenBaoTap(t, b)
	login := "/v1/auth/" + auth.mount + "/login"
	renew := "/v1/auth/token/renew-self"
	c := signing.custodianLoggingIn(t, tap, auth, auth.identityFile(t, identityAudience))
	ctx := context.Background()

	require.NoError(t, signOnce(ctx, c), "a login, then a signing call under the token it gave")
	assert.Equal(t, 1, tap.count(login))
	first := tap.token()
	require.NotEmpty(t, first)
	assert.Equal(t, 0, tap.count(renew), "a token well inside its lease is not renewed")

	time.Sleep(leaseSeconds*time.Second*2/3 + 300*time.Millisecond)
	require.NoError(t, signOnce(ctx, c))
	assert.Equal(t, 1, tap.count(renew), "past two thirds of the lease the call renews")

	time.Sleep(time.Second) // now past the first lease, which only the renewal carried on
	require.NoError(t, signOnce(ctx, c), "the renewed token signs past the lease it was granted")
	assert.Equal(t, 1, tap.count(login), "the same token served throughout")
	assert.Equal(t, first, tap.token())

	b.call(t, b.root, http.MethodPost, "auth/token/revoke", map[string]string{"token": first})
	require.NoError(t, signOnce(ctx, c), "a revoked token is replaced by a login, not an error")
	assert.Equal(t, 2, tap.count(login))
	assert.NotEqual(t, first, tap.token(), "the second login granted a different token")
}

// TestTransitSigning_AnIdentityOpenBaoDoesNotAcceptSignsNothing is the
// control the success above is read against: the same flow with an
// audience the role does not bind, and with a role OpenBao does not
// have, never reaches a signing call.
func TestTransitSigning_AnIdentityOpenBaoDoesNotAcceptSignsNothing(t *testing.T) {
	b := newOpenBao(t)
	signing := newSigningKey(t, b)
	auth := newJWTAuth(t, b, signing)
	tap := newOpenBaoTap(t, b)
	ctx := context.Background()

	wrongAudience := signing.custodianLoggingIn(t, tap, auth, auth.identityFile(t, "someone-else"))
	err := signOnce(ctx, wrongAudience)
	require.Error(t, err)
	assert.Zero(t, tap.count("/v1/"+signing.mount+"/keys/messages/rotate"), "no signing call followed the refused login")

	unknownRole := auth
	unknownRole.role = "no-such-role"
	err = signOnce(ctx, signing.custodianLoggingIn(t, tap, unknownRole, auth.identityFile(t, identityAudience)))
	require.Error(t, err)
	assert.Zero(t, tap.count("/v1/"+signing.mount+"/keys/messages/rotate"))
}

// TestTransitSigning_AnIdentityFileGaramCannotReadRefusesTheStart holds
// the start's refusal: an identity file that is absent, or that others
// can read, is refused by New before any call, where the readable one
// beside them is accepted.
func TestTransitSigning_AnIdentityFileGaramCannotReadRefusesTheStart(t *testing.T) {
	b := newOpenBao(t)
	signing := newSigningKey(t, b)
	auth := newJWTAuth(t, b, signing)
	good := auth.identityFile(t, identityAudience)
	open := filepath.Join(t.TempDir(), "open-identity")
	raw, err := os.ReadFile(good)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(open, raw, 0o600))
	require.NoError(t, os.Chmod(open, 0o644))

	settings := func(identity string) transit.Settings {
		return transit.Settings{
			Address: b.addr, CAFile: b.caFile, IdentityFile: identity,
			AuthMount: auth.mount, AuthRole: auth.role, Mount: signing.mount, Key: "messages",
		}
	}
	_, err = transit.New(settings(good))
	require.NoError(t, err, "the control: an owner-only identity file starts")
	_, err = transit.New(settings(filepath.Join(filepath.Dir(good), "absent")))
	require.Error(t, err)
	_, err = transit.New(settings(open))
	require.Error(t, err)
}
