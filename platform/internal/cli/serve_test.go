package cli_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/health"
)

// unreachableDatabase is a well-formed URL nothing answers on. A pool
// is built from it without dialling, so a serve command gets as far as
// its first query.
const unreachableDatabase = "postgres://garam@127.0.0.1:1/garam"

// serveAPIEnv sets what `serve api` reads to a start that opens no
// connection, with overrides applied over it.
func serveAPIEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	env := map[string]string{
		"GARAM_DATABASE_URL":      unreachableDatabase,
		"GARAM_AUTHZ_ADDR":        "127.0.0.1:1",
		"GARAM_HTTP_ADDR":         "",
		"GARAM_SESSION_TTL":       "",
		"GARAM_COOKIE_SECURE":     "",
		"GARAM_MACHINE_TOKEN_URL": "",
		"GOOGLE_CLIENT_ID":        "",
		"GOOGLE_CLIENT_SECRET":    "",
		"GOOGLE_REDIRECT_URL":     "",
	}
	for name, value := range overrides {
		env[name] = value
	}
	for name, value := range env {
		t.Setenv(name, value)
	}
}

// refusedStart runs `serve api` over overrides, which are expected to
// stop it before it listens, and answers the error it stopped with. An
// unreadable GARAM_SESSION_TTL is set beneath them unless they name it,
// so a start nothing refused earlier still ends, on that variable.
func refusedStart(t *testing.T, overrides map[string]string) error {
	t.Helper()
	if _, named := overrides["GARAM_SESSION_TTL"]; !named {
		overrides["GARAM_SESSION_TTL"] = "a week"
	}
	serveAPIEnv(t, overrides)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := runRootContext(ctx, t, "serve", "api")
	require.Error(t, err)
	return err
}

// servedAPI runs `serve api` on a loopback port until the test ends and
// answers its base URL once the listener answers liveness.
func servedAPI(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	serveAPIEnv(t, map[string]string{"GARAM_HTTP_ADDR": addr})

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() {
		_, err := runRootContext(ctx, t, "serve", "api")
		stopped <- err
	}()
	t.Cleanup(func() {
		cancel()
		assert.NoError(t, <-stopped, "a requested stop is not a failure")
	})

	url := "http://" + addr
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		resp, err := http.Get(url + health.LivePath)
		if !assert.NoError(c, err) {
			return
		}
		_ = resp.Body.Close()
		assert.Equal(c, http.StatusOK, resp.StatusCode)
	}, 10*time.Second, 10*time.Millisecond, "serve api never answered liveness")
	return url
}

func TestGoogleProvider_NotConfigured(t *testing.T) {
	// No credentials: no provider, and no discovery round trip, so the
	// start reaches the variable read after the provider.
	err := refusedStart(t, map[string]string{})
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "got %v", err)
	assert.Contains(t, err.Error(), "GARAM_SESSION_TTL")
}

// TestGoogleProvider_RefusesPartialCredentials is read against
// TestGoogleProvider_NotConfigured: with neither credential the same
// start goes past the provider, so the refusal is the credential check.
func TestGoogleProvider_RefusesPartialCredentials(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"client id without secret", map[string]string{"GOOGLE_CLIENT_ID": "client-id"}},
		{"secret without client id", map[string]string{"GOOGLE_CLIENT_SECRET": "secret"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := refusedStart(t, tc.env)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "kind: %v", err)
			assert.Contains(t, err.Error(), "GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET")
		})
	}
}

// passwordLogin is a sign-in body carrying a password of n bytes. The
// username names no account, so the route's own answer is the same
// whatever the body weighs.
func passwordLogin(n int) string {
	return `{"username":"","password":"` + strings.Repeat("x", n) + `"}`
}

// postTo sends one POST to url and returns the status and body.
func postTo(t *testing.T, url, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(got)
}

// TestAPIHandler_RefusesARequestBodyPastTheBound holds the browser
// listener's bound on what one request may weigh. Nothing after the
// decoder is a bound: `api/openapi.yaml` declares no maxLength at all,
// and where a specification does the check runs on a string already in
// memory, so the refusal has to be the listener's.
//
// The control is the same body well inside the bound. The route refuses
// that too — for the username, which names no account — so a refusal
// alone would hold as readily for a route refusing everything.
func TestAPIHandler_RefusesARequestBodyPastTheBound(t *testing.T) {
	login := servedAPI(t) + "/auth/password/login"

	status, body := postTo(t, login, passwordLogin(1024))
	require.Equal(t, http.StatusUnauthorized, status, "the control: refused for what it says")
	require.JSONEq(t, `{"kind":"unauthenticated","message":"username or password is incorrect"}`, body)

	status, body = postTo(t, login, passwordLogin(1<<20))

	assert.Equal(t, http.StatusRequestEntityTooLarge, status, "past the bound the body is not read at all")
	assert.JSONEq(t, `{"kind":"invalid_argument","message":"the request body is larger than a request may carry"}`, body)
}

// connectionsReused sends two GETs of url in turn over one keep-alive
// client, and returns whether each response asked to close its
// connection and whether each request reused one.
func connectionsReused(t *testing.T, url string) (closed, reused []bool) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{}}
	t.Cleanup(client.CloseIdleConnections)
	for range 2 {
		var got httptrace.GotConnInfo
		ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) { got = info },
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		_, _ = io.Copy(io.Discard, resp.Body)
		require.NoError(t, resp.Body.Close())
		// net/http moves a Connection: close answer to resp.Close.
		closed = append(closed, resp.Close)
		reused = append(reused, got.Reused)
	}
	return closed, reused
}

func TestAPIServer_ClosesEachConnectionAfterOneResponse(t *testing.T) {
	closed, reused := connectionsReused(t, servedAPI(t)+health.LivePath)
	assert.Equal(t, []bool{true, true}, closed)
	assert.Equal(t, []bool{false, false}, reused, "the second request reused a connection")

	// The control: a server left at net/http's default keeps the
	// connection, so the client above would reuse one it could.
	keeping := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(keeping.Close)
	closed, reused = connectionsReused(t, keeping.URL)
	assert.Equal(t, []bool{false, false}, closed)
	assert.Equal(t, []bool{false, true}, reused)
}
