package transit_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/message/transit"
)

// The real OpenBao is what tests/integration logs in to; these hold
// the exchange's edges a real server cannot be made to take on demand:
// a renewal that cannot be answered, a lease the test moves past.

const identity = "eyJ.test-identity"

// fakeBao answers JWT logins and renewals, and holds a token valid
// until it is revoked. Everything else goes to the Transit fake, once
// the token is valid.
type fakeBao struct {
	fakeTransit

	mu          sync.Mutex
	lease       int
	logins      []map[string]string
	renewed     []string
	valid       map[string]bool
	denyLogin   bool
	renewStatus int
	forbidAll   bool
}

func (f *fakeBao) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/v1/auth/jwt/login", "/v1/auth/gaon/jwt/login":
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.logins = append(f.logins, req)
		if f.denyLogin {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"errors":["role not found"]}`))
			return
		}
		issued := "s.login-" + strconv.Itoa(len(f.logins))
		f.valid[issued] = true
		f.grant(w, issued)
	case "/v1/auth/token/renew-self":
		presented := r.Header.Get("X-Vault-Token")
		f.renewed = append(f.renewed, presented)
		if f.renewStatus != 0 {
			w.WriteHeader(f.renewStatus)
			_, _ = w.Write([]byte(`{"errors":["renewal refused"]}`))
			return
		}
		if !f.valid[presented] {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
			return
		}
		f.grant(w, presented)
	default:
		if f.forbidAll || !f.valid[r.Header.Get("X-Vault-Token")] {
			f.tokens = append(f.tokens, r.Header.Get("X-Vault-Token"))
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
			return
		}
		f.fakeTransit.ServeHTTP(w, r)
	}
}

func (f *fakeBao) grant(w http.ResponseWriter, token string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"auth": map[string]any{
		"client_token": token, "lease_duration": f.lease, "renewable": true,
	}})
}

// loginCustodian starts f over TLS and answers a custodian that logs in
// to it, with the identity file written owner-only and a clock the test
// moves.
func loginCustodian(t *testing.T, f *fakeBao, mount string) (*transit.Custodian, string, *time.Time) {
	t.Helper()
	f.keyType = "ecdsa-p256"
	f.lease = 100
	f.valid = map[string]bool{}
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	idFile := filepath.Join(dir, "identity")
	require.NoError(t, os.WriteFile(idFile, []byte(identity+"\n"), 0o600))
	c, err := transit.New(transit.Settings{
		Address: srv.URL, CAFile: caFile, IdentityFile: idFile, AuthMount: mount, AuthRole: "garam", Key: "garam",
	})
	require.NoError(t, err)
	clock := time.Now()
	require.True(t, transit.SetClock(c, func() time.Time { return clock }))
	return c, idFile, &clock
}

func TestLogin_LogsInOnceAndPresentsWhatItWasGiven(t *testing.T) {
	f := &fakeBao{}
	c, _, _ := loginCustodian(t, f, "")

	_, err := c.Create(context.Background())
	require.NoError(t, err)
	_, err = c.Create(context.Background())
	require.NoError(t, err)

	require.Len(t, f.logins, 1, "the second call reuses the token the first logged in for")
	assert.Equal(t, map[string]string{"role": "garam", "jwt": identity}, f.logins[0], "the identity is sent trimmed")
	assert.Equal(t, []string{"s.login-1", "s.login-1"}, f.tokens)
	assert.Empty(t, f.renewed, "a token well inside its lease is not renewed")
}

func TestLogin_LogsInAtTheMountItIsGiven(t *testing.T) {
	f := &fakeBao{}
	c, _, _ := loginCustodian(t, f, "gaon/jwt")

	_, err := c.Create(context.Background())
	require.NoError(t, err)
	assert.Len(t, f.logins, 1)
}

func TestLogin_RenewsBeforeTheLeaseLapses(t *testing.T) {
	f := &fakeBao{}
	c, _, clock := loginCustodian(t, f, "")
	_, err := c.Create(context.Background())
	require.NoError(t, err)

	*clock = clock.Add(70 * time.Second) // past two thirds of 100s
	_, err = c.Create(context.Background())
	require.NoError(t, err)

	assert.Equal(t, []string{"s.login-1"}, f.renewed)
	assert.Len(t, f.logins, 1, "a renewal that was granted needs no second login")
	assert.Equal(t, "s.login-1", f.tokens[len(f.tokens)-1])

	*clock = clock.Add(50 * time.Second) // past the first lease, inside the renewed one
	_, err = c.Create(context.Background())
	require.NoError(t, err)
	assert.Len(t, f.logins, 1, "the renewed lease holds past the first one")
}

func TestLogin_LogsInAgainWhereRenewalIsRefused(t *testing.T) {
	f := &fakeBao{}
	c, idFile, clock := loginCustodian(t, f, "")
	_, err := c.Create(context.Background())
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(idFile, []byte("eyJ.renewed-identity"), 0o600))
	f.renewStatus = http.StatusForbidden
	*clock = clock.Add(70 * time.Second)
	_, err = c.Create(context.Background())
	require.NoError(t, err)

	require.Len(t, f.logins, 2)
	assert.Equal(t, "eyJ.renewed-identity", f.logins[1]["jwt"], "the identity renewed in place is the one presented")
	assert.Equal(t, "s.login-2", f.tokens[len(f.tokens)-1])
}

func TestLogin_KeepsATokenARenewalCouldNotBeAnsweredFor(t *testing.T) {
	f := &fakeBao{}
	c, _, clock := loginCustodian(t, f, "")
	_, err := c.Create(context.Background())
	require.NoError(t, err)

	f.renewStatus = http.StatusServiceUnavailable
	*clock = clock.Add(70 * time.Second)
	_, err = c.Create(context.Background())
	require.NoError(t, err, "an OpenBao that is down for a moment does not end a token that has not lapsed")
	assert.Len(t, f.logins, 1)

	*clock = clock.Add(40 * time.Second) // past the lease
	_, err = c.Create(context.Background())
	require.NoError(t, err)
	assert.Len(t, f.logins, 2, "a token past its lease is logged in for again")
}

func TestLogin_LogsInAgainAfterTheTokenIsRevoked(t *testing.T) {
	f := &fakeBao{}
	c, _, _ := loginCustodian(t, f, "")
	for range 2 {
		_, err := c.Create(context.Background())
		require.NoError(t, err)
	}

	delete(f.valid, "s.login-1") // revoked, as an operator or a restart of OpenBao would
	_, err := c.Create(context.Background())
	require.NoError(t, err)

	require.Len(t, f.logins, 2)
	assert.Equal(t, "s.login-2", f.tokens[len(f.tokens)-1])
}

// A 403 to a token just logged in for is the policy's, not a revoked
// token's: a second login would be refused the same way.
func TestLogin_DoesNotRetryAPolicyThatDeniesAFreshToken(t *testing.T) {
	f := &fakeBao{forbidAll: true}
	c, _, _ := loginCustodian(t, f, "")

	_, err := c.Create(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
	assert.Len(t, f.logins, 1)
}

func TestLogin_RefusalsNameNeitherTheIdentityNorAToken(t *testing.T) {
	f := &fakeBao{denyLogin: true}
	c, idFile, _ := loginCustodian(t, f, "")

	_, err := c.Create(context.Background())
	require.Error(t, err)
	assert.False(t, errorx.IsKind(err, errorx.KindUnavailable), "a role OpenBao does not know is a misconfiguration")
	assert.NotContains(t, err.Error(), identity)

	require.NoError(t, os.Remove(idFile))
	_, err = c.Create(context.Background())
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "an identity not yet written is waited for")
	assert.NotContains(t, err.Error(), identity)
}

func TestNew_RefusesTheIdentityFormWhereItCannotWork(t *testing.T) {
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	idFile := filepath.Join(dir, "identity")
	require.NoError(t, os.WriteFile(idFile, []byte(identity), 0o600))
	open := filepath.Join(dir, "open")
	require.NoError(t, os.WriteFile(open, []byte(identity), 0o600))
	require.NoError(t, os.Chmod(open, 0o644))
	tokenFile := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte(token), 0o600))

	good := transit.Settings{Address: srv.URL, CAFile: caFile, IdentityFile: idFile, AuthRole: "garam", Key: "garam"}
	_, err := transit.New(good)
	require.NoError(t, err, "the control: the settings every refusal below departs from")

	for name, mutate := range map[string]func(*transit.Settings){
		"an identity others can read": func(s *transit.Settings) { s.IdentityFile = open },
		"an identity not there":       func(s *transit.Settings) { s.IdentityFile = filepath.Join(dir, "absent") },
		"no role":                     func(s *transit.Settings) { s.AuthRole = "" },
		"both files":                  func(s *transit.Settings) { s.TokenFile = tokenFile },
		"a mount that climbs":         func(s *transit.Settings) { s.AuthMount = "a/../b" },
		"neither file":                func(s *transit.Settings) { s.IdentityFile = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s := good
			mutate(&s)
			_, err := transit.New(s)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), err.Error())
		})
	}

	_, err = transit.New(transit.Settings{Address: srv.URL, CAFile: caFile, TokenFile: tokenFile, AuthRole: "garam", Key: "garam"})
	require.Error(t, err, "a role beside the token file logs in to nothing")
}
