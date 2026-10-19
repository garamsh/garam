package e2e_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCLI_TheBuiltBinaryLogsInByTheDeviceGrant runs `garam login`
// against the built `serve api`, reached behind an /api prefix as the
// console's rewrite serves it, approves the code it prints on the
// principal's session, and reads who it signed in as with `garam
// whoami`. The approval goes through the API rather than the console's
// /device page, which is not yet built (#1616).
func TestCLI_TheBuiltBinaryLogsInByTheDeviceGrant(t *testing.T) {
	garam := buildGaram(t)
	p := prepareDatabase(t, buildPrincipalFixture(t))
	grpcAddr, metricsAddr, httpAddr := freeAddr(t), freeAddr(t), freeAddr(t)
	serve(t, garam, "authz", []string{
		"GARAM_DATABASE_URL=" + p.DatabaseURL, "GARAM_GRPC_ADDR=" + grpcAddr, "GARAM_METRICS_ADDR=" + metricsAddr,
	}, "http://"+metricsAddr+readyPath)
	stopAPI := serve(t, garam, "api", []string{
		"GARAM_DATABASE_URL=" + p.DatabaseURL, "GARAM_HTTP_ADDR=" + httpAddr, "GARAM_AUTHZ_ADDR=" + grpcAddr,
	}, "http://"+httpAddr+readyPath)
	api, err := url.Parse("http://" + httpAddr)
	require.NoError(t, err)
	console := httptest.NewServer(http.StripPrefix("/api", httputil.NewSingleHostReverseProxy(api)))
	t.Cleanup(console.Close)

	env := []string{"XDG_CONFIG_HOME=" + t.TempDir(), "GARAM_ENDPOINT=" + console.URL, "HOME=" + t.TempDir()}
	login := exec.Command(garam, "login", "--no-browser")
	login.Env = env
	stderr, err := login.StderrPipe()
	require.NoError(t, err)
	require.NoError(t, login.Start())
	t.Cleanup(func() { _ = login.Process.Kill() })

	code := regexp.MustCompile(`Your code is ([B-Z]{4}-[B-Z]{4})\.`)
	lines := bufio.NewScanner(stderr)
	var userCode string
	for userCode == "" && lines.Scan() {
		if m := code.FindStringSubmatch(lines.Text()); m != nil {
			userCode = m[1]
		}
	}
	require.NotEmpty(t, userCode, "login printed its code")
	c := consumerClient{t: t, base: "http://" + httpAddr}
	status, body := c.do(http.MethodPost, "/auth/device/approval", p.Session.Token,
		`{"userCode":"`+userCode+`","approve":true}`)
	require.Equal(t, http.StatusNoContent, status, body)
	// The pipe is drained before Wait, which closes it, and both run
	// apart from the test so a login that never exits fails it.
	var rest []string
	exited := make(chan error, 1)
	go func() {
		for lines.Scan() {
			rest = append(rest, lines.Text())
		}
		exited <- login.Wait()
	}()
	select {
	case err := <-exited:
		require.NoError(t, err, "login: %v", rest)
	case <-time.After(30 * time.Second):
		t.Fatal("login did not exit once its code was approved")
	}

	whoami := exec.Command(garam, "whoami", "-o", "json")
	whoami.Env = env
	out, err := whoami.Output()
	require.NoError(t, err)
	assert.Equal(t, p.UserGRN, field(t, string(out), "grn"), "the CLI is signed in as who approved it")

	credentials := filepath.Join(env[0][len("XDG_CONFIG_HOME="):], "garam", "credentials")
	raw, err := os.ReadFile(credentials)
	require.NoError(t, err)
	var file struct {
		Endpoints map[string]struct {
			AccessToken string `json:"accessToken"`
		} `json:"endpoints"`
	}
	require.NoError(t, json.Unmarshal(raw, &file))
	access := file.Endpoints[console.URL].AccessToken
	require.NotEmpty(t, access)

	logout := exec.Command(garam, "logout")
	logout.Env = env
	require.NoError(t, logout.Run())
	again := exec.Command(garam, "whoami")
	again.Env = env
	assert.Error(t, again.Run(), "after logout the CLI is signed out")

	assert.NotContains(t, stopAPI(), access, "the api logs no bearer token")
}
