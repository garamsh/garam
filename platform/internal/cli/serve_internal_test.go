package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	"github.com/garamsh/garam/platform/internal/health"
)

// TestGoogleProvider_DiscoveryFailure is also the control for the two
// gates above: a fully configured provider reaches construction, so
// their refusals come from the credential check and not from a helper
// that refuses everything.
func TestGoogleProvider_DiscoveryFailure(t *testing.T) {
	// Internal: the environment names no Google issuer to fail on.
	// Port 1 is reserved and unservable, so discovery cannot connect.
	_, err := googleProvider(context.Background(), authn.GoogleConfig{
		ClientID:     "client-id",
		ClientSecret: "secret",
		Issuer:       "http://127.0.0.1:1",
	})
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "kind: %v", err)
}

// TestEnvBool_RefusesAValueItCannotRead covers the three outcomes the
// setting has: a spelling that reads as true, "false", the one spelling
// that must still disable Secure, and a value that reads as neither and
// stops the process rather than landing silently on the weaker setting.
func TestEnvBool_RefusesAValueItCannotRead(t *testing.T) {
	// Internal: the value read reaches only a stored session's cookie.
	const key = "GARAM_COOKIE_SECURE"

	// The control for the refusal below: these are what stops being
	// accepted if the parse is what refuses rather than the value.
	t.Run("a spelling that reads as true", func(t *testing.T) {
		t.Setenv(key, "True")
		got, err := envBool(key, "true")
		require.NoError(t, err)
		assert.True(t, got)
	})
	t.Run("false still disables it", func(t *testing.T) {
		t.Setenv(key, "false")
		got, err := envBool(key, "true")
		require.NoError(t, err)
		assert.False(t, got)
	})
	t.Run("unset takes the fallback", func(t *testing.T) {
		t.Setenv(key, "")
		got, err := envBool(key, "true")
		require.NoError(t, err)
		assert.True(t, got)
	})

	t.Run("a value it cannot read", func(t *testing.T) {
		t.Setenv(key, "yes")
		_, err := envBool(key, "true")
		require.Error(t, err)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		assert.Contains(t, err.Error(), key, "the error names the variable, as GARAM_SESSION_TTL's does")
	})
}

// panickingProbe is a readiness dependency whose check is the
// programmer bug a recover middleware exists for. The probes are
// mounted beside a surface's operations rather than inside them, so
// nothing but the server-level recovery an assembly puts outermost
// answers a panic raised here.
type panickingProbe struct{}

func (panickingProbe) Name() string { return "readiness" }

func (panickingProbe) Check(context.Context) error { panic("readiness defect") }

// presentProbe is that dependency answering. It is the control for each
// panic below: the same handler on the same path answers 200 with it,
// so what the 500 reports is the panic rather than a probe mounted
// somewhere a caller cannot reach.
type presentProbe struct{}

func (presentProbe) Name() string { return "readiness" }

func (presentProbe) Check(context.Context) error { return nil }

// readiness runs one GET of the readiness probe against handler and
// returns the status and body it answered with.
func readiness(t *testing.T, handler http.Handler) (int, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	resp, err := server.Client().Get(server.URL + health.ReadyPath)
	require.NoError(t, err, "the client read no answer at all")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, string(body)
}

// TestAPIHandler_AnswersAPanicOnARouteTheGeneratedSliceDoesNotReach is
// `serve api`'s wiring. The composition root puts /metrics and the two
// probes on the mux it hands the generated router as BaseRouter, and
// oapi-codegen applies the middleware slice inside each operation's own
// wrapper rather than around the router — so a route registered beside
// the spec operations carries none of it, however the slice is written.
//
// Without the server-level middleware apiHandler puts outermost, the
// client's own Get fails and the require below never reaches a status.
// The body is what says which middleware answered: the surface's writes
// the specification's Error as JSON, and this one writes plain text
// because no specification describes the path it is answering for.
//
// The line names no route: otelhttp clones the request before the mux
// matches, so the pattern the mux writes reaches nothing outside it —
// which is what this middleware is. That is the wiring's behaviour and
// not this test's subject, so it is left unasserted.
func TestAPIHandler_AnswersAPanicOnARouteTheGeneratedSliceDoesNotReach(t *testing.T) {
	// Internal: serve api mounts no probe a test can make panic.
	srv := api.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, api.Config{})

	var served lines
	status, _ := readiness(t, recordingObservability(&served).apiHandler(srv, presentProbe{}))
	require.Equal(t, http.StatusOK, status, "the probe this handler mounts is reachable")

	var out lines
	status, body := readiness(t, recordingObservability(&out).apiHandler(srv, panickingProbe{}))

	require.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, "internal error\n", body, "no internals reach the body")

	line := decodeLine(t, &out)
	assert.Equal(t, "http panic", line["msg"])
	assert.Equal(t, float64(http.StatusInternalServerError), line["status"])
	assert.NotEmpty(t, line["trace_id"])
	assert.Contains(t, line["error"], "panic: readiness defect",
		"the line is the only record this panic leaves")
}
