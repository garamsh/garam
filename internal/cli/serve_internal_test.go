package cli

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authz"
	authzmocks "github.com/garamsh/garam/internal/authz/mocks"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/health"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
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

// channelTo returns a client channel for addr, closed with the test.
func channelTo(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
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

func TestAuthzChannel_RefusesAChannelThatReachesNoEngine(t *testing.T) {
	// Internal: readiness reads this beside an unreachable database.
	// A gRPC server on a port of the kernel's choosing is the engine;
	// the address nothing serves is the same channel without one, which
	// is what the check has to tell apart.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	engine := grpc.NewServer()
	go func() { _ = engine.Serve(lis) }()
	t.Cleanup(engine.Stop)

	served, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, authzChannel{conn: channelTo(t, lis.Addr().String())}.Check(served))

	// Port 1 is reserved and unservable, so this channel never reaches
	// Ready and the check's own deadline is what ends the wait.
	unserved, cancelUnserved := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelUnserved()
	err = authzChannel{conn: channelTo(t, "127.0.0.1:1")}.Check(unserved)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
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
	srv := api.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, api.Config{})

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

// TestEngineProbeHandler_AnswersAPanicOnTheEnginesOtherListener is
// `serve authz`'s wiring for the port beside its gRPC one. Nothing on
// that port is a spec operation, so the recovery this assembly puts
// outermost is the only one a panic here meets.
func TestEngineProbeHandler_AnswersAPanicOnTheEnginesOtherListener(t *testing.T) {
	// Internal: serve authz mounts no probe a test can make panic.
	var served lines
	status, _ := readiness(t, recordingObservability(&served).engineProbeHandler(presentProbe{}))
	require.Equal(t, http.StatusOK, status, "the probe this handler mounts is reachable")

	var out lines
	status, body := readiness(t, recordingObservability(&out).engineProbeHandler(panickingProbe{}))

	require.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, "internal error\n", body, "no internals reach the body")

	line := decodeLine(t, &out)
	assert.Equal(t, "http panic", line["msg"])
	assert.Equal(t, "GET "+health.ReadyPath, line["operation"])
	assert.Contains(t, line["error"], "panic: readiness defect",
		"the line is the only record this panic leaves")
}

// panickingEngine is the engine handler a recover interceptor exists
// for. Check is the programmer bug; Explain answers, so one connection
// carries both a call that panics and a call that must still be served
// after it.
type panickingEngine struct {
	authzv1.UnimplementedAuthzServiceServer
}

func (panickingEngine) Check(context.Context, *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	panic("policy engine defect")
}

func (panickingEngine) Explain(context.Context, *authzv1.ExplainRequest) (*authzv1.ExplainResponse, error) {
	return &authzv1.ExplainResponse{Allowed: true}, nil
}

// TestEngineServer_AnswersAPanicAndKeepsServing panics a real handler
// through a real server, because an interceptor called directly proves
// the interceptor and not the wiring. The server is the one serveAuthz
// serves the engine on, assembled by the function that assembles it
// there, so an interceptor dropped from that chain is dropped here.
//
// The second call is what makes this evidence. Without the
// interceptor grpc-go reaches no frame that stops the panic and the
// process ends — this test binary among them — so a run that gets far
// enough to answer Explain has already shown the process survived, and
// answering it on the same connection shows the server did too rather
// than merely staying alive with nothing left listening.
//
// The status message is asserted for what it does not carry. It
// travels to the caller, and a stack is not something a caller of the
// engine is owed; the stack goes to the line instead, which decodeLine
// holds to exactly one by parsing the whole buffer.
func TestEngineServer_AnswersAPanicAndKeepsServing(t *testing.T) {
	// Internal: serve authz registers no engine a test can make panic.
	var out lines
	obs := recordingObservability(&out)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	engine := obs.engineServer()
	authzv1.RegisterAuthzServiceServer(engine, panickingEngine{})
	go func() { _ = engine.Serve(lis) }()
	t.Cleanup(engine.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := authzv1.NewAuthzServiceClient(conn)

	_, err = client.Check(context.Background(), &authzv1.CheckRequest{SubjectGrn: testSubject})

	require.Error(t, err, "the caller is answered rather than left with a dead connection")
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Equal(t, "internal error", status.Convert(err).Message())
	assert.NotContains(t, status.Convert(err).Message(), "policy engine defect",
		"the panic value does not travel to the caller")

	// Read before the second call, so decodeLine's whole-buffer parse
	// holds the panic to one line: recovering outside logRPCs is what
	// keeps it from also emitting the served line for the same call.
	line := decodeLine(t, &out)
	assert.Equal(t, "rpc panic", line["msg"])
	assert.Equal(t, codes.Internal.String(), line["code"])
	assert.Equal(t, "/garam.authz.v1.AuthzService/Check", line["operation"])
	assert.Contains(t, line["error"], "policy engine defect", "the panic reaches the line")
	assert.Contains(t, line["error"], "goroutine", "and so does the stack")

	explained, err := client.Explain(context.Background(), &authzv1.ExplainRequest{SubjectGrn: testSubject})
	require.NoError(t, err, "the server still serves the connection the panic was answered on")
	assert.True(t, explained.GetAllowed())
}

// enginePolicySeam stands the engine's policy service and the api's
// seam to it on either side of a real gRPC hop, and answers every store
// call with err. The two packages never import each other (ADR-0003),
// so the composition root is where the pair can be held together.
func enginePolicySeam(t *testing.T, out *lines, err error) api.PolicyManager {
	t.Helper()
	store := authzmocks.NewMockStore(t)
	store.EXPECT().SetPolicyStatus(mock.Anything, mock.Anything, mock.Anything).Return(nil, err)

	lis, listenErr := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, listenErr)
	engine := recordingObservability(out).engineServer()
	authzv1.RegisterPolicyServiceServer(engine, authz.NewPolicyGRPCServer(store, authz.NewPolicyCache(store)))
	go func() { _ = engine.Serve(lis) }()
	t.Cleanup(engine.Stop)

	conn, dialErr := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, dialErr)
	t.Cleanup(func() { _ = conn.Close() })
	return api.NewGRPCPolicyManager(conn)
}

// TestEngineSeam_DisclosesOverTheHopWhatItWouldInProcess holds the
// client's answer for one store error against what the same error
// reads as in process. wire.MessageOf decides what a caller may be
// told from the type of every link in a chain; a gRPC status carries
// text and no types, so the hop is where that decision is lost.
//
// The driver's error is what must not cross — a plain error stands for
// it, because wire.authored reads the link's type and never its words.
// The authored sentinel beside it is the control: withholding
// everything would pass the first case and fail this one, so the two
// are read together. The third holds the other half of
// runtime-safety.md §Errors reach a handler — the client is told
// nothing, and the engine's own line carries the cause.
func TestEngineSeam_DisclosesOverTheHopWhatItWouldInProcess(t *testing.T) {
	// Internal: serve authz holds a Postgres store, not a failing one.
	driver := errors.New("policy attachment crosses orgs (SQLSTATE P0001)")
	tests := []struct {
		name    string
		stored  error
		logged  bool
		refused string
	}{
		{
			name:    "unauthored",
			stored:  errorx.Wrap(driver, "attach policy", errorx.KindFailedPrecondition),
			refused: "SQLSTATE",
		},
		{
			name:   "authored",
			stored: errorx.New(errorx.KindNotFound, "policy not found"),
		},
		{
			name:    "unauthored fault of this process",
			stored:  errorx.Wrap(driver, "list active policies", errorx.KindInternal),
			logged:  true,
			refused: "SQLSTATE",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out lines
			seam := enginePolicySeam(t, &out, tc.stored)

			_, err := seam.ActivatePolicy(context.Background(),
				grn.New("acme", "default", "policy", "9f2ac1b40d8e7a35"))

			require.Error(t, err)
			assert.Equal(t, errorx.KindOf(tc.stored), errorx.KindOf(err), "the kind crosses the hop")
			assert.Equal(t, wire.MessageOf(tc.stored), wire.MessageOf(err),
				"the hop tells a caller what the same error tells one in process")
			if tc.refused != "" {
				assert.NotContains(t, wire.MessageOf(err), tc.refused)
			}
			if tc.logged {
				assert.Contains(t, decodeLine(t, &out)["error"], tc.refused,
					"the cause the client is not told reaches the engine's line")
			}
		})
	}
}
