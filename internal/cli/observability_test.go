// Internal test package: the middleware, the interceptor and the
// trace-id fallback are unexported wiring with no exported surface
// that reaches them.
package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/user"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

const testSubject = "grn:acme:default:user:alice"

// lines collects the log output of a handler that may run on another
// goroutine.
type lines struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lines) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// recordingObservability returns one whose lines land in out.
func recordingObservability(out *lines) *observability {
	return &observability{logger: slog.New(slog.NewJSONHandler(out, nil))}
}

// decodeLine parses the single line out holds.
func decodeLine(t *testing.T, out *lines) map[string]any {
	t.Helper()
	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(out.String()), &line))
	return line
}

func TestNewObservability_CollectsIntoOneRegistry(t *testing.T) {
	obs, err := newObservability("garam-test")
	require.NoError(t, err)

	families, err := obs.registry.Gather()
	require.NoError(t, err)
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}
	assert.Contains(t, names, "target_info", "the exporter collects into the registry")
	assert.Contains(t, names, "go_goroutines", "and so does the Go runtime")
}

func TestLogRequests_ReportsTheRouteAndThePrincipal(t *testing.T) {
	var out lines
	obs := recordingObservability(&out)

	// The route carries an invitation token and the query an OAuth
	// code: neither may reach the line.
	mux := http.NewServeMux()
	mux.Handle("GET /invitations/{token}/accept", obs.logRequests(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			api.PrincipalFrom(r).GRN = testSubject
			w.WriteHeader(http.StatusNoContent)
		})))
	mux.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/invitations/tok-secret/accept?code=oauth-code", nil))

	line := decodeLine(t, &out)
	assert.Equal(t, "GET /invitations/{token}/accept", line["operation"])
	assert.Equal(t, testSubject, line["user_id"])
	assert.Equal(t, float64(http.StatusNoContent), line["status"])
	assert.NotEmpty(t, line["trace_id"])
	assert.NotContains(t, out.String(), "tok-secret")
	assert.NotContains(t, out.String(), "oauth-code")
}

func TestLogRequests_OmitsAnUnknownPrincipal(t *testing.T) {
	var out lines
	obs := recordingObservability(&out)

	mux := http.NewServeMux()
	mux.Handle("GET /auth/providers", obs.logRequests(http.HandlerFunc(
		func(http.ResponseWriter, *http.Request) {})))
	mux.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/auth/providers", nil))

	line := decodeLine(t, &out)
	assert.Equal(t, float64(http.StatusOK), line["status"], "an unwritten status is 200")
	assert.NotContains(t, line, "user_id")
}

// TestLogRequests_ServesAStreamThroughTheRecorder exercises the seam the
// agent watch broke on (#380): a streaming handler behind the logging
// middleware asserts http.Flusher exactly the way WatchAgent does, and
// the statusRecorder the middleware writes through hid it. The request
// runs through a real server rather than httptest.NewRecorder, because
// the recorder is itself a Flusher — which is how the api suite, which
// drives the handler directly, missed the wrapper swallowing the
// capability.
func TestLogRequests_ServesAStreamThroughTheRecorder(t *testing.T) {
	var out lines
	obs := recordingObservability(&out)

	handler := obs.logRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "response cannot stream", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		// A frame the client reads while the stream stays open is what
		// proves the flush reached the wire rather than a buffer.
		_, _ = io.WriteString(w, ": keepalive\n\n")
		flusher.Flush()
	}))
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	resp, err := server.Client().Get(server.URL + "/orgs/acme/agents/bot-1/watch")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	type read struct {
		line string
		err  error
	}
	frame := make(chan read, 1)
	go func() {
		line, err := bufio.NewReader(resp.Body).ReadString('\n')
		frame <- read{line, err}
	}()
	select {
	case got := <-frame:
		require.NoError(t, got.err)
		assert.Equal(t, ": keepalive\n", got.line)
	case <-time.After(2 * time.Second):
		t.Fatal("the stream delivered nothing through the middleware")
	}

	// Recording what the handler wrote is the middleware's own half of
	// the seam, and it must survive streaming.
	assert.Eventually(t, func() bool {
		return strings.Contains(out.String(), `"status":200`)
	}, time.Second, 10*time.Millisecond, "the streamed response was not recorded")
}

// machineListener starts the machine listener behind obs's middleware
// and returns its URL and a client holding a certificate the
// organization's own authority signed for subject. The parts are the
// ones serveMachine assembles.
func machineListener(t *testing.T, obs *observability, subject grn.GRN) (string, *http.Client) {
	t.Helper()
	// The identity endpoint answers out of the request alone, so the
	// listener is assembled with no service behind it.
	return machineListenerServing(t, obs, subject, machineapi.NewServer(nil, nil, nil, nil, nil, nil))
}

// machineListenerServing is machineListener for a test that needs a
// service behind the surface: it starts the same listener around the
// given server.
func machineListenerServing(t *testing.T, obs *observability, subject grn.GRN, server *machineapi.Server) (string, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir)
	pair, _, _, err := machineKeyMaterial(certFile, keyFile, writeKeyEncryptionKey(t, dir), writeServerRoot(t, dir))
	require.NoError(t, err)

	store := authorities{}
	key := make([]byte, ca.KeySize)
	_, err = rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	issuer, err := ca.NewService(store, sealer, ca.Config{})
	require.NoError(t, err)
	issued, err := issuer.Issue(context.Background(), subject)
	require.NoError(t, err)
	client, err := tls.X509KeyPair(issued.CertificatePEM(), issued.PrivateKeyPEM())
	require.NoError(t, err)

	handler := machinegen.HandlerWithOptions(server, machinegen.StdHTTPServerOptions{
		Middlewares: []machinegen.MiddlewareFunc{obs.logMachineRequests},
	})
	srv := machine.NewServer(
		machine.NewService(certificateAuthorities{store: store}),
		pair,
		handler,
	)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })

	// What garam presents is not what is under test; the certificate it
	// asks for is.
	c := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{client},
	}}}
	t.Cleanup(c.CloseIdleConnections)
	return "https://" + lis.Addr().String(), c
}

// TestLogServed_NamesAMachinePrincipalAsOne reads the machine
// listener's line against the browser API's. stack-go.md §Logging &
// observability names user_id when the principal is known and names no
// field for a machine one; putting both under user_id leaves an audit
// log unable to tell an agent from a person, which is the distinction
// ADR-0014 rests on.
//
// The person's line is asserted beside the machine's, because a machine
// line without user_id would hold as readily for a line that names no
// principal at all, and because the two fields have to stay apart in
// both directions.
func TestLogServed_NamesAMachinePrincipalAsOne(t *testing.T) {
	subject := machineGRN("acme", "bot-1")

	var served lines
	url, client := machineListener(t, recordingObservability(&served), subject)
	resp, err := client.Get(url + "/identity")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	machineLine := decodeLine(t, &served)
	assert.Equal(t, subject.String(), machineLine[fieldMachineID])
	assert.NotContains(t, machineLine, fieldUserID)

	var byUser lines
	obs := recordingObservability(&byUser)
	mux := http.NewServeMux()
	mux.Handle("GET /orgs", obs.logRequests(http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) { api.PrincipalFrom(r).GRN = testSubject })))
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orgs", nil))

	userLine := decodeLine(t, &byUser)
	assert.Equal(t, testSubject, userLine[fieldUserID])
	assert.NotContains(t, userLine, fieldMachineID)
}

func TestLogRPCs_ReportsTheMethodAndTheSubject(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		code  string
		level string
		cause string
	}{
		{name: "served", code: codes.OK.String(), level: slog.LevelInfo.String()},
		{
			name:  "refused",
			err:   status.Error(codes.PermissionDenied, "denied"),
			code:  codes.PermissionDenied.String(),
			level: slog.LevelError.String(),
			// A refusal carries its reason to the caller, so the line
			// takes no cause: the rule names the 5xx case only.
			cause: "",
		},
		{
			name:  "fault",
			err:   status.Error(codes.Internal, "policy store: deadlock detected"),
			code:  codes.Internal.String(),
			level: slog.LevelError.String(),
			cause: "policy store: deadlock detected",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out lines
			obs := recordingObservability(&out)

			_, err := obs.logRPCs(
				context.Background(),
				&authzv1.CheckRequest{SubjectGrn: testSubject},
				&grpc.UnaryServerInfo{FullMethod: "/garam.authz.v1.AuthzService/Check"},
				func(context.Context, any) (any, error) { return &authzv1.CheckResponse{}, tt.err },
			)
			assert.Equal(t, tt.err, err)

			line := decodeLine(t, &out)
			assert.Equal(t, "/garam.authz.v1.AuthzService/Check", line["operation"])
			assert.Equal(t, testSubject, line["user_id"])
			assert.Equal(t, tt.code, line["code"])
			assert.Equal(t, tt.level, line["level"])
			assert.NotEmpty(t, line["trace_id"])
			if tt.cause == "" {
				assert.NotContains(t, line, "error")
			} else {
				assert.Equal(t, tt.cause, line["error"],
					"a fault of this process leaves its chain on the line")
			}
		})
	}
}

// lineForSubject runs one Check about subject through the interceptor
// and returns the line it logged.
func lineForSubject(t *testing.T, subject grn.GRN) map[string]any {
	t.Helper()
	var out lines
	obs := recordingObservability(&out)
	_, err := obs.logRPCs(
		context.Background(),
		&authzv1.CheckRequest{SubjectGrn: subject.String()},
		&grpc.UnaryServerInfo{FullMethod: "/garam.authz.v1.AuthzService/Check"},
		func(context.Context, any) (any, error) { return &authzv1.CheckResponse{}, nil },
	)
	require.NoError(t, err)
	return decodeLine(t, &out)
}

// TestLogRPCs_NamesAMachineSubjectAsOne reads the engine's line for a
// check made on behalf of a machine against its line for one made on
// behalf of a person. The engine matches subject strings and does not
// validate that one names a user or a group (ADR-0008), so an agent GRN
// is a valid subject and a check about it would otherwise be logged as
// though a person made it — the distinction ADR-0014 rests on.
//
// The person's line is asserted beside the machine's, because a machine
// line without user_id would hold as readily for a line that names no
// principal at all, and because the two fields have to stay apart in
// both directions. The person is named by a well-formed user GRN, so
// what separates the lines is the service type rather than a string the
// parse rejects.
func TestLogRPCs_NamesAMachineSubjectAsOne(t *testing.T) {
	for _, subject := range []grn.GRN{
		machineGRN("acme", "bot-1"),
		grn.New("acme", grn.DefaultProject, agent.ServiceTypeOperator, "op-1"),
	} {
		line := lineForSubject(t, subject)
		assert.Equal(t, subject.String(), line[fieldMachineID])
		assert.NotContains(t, line, fieldUserID)
	}

	person := grn.New("acme", grn.DefaultProject, user.ServiceType, "alice")
	personLine := lineForSubject(t, person)
	assert.Equal(t, person.String(), personLine[fieldUserID])
	assert.NotContains(t, personLine, fieldMachineID)
}

// noGroups is the machineapi.Groups of a caller no group holds.
type noGroups struct{}

func (noGroups) List(context.Context, grn.GRN) ([]grn.GRN, error) { return nil, nil }

// unreachableEngine is a machineapi.Evaluator that cannot answer: the
// failure #255 could not read out of the machine listener's logs.
type unreachableEngine struct{ err error }

func (e unreachableEngine) Evaluate(context.Context, machineapi.EvalInput) (bool, error) {
	return false, e.err
}

// deniedEngine is a machineapi.Evaluator answering a refusal.
type deniedEngine struct{}

func (deniedEngine) Evaluate(context.Context, machineapi.EvalInput) (bool, error) { return false, nil }

// TestLogServed_CarriesTheCauseOfAMachineFailure is the machine
// surface's half of the rule in runtime-safety.md §Errors reach a
// handler: a handled error that becomes a 5xx leaves its chain on the
// request's line, and the client body still withholds it. The refused
// send sits beside it as the control, because a line carrying a cause
// on every failure would read exactly like one carrying it on a 5xx,
// and a refusal's reason already reaches its caller.
func TestLogServed_CarriesTheCauseOfAMachineFailure(t *testing.T) {
	sender := machineGRN("acme", "bot-1")
	recipient := machineGRN("acme", "bot-2")
	submission := `{"recipient":"` + recipient.String() + `","idempotencyKey":"key-1","body":"x"}`
	cause := errorx.Wrap(errorx.New(errorx.KindInternal, "dial tcp: connection refused"), "authz engine")

	var out lines
	url, client := machineListenerServing(t, recordingObservability(&out), sender,
		machineapi.NewServer(nil, nil, nil, nil, noGroups{}, unreachableEngine{err: cause}))

	resp, err := client.Post(url+"/messages", "application/json", strings.NewReader(submission))
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.JSONEq(t, `{"kind":"internal","message":"internal error"}`, string(body),
		"the client body still withholds the cause")

	line := decodeLine(t, &out)
	assert.Equal(t, "authz engine: dial tcp: connection refused", line["error"],
		"the line carries the whole chain")
	assert.Equal(t, float64(http.StatusInternalServerError), line["status"])
	assert.NotEmpty(t, line["trace_id"])

	var refusedOut lines
	url, client = machineListenerServing(t, recordingObservability(&refusedOut), sender,
		machineapi.NewServer(nil, nil, nil, nil, noGroups{}, deniedEngine{}))
	resp, err = client.Post(url+"/messages", "application/json", strings.NewReader(submission))
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusForbidden, resp.StatusCode)
	assert.NotContains(t, decodeLine(t, &refusedOut), "error")
}

// brokenSessions is an authn.SessionStore whose Delete fails the way
// a store does when the database under it drops.
type brokenSessions struct {
	authn.SessionStore
	err error
}

func (s brokenSessions) Delete(context.Context, string) error { return s.err }

// TestLogServed_CarriesTheCauseOfAnAPIFailure is the api surface's
// half of the same rule, driven through a real handler rather than a
// stub so the surface's own error writer is what records the cause.
// The 401 beside it is the control: a refusal whose reason the caller
// holds leaves no cause on the line.
func TestLogServed_CarriesTheCauseOfAnAPIFailure(t *testing.T) {
	cause := errorx.Wrap(errorx.New(errorx.KindInternal, "connection reset by peer"), "session delete")
	srv := api.NewServer(nil, nil, nil, nil, nil, brokenSessions{err: cause}, nil, nil, nil, nil, nil, nil, api.Config{})

	var out lines
	mux := http.NewServeMux()
	mux.Handle("POST /auth/logout", recordingObservability(&out).logRequests(http.HandlerFunc(srv.Logout)))

	req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "garam_session", Value: "tok-1"})
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.JSONEq(t, `{"kind":"internal","message":"internal error"}`, recorder.Body.String(),
		"the client body still withholds the cause")

	line := decodeLine(t, &out)
	assert.Equal(t, "session delete: connection reset by peer", line["error"],
		"the line carries the whole chain")
	assert.Equal(t, float64(http.StatusInternalServerError), line["status"])
	assert.NotEmpty(t, line["trace_id"])

	var refusedOut lines
	refusedMux := http.NewServeMux()
	refusedMux.Handle("POST /auth/logout", recordingObservability(&refusedOut).logRequests(http.HandlerFunc(srv.Logout)))
	refusedRecorder := httptest.NewRecorder()
	refusedMux.ServeHTTP(refusedRecorder, httptest.NewRequest(http.MethodPost, "/auth/logout", nil))
	require.Equal(t, http.StatusUnauthorized, refusedRecorder.Code)
	assert.NotContains(t, decodeLine(t, &refusedOut), "error")
}

// stubEngine answers Check so the interceptor has a call to observe.
type stubEngine struct {
	authzv1.UnimplementedAuthzServiceServer
}

func (stubEngine) Check(context.Context, *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	return &authzv1.CheckResponse{}, nil
}

func TestLogRPCs_ReportsTheCallersTraceID(t *testing.T) {
	// Without the same id on both lines the two halves of a request
	// cannot be joined, which is the whole reason the id is logged.
	_, err := newObservability("garam-test")
	require.NoError(t, err)

	var out lines
	obs := recordingObservability(&out)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	engine := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(obs.logRPCs),
	)
	authzv1.RegisterAuthzServiceServer(engine, stubEngine{})
	go func() { _ = engine.Serve(lis) }()
	t.Cleanup(engine.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	ctx, span := otel.Tracer("garam-test").Start(context.Background(), "call")
	defer span.End()
	_, err = authzv1.NewAuthzServiceClient(conn).Check(ctx, &authzv1.CheckRequest{SubjectGrn: testSubject})
	require.NoError(t, err)

	assert.Equal(t, traceIDOf(ctx), decodeLine(t, &out)["trace_id"])
}

func TestTraceIDOf_PrefersTheSpan(t *testing.T) {
	id := trace.TraceID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: id,
		SpanID:  trace.SpanID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
	}))

	assert.Equal(t, id.String(), traceIDOf(ctx))
}

func TestTraceIDOf_FallsBackToAUUID(t *testing.T) {
	_, err := uuid.Parse(traceIDOf(context.Background()))
	assert.NoError(t, err)
}
