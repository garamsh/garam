package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The operation-authority routes' contract header and the version
// this suite was written against, spelled here for the reason the
// probe paths are: this layer calls the binary as a consumer does.
const (
	contractHeader  = "Garam-Contract-Version"
	contractVersion = "operation-authority.v1"
)

// prepared is the part of `testprincipal prepare`'s output a
// consumer reads.
type prepared struct {
	Contract    int    `json:"contract"`
	Database    string `json:"database"`
	DatabaseURL string `json:"databaseUrl"`
	UserGRN     string `json:"userGrn"`
	Session     struct {
		Cookie string `json:"cookie"`
		Token  string `json:"token"`
	} `json:"session"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// buildPrincipalFixture builds tests/testprincipal as a consumer
// builds it from a Garam checkout.
func buildPrincipalFixture(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "testprincipal")
	build := exec.Command("go", "build", "-o", bin, "./tests/testprincipal")
	build.Dir = moduleRoot(t)
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build: %s", out)
	return bin
}

// fixture runs the fixture binary and answers its exit code, stdout
// and stderr.
func fixture(t *testing.T, bin string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else {
		require.NoError(t, err)
	}
	return code, stdout.String(), stderr.String()
}

// prepareDatabase runs `prepare` against the suite's server and drops
// the database once every process using it has stopped: cleanups run
// last-registered first, so one registered here runs after those of
// the processes started on it.
func prepareDatabase(t *testing.T, bin string) prepared {
	t.Helper()
	server := os.Getenv("GARAM_TEST_DATABASE_URL")
	code, stdout, stderr := fixture(t, bin, "prepare", "-contract", "1",
		"-server-url", server, "-migrations", filepath.Join(moduleRoot(t), "migrations"))
	require.Equal(t, 0, code, "prepare: %s", stderr)
	assert.Empty(t, stderr, "a successful prepare writes nothing to stderr")
	var p prepared
	require.NoError(t, json.Unmarshal([]byte(stdout), &p))
	t.Cleanup(func() {
		code, _, stderr := fixture(t, bin, "drop", "-contract", "1", "-server-url", server, "-database", p.Database)
		assert.Equal(t, 0, code, "drop: %s", stderr)
	})
	return p
}

// serve starts one serve process and stops it with SIGTERM at
// cleanup, waiting for it to exit so nothing holds the database.
func serve(t *testing.T, bin, name string, env []string, probe string) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	cmd := exec.Command(bin, "serve", name)
	cmd.Env = env
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
	})
	waitForProbe(t, probe, &logs)
	return &logs
}

// consumerClient calls the built api over HTTP as a consumer does.
type consumerClient struct {
	t    *testing.T
	base string
}

// do sends one request, with the session cookie when token is set,
// and answers the status and body.
func (c consumerClient) do(method, path, token, body string) (int, string) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, strings.NewReader(body))
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(contractHeader, contractVersion)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "garam_session", Value: token})
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(c.t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)
	return resp.StatusCode, string(got)
}

// register accepts invitation as a new password account and answers
// the session token its sign-in set.
func (c consumerClient) register(invitation, username string) string {
	c.t.Helper()
	resp, err := http.Post(c.base+"/auth/password/register", "application/json", strings.NewReader(
		`{"invitationToken":"`+invitation+`","username":"`+username+`","password":"correct-horse-battery"}`))
	require.NoError(c.t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(c.t, http.StatusCreated, resp.StatusCode)
	for _, ck := range resp.Cookies() {
		if ck.Name == "garam_session" {
			return ck.Value
		}
	}
	c.t.Fatal("registration set no session cookie")
	return ""
}

// field reads one string field of a JSON object.
func field(t *testing.T, body, name string) string {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &m), body)
	v, ok := m[name].(string)
	require.True(t, ok, "%s has no string %q", body, name)
	return v
}

// createOrg creates an organization with its hosted and controller
// operators as token's user, and answers its slug.
func (c consumerClient) createOrg(token, name string) string {
	c.t.Helper()
	status, body := c.do(http.MethodPost, "/orgs", token, `{"name":"`+name+`"}`)
	require.Equal(c.t, http.StatusCreated, status, body)
	g := field(c.t, body, "grn")
	slug := g[strings.LastIndex(g, ":")+1:]
	for _, id := range []string{"hosted", "controller"} {
		status, body = c.do(http.MethodPost, "/orgs/"+slug+"/operators", token, `{"identifier":"`+id+`"}`)
		require.Equal(c.t, http.StatusCreated, status, body)
	}
	return slug
}

func (c consumerClient) delegate(token, slug string) (int, string) {
	c.t.Helper()
	return c.do(http.MethodPut, "/orgs/"+slug+"/operators/hosted/delegation", token,
		`{"requestId":"g-1","controllers":["grn:`+slug+`:default:operator:controller"],"operations":["agent:create"],"expiresAt":"`+
			time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+`"}`)
}

func (c consumerClient) mint(token, slug, requestID string) (int, string) {
	c.t.Helper()
	return c.do(http.MethodPost, "/orgs/"+slug+"/operation-authorities", token,
		`{"audience":"grn:`+slug+`:default:operator:hosted","operation":"agent:create","target":"grn:`+slug+
			`:default:operator:controller","requestId":"`+requestID+`","bodySha256":"`+strings.Repeat("c", 64)+`"}`)
}

// TestPrincipalFixture_DrivesDelegationThroughThePublicAPI is what a
// consumer does with the fixture: prepare a database, start the built
// garam on it, and act through public routes only. The principal sets
// up two organizations, invites a member into the first, delegates,
// and mints; every refusal sits beside the call that is accepted.
func TestPrincipalFixture_DrivesDelegationThroughThePublicAPI(t *testing.T) {
	garam := buildGaram(t)
	p := prepareDatabase(t, buildPrincipalFixture(t))
	assert.Equal(t, 1, p.Contract)
	assert.True(t, p.ExpiresAt.After(time.Now()), "the session expires in the future")
	assert.True(t, p.ExpiresAt.Before(time.Now().Add(2*time.Hour)), "and soon: the default lifetime is an hour")

	grpcAddr, metricsAddr, httpAddr := freeAddr(t), freeAddr(t), freeAddr(t)
	serve(t, garam, "authz", []string{
		"GARAM_DATABASE_URL=" + p.DatabaseURL, "GARAM_GRPC_ADDR=" + grpcAddr, "GARAM_METRICS_ADDR=" + metricsAddr,
	}, "http://"+metricsAddr+readyPath)
	apiLogs := serve(t, garam, "api", []string{
		"GARAM_DATABASE_URL=" + p.DatabaseURL, "GARAM_HTTP_ADDR=" + httpAddr, "GARAM_AUTHZ_ADDR=" + grpcAddr,
	}, "http://"+httpAddr+readyPath)
	c := consumerClient{t: t, base: "http://" + httpAddr}
	owner := p.Session.Token

	status, body := c.do(http.MethodGet, "/me", "", "")
	assert.Equal(t, http.StatusUnauthorized, status, "no session: %s", body)
	status, body = c.do(http.MethodGet, "/me", owner, "")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, p.UserGRN, field(t, body, "grn"), "the fixture session is the principal's")

	status, body = c.do(http.MethodPost, "/orgs", "", `{"name":"acme"}`)
	assert.Equal(t, http.StatusUnauthorized, status, "no session: %s", body)
	acme := c.createOrg(owner, "acme")
	globex := c.createOrg(owner, "globex")

	status, body = c.do(http.MethodPost, "/orgs/"+acme+"/invitations", owner, `{"role":"member"}`)
	require.Equal(t, http.StatusCreated, status, body)
	member := c.register(field(t, body, "token"), "bob")

	status, body = c.delegate(member, acme)
	assert.Equal(t, http.StatusForbidden, status, "a member does not delegate: %s", body)
	for _, slug := range []string{acme, globex} {
		status, body = c.delegate(owner, slug)
		require.Equal(t, http.StatusCreated, status, body)
	}

	status, body = c.mint("", acme, "req-0")
	assert.Equal(t, http.StatusUnauthorized, status, "no session: %s", body)
	status, body = c.mint(member, acme, "req-1")
	require.Equal(t, http.StatusCreated, status, "a member of acme mints in acme: %s", body)
	assert.NotEmpty(t, field(t, body, "authority"))

	// globex holds a grant covering the request, so the member's
	// refusal there is the organization's and the owner's acceptance
	// of the same request is the control.
	status, body = c.mint(member, globex, "req-2")
	assert.Equal(t, http.StatusForbidden, status, "acme's member mints nothing in globex: %s", body)
	status, body = c.mint(owner, globex, "req-2")
	assert.Equal(t, http.StatusCreated, status, body)

	assert.NotContains(t, apiLogs.String(), owner, "the api logs no session token")
}

// TestPrincipalFixture_DropRemovesOnlyWhatPrepareCreated asks drop to
// remove the suite's own database and a database shaped like one
// prepare names but not marked by it; both stay. The prepared one is
// removed, which is the control.
func TestPrincipalFixture_DropRemovesOnlyWhatPrepareCreated(t *testing.T) {
	ctx := context.Background()
	server := os.Getenv("GARAM_TEST_DATABASE_URL")
	bin := buildPrincipalFixture(t)
	conn, err := pgx.Connect(ctx, server)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(ctx) })

	const unmarked = "garam_principal_00000000000000aa"
	_, err = conn.Exec(ctx, "CREATE DATABASE "+unmarked)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+unmarked) })

	exists := func(name string) bool {
		var n int
		require.NoError(t, conn.QueryRow(ctx, "SELECT count(*) FROM pg_database WHERE datname = $1", name).Scan(&n))
		return n == 1
	}
	var own string
	require.NoError(t, conn.QueryRow(ctx, "SELECT current_database()").Scan(&own))

	code, _, stderr := fixture(t, bin, "drop", "-contract", "1", "-server-url", server, "-database", own)
	assert.Equal(t, 2, code, stderr)
	assert.True(t, exists(own))
	code, _, stderr = fixture(t, bin, "drop", "-contract", "1", "-server-url", server, "-database", unmarked)
	assert.Equal(t, 1, code, stderr)
	assert.Contains(t, stderr, "does not carry the marker")
	assert.True(t, exists(unmarked))

	code, stdout, stderr := fixture(t, bin, "prepare", "-contract", "1", "-server-url", server,
		"-migrations", filepath.Join(moduleRoot(t), "migrations"))
	require.Equal(t, 0, code, stderr)
	var p prepared
	require.NoError(t, json.Unmarshal([]byte(stdout), &p))
	require.True(t, exists(p.Database))
	code, _, stderr = fixture(t, bin, "drop", "-contract", "1", "-server-url", server, "-database", p.Database)
	assert.Equal(t, 0, code, stderr)
	assert.False(t, exists(p.Database))
}

// TestPrincipalFixture_IsAbsentFromTheGaramBinary asks the built
// binary and its build target. `garam serve` is the control for the
// first: the binary answers the commands it has.
func TestPrincipalFixture_IsAbsentFromTheGaramBinary(t *testing.T) {
	bin := buildGaram(t)
	for _, name := range []string{"testprincipal", "prepare"} {
		out, err := exec.Command(bin, name, "--help").CombinedOutput()
		assert.Error(t, err, "garam %s: %s", name, out)
		assert.Contains(t, string(out), "unknown command")
	}
	out, err := exec.Command(bin, "serve", "--help").CombinedOutput()
	assert.NoError(t, err, string(out))

	list := exec.Command("go", "list", "-deps", "./cmd/garam")
	list.Dir = moduleRoot(t)
	deps, err := list.Output()
	require.NoError(t, err)
	assert.Contains(t, string(deps), "github.com/garamsh/garam/internal/authn", "the list is the binary's own packages")
	assert.NotContains(t, string(deps), "github.com/garamsh/garam/tests/")
}
