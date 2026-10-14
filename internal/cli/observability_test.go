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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/pgxerr"
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

// everyRecordHeld admits every identity, because what a withdrawn
// record does to a handshake is internal/machine's and these tests are
// about the wrapping this file assembles around it (ADR-0070).
type everyRecordHeld struct{}

func (everyRecordHeld) Holds(context.Context, grn.GRN) error { return nil }

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

// recordingObservability returns one whose lines land in out. The
// logger is the one a serve command builds, so what a line carries
// without being asked is what it carries here; the registry is the
// empty one a server would collect into, because the handlers the
// serve commands assemble mount it.
func recordingObservability(out *lines) *observability {
	return &observability{
		logger:   newLogger(out, "garam-test"),
		registry: prometheus.NewRegistry(),
	}
}

// decodeLines parses every line out holds, in the order they were
// written.
func decodeLines(t *testing.T, out *lines) []map[string]any {
	t.Helper()
	var decoded []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var line map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &line))
		decoded = append(decoded, line)
	}
	return decoded
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

// TestLogRequests_LevelsTheLineByWhoseFailureItIs covers the level
// rather than the fields. A 5xx is the server's own failure and is what
// an operator is paged on, so the line carries error; everything below
// it is an answer the server meant to give, so the line carries info.
// The two are asserted together because each is what the other cannot
// see: a level read backwards is wrong on both, and one taking 500 for
// an ordinary answer is wrong only on the first.
func TestLogRequests_LevelsTheLineByWhoseFailureItIs(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		want   string
	}{
		"the first status the server failed on": {http.StatusInternalServerError, "ERROR"},
		"a refusal the server meant to give":    {http.StatusNotFound, "INFO"},
	} {
		t.Run(name, func(t *testing.T) {
			var out lines
			obs := recordingObservability(&out)

			mux := http.NewServeMux()
			mux.Handle("GET /orgs", obs.logRequests(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status) })))
			mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orgs", nil))

			line := decodeLine(t, &out)
			assert.Equal(t, float64(tc.status), line["status"])
			assert.Equal(t, tc.want, line["level"])
		})
	}
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
	return machineListenerServing(t, obs, subject, machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))
}

// machineListenerServing is machineListener for a test that needs a
// service behind the surface: it starts the same listener around the
// given server.
func machineListenerServing(t *testing.T, obs *observability, subject grn.GRN, server *machineapi.Server) (string, *http.Client) {
	t.Helper()
	return machineListenerAround(t, obs, subject, machinegen.HandlerWithOptions(server,
		machinegen.StdHTTPServerOptions{
			Middlewares: []machinegen.MiddlewareFunc{machineapi.RecoverPanics, obs.logMachineRequests},
		}))
}

// machineListenerAround starts serveMachine's own listener around next,
// on a port of the kernel's choosing, and returns its URL and a client
// holding a certificate the organization's own authority signed for
// subject. Everything but the address is what serveMachine assembles,
// because machineServer is what assembles it.
func machineListenerAround(t *testing.T, obs *observability, subject grn.GRN, next http.Handler) (string, *http.Client) {
	t.Helper()
	dir := t.TempDir()
	certFile, keyFile := writeTLSKeyPair(t, dir)
	keys, err := machineKeyMaterial(certFile, keyFile, writeKeyEncryptionKey(t, dir), writeServerRoot(t, dir))
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

	srv := obs.machineServer(machine.NewService(certificateAuthorities{store: store}, everyRecordHeld{}, machine.Config{}),
		identityTokens{operators: liveTokens{}}, keys.listener, next)
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
		machineapi.NewServer(nil, nil, nil, nil, nil, noGroups{}, unreachableEngine{err: cause}, nil, nil, nil, nil, nil, nil))

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
		machineapi.NewServer(nil, nil, nil, nil, nil, noGroups{}, deniedEngine{}, nil, nil, nil, nil, nil, nil))
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
	srv := api.NewServer(nil, nil, nil, nil, nil, nil, brokenSessions{err: cause}, nil, nil, nil, nil, nil, nil, nil, nil, nil, api.Config{})

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

// driverConflict is a duplicate row as a store here answers one:
// pgxerr.Classify over the driver's own error, which names the
// constraint, the table and the SQLSTATE. #733 stopped that text
// reaching a client, and nothing replaced it on the server's side.
func driverConflict(op string) error {
	return pgxerr.Classify(&pgconn.PgError{
		Severity:       "ERROR",
		Code:           "23505",
		Message:        `duplicate key value violates unique constraint "sessions_pkey"`,
		ConstraintName: "sessions_pkey",
		TableName:      "sessions",
	}, op, nil, nil)
}

// driverConflictText is what that error's whole chain reads as. The
// question #1064 opens with — which constraint, on which table, under
// which SQLSTATE — is answerable from this string and from nothing
// else the line carries.
const driverConflictText = `delete session: ERROR: duplicate key value ` +
	`violates unique constraint "sessions_pkey" (SQLSTATE 23505)`

// TestLogServed_CarriesTheCauseAnAPIAnswerWithheld is the half of
// runtime-safety.md §Errors reach a handler that #733 left without a
// record. Below 500 the client reads the kind's own sentence wherever
// the chain reaches an error from outside this project, so the
// constraint that refused the request is on this line or nowhere.
//
// The rows are read against each other. The authored refusal answers
// the same status from the same route, so what puts the cause on the
// line is the chain and not the status; the served answer is the
// control that keeps this from passing on a line that carries a cause
// for every request, which would point at nothing. Each row asserts
// the body exactly, because what the client reads may not move.
func TestLogServed_CarriesTheCauseAnAPIAnswerWithheld(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		body   string
		cause  string
	}{
		{
			name:   "a conflict the driver raised",
			err:    driverConflict("delete session"),
			status: http.StatusConflict,
			body:   `{"kind":"already_exists","message":"already exists"}`,
			cause:  driverConflictText,
		},
		{
			name:   "a refusal this project wrote",
			err:    errorx.New(errorx.KindAlreadyExists, "the session is already gone"),
			status: http.StatusConflict,
			body:   `{"kind":"already_exists","message":"the session is already gone"}`,
		},
		{
			name:   "an answer the server meant to give",
			status: http.StatusNoContent,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := api.NewServer(nil, nil, nil, nil, nil, nil, brokenSessions{err: tt.err},
				nil, nil, nil, nil, nil, nil, nil, nil, nil, api.Config{})

			var out lines
			mux := http.NewServeMux()
			mux.Handle("POST /auth/logout", recordingObservability(&out).logRequests(http.HandlerFunc(srv.Logout)))
			req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
			req.AddCookie(&http.Cookie{Name: "garam_session", Value: "tok-1"})
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, req)

			require.Equal(t, tt.status, recorder.Code)
			if tt.body == "" {
				require.Empty(t, recorder.Body.String(), "204 writes no body")
			} else {
				assert.JSONEq(t, tt.body, recorder.Body.String(), "what the client reads")
			}

			line := decodeLine(t, &out)
			assert.Equal(t, float64(tt.status), line["status"])
			assert.Equal(t, slog.LevelInfo.String(), line["level"],
				"a 4xx is an answer the server meant to give, cause or no cause")
			if tt.cause == "" {
				assert.NotContains(t, line, "error")
				return
			}
			assert.Equal(t, tt.cause, line["error"], "the line names the constraint and the SQLSTATE")
		})
	}
}

// TestLogServed_CarriesTheCauseAMachineAnswerWithheld is the machine
// listener's half of the same rule, driven through the real listener
// so the surface's own error writer is what records the cause. The
// rows are the ones the browser API's test holds, chosen from what
// this listener can refuse: a policy the engine could not read
// against one it refused, and an answered request.
func TestLogServed_CarriesTheCauseAMachineAnswerWithheld(t *testing.T) {
	sender := machineGRN("acme", "bot-1")
	recipient := machineGRN("acme", "bot-2")
	submission := `{"recipient":"` + recipient.String() + `","idempotencyKey":"key-1","body":"x"}`

	tests := []struct {
		name   string
		engine machineapi.Evaluator
		status int
		body   string
		cause  string
	}{
		{
			name:   "a conflict the driver raised",
			engine: unreachableEngine{err: driverConflict("delete session")},
			status: http.StatusConflict,
			body:   `{"kind":"already_exists","message":"already exists"}`,
			cause:  driverConflictText,
		},
		{
			name:   "a refusal this project wrote",
			engine: deniedEngine{},
			status: http.StatusForbidden,
			body:   `{"kind":"permission_denied","message":"sender may not send to this recipient"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out lines
			url, client := machineListenerServing(t, recordingObservability(&out), sender,
				machineapi.NewServer(nil, nil, nil, nil, nil, noGroups{}, tt.engine, nil, nil, nil, nil, nil, nil))

			resp, err := client.Post(url+"/messages", "application/json", strings.NewReader(submission))
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())

			require.Equal(t, tt.status, resp.StatusCode)
			assert.JSONEq(t, tt.body, string(body), "what the caller reads")

			line := decodeLine(t, &out)
			assert.Equal(t, float64(tt.status), line["status"])
			assert.Equal(t, slog.LevelInfo.String(), line["level"])
			if tt.cause == "" {
				assert.NotContains(t, line, "error")
				return
			}
			assert.Equal(t, tt.cause, line["error"])
		})
	}

	// The control the two rows cannot supply between them: a request
	// this listener answered leaves a line with no cause at all.
	t.Run("an answer the server meant to give", func(t *testing.T) {
		var out lines
		url, client := machineListener(t, recordingObservability(&out), sender)
		resp, err := client.Get(url + "/identity")
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)

		assert.NotContains(t, decodeLine(t, &out), "error")
	})
}

// enginePolicies is an authz.PolicySource answering every read with
// err. The engine's own error writer is what the interceptor then
// reads, so what is under test is the shape grpcError produces rather
// than one assembled here.
type enginePolicies struct{ err error }

func (p enginePolicies) Statements(context.Context, grn.GRN, []grn.GRN) ([]authz.Statement, error) {
	return nil, p.err
}

func (p enginePolicies) CurrentStatements(context.Context, grn.GRN, []grn.GRN) ([]authz.Statement, error) {
	return nil, p.err
}

// TestLogRPCs_CarriesTheCauseTheEngineWithheld is the engine's half.
// It answers with wire.MessageOf too (#877), so a chain reaching
// outside this project reads to the caller as the kind's sentence and
// survives on this line alone — for a code that is no fault of this
// process as much as for one that is.
//
// The rows share a route and a kind and differ in who wrote the
// chain, and the answered check is the control: an interceptor
// carrying a cause on every call would pass both refusals.
func TestLogRPCs_CarriesTheCauseTheEngineWithheld(t *testing.T) {
	object := grn.New(grn.RootOwner, grn.DefaultProject, org.ServiceTypeOrg, "acme")
	tests := []struct {
		name    string
		err     error
		code    codes.Code
		message string
		cause   string
	}{
		{
			name:    "a conflict the driver raised",
			err:     driverConflict("delete session"),
			code:    codes.AlreadyExists,
			message: "already exists",
			cause:   driverConflictText,
		},
		{
			name:    "a refusal this project wrote",
			err:     errorx.New(errorx.KindAlreadyExists, "the policy is already attached"),
			code:    codes.AlreadyExists,
			message: "the policy is already attached",
		},
		{
			name: "a decision the engine reached",
			code: codes.OK,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out lines
			engine := authz.NewGRPCServer(enginePolicies{err: tt.err})

			_, err := recordingObservability(&out).logRPCs(
				context.Background(),
				&authzv1.CheckRequest{
					SubjectGrn: testSubject,
					ObjectGrn:  object.String(),
					SubjectOrg: object.String(),
					Action:     "org:read",
				},
				&grpc.UnaryServerInfo{FullMethod: "/garam.authz.v1.AuthzService/Check"},
				func(ctx context.Context, req any) (any, error) {
					return engine.Check(ctx, req.(*authzv1.CheckRequest))
				},
			)

			require.Equal(t, tt.code, status.Code(err))
			assert.Equal(t, tt.message, status.Convert(err).Message(), "what the peer reads")

			line := decodeLine(t, &out)
			assert.Equal(t, tt.code.String(), line["code"])
			if tt.cause == "" {
				assert.NotContains(t, line, "error")
				return
			}
			assert.Equal(t, tt.cause, line["error"])
		})
	}
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

// TestTraceIDOf_FallsBackToAUUID holds the fallback against what a
// spanless context renders without one. An empty SpanContext renders a
// trace id of thirty-two zeroes, which uuid.Parse accepts in its
// undashed form, so parsing alone passes with the fallback deleted. Two
// calls differing is the other half: a fallback shared between calls
// would correlate log lines that have nothing to do with each other.
func TestTraceIDOf_FallsBackToAUUID(t *testing.T) {
	first, second := traceIDOf(context.Background()), traceIDOf(context.Background())

	_, err := uuid.Parse(first)
	assert.NoError(t, err)
	assert.NotEqual(t, trace.TraceID{}.String(), first, "the zero trace id is what no fallback renders")
	assert.NotEqual(t, first, second)
}

// panickingSessions is an authn.SessionStore whose Delete is the
// programmer bug a recover middleware exists for.
type panickingSessions struct{ authn.SessionStore }

func (panickingSessions) Delete(context.Context, string) error { panic("session store defect") }

// TestLogRequests_ReportsARecoveredPanicOnce holds the order the serve
// commands list the two middlewares in. Recovery is innermost, so the
// panic becomes a 500 this line reports; reversed, it unwinds past
// logServed and decodeLine finds no line at all. The cause carries the
// stack because net/http, having never seen the panic, no longer logs
// one. logMachineRequests shares logServed, so the machine listener is
// the same wiring.
//
// The request runs through apiHandler, which makes the count the second
// thing this test holds: the surface's middleware answers first, so the
// server-level one that assembly wraps it in writes nothing and only
// one line is emitted. decodeLine parses the whole buffer, so a second
// line fails it.
func TestLogRequests_ReportsARecoveredPanicOnce(t *testing.T) {
	var out lines
	obs := recordingObservability(&out)
	srv := api.NewServer(nil, nil, nil, nil, nil, nil, panickingSessions{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, api.Config{})
	server := httptest.NewServer(obs.apiHandler(srv))
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodPost, server.URL+"/auth/logout", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "garam_session", Value: "tok-1"})
	resp, err := server.Client().Do(req)
	require.NoError(t, err, "the client read no answer at all")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.JSONEq(t, `{"kind":"internal","message":"internal error"}`, string(body),
		"the client body still withholds the cause")

	line := decodeLine(t, &out)
	assert.Equal(t, "POST /auth/logout", line["operation"])
	assert.Equal(t, float64(http.StatusInternalServerError), line["status"])
	assert.Contains(t, line["error"], "panic: session store defect")
	assert.Contains(t, line["error"], "panickingSessions.Delete",
		"the stack net/http no longer logs travels in the cause")
}

// TestNewLogger_JoinsAHandlersLineToTheRequestItRanIn covers the
// trace_id stack-go.md §Logging & observability asks handler lines to
// carry. The surfaces write their error-path lines through log/slog
// with no id of their own — internal/api and internal/machineapi may
// not import this package to mint one — so the field is the logger's to
// supply or nobody's.
//
// Equality with the request's line is the assertion rather than
// presence: an id nothing else carries joins nothing, which is what a
// per-line fallback would leave behind.
func TestNewLogger_JoinsAHandlersLineToTheRequestItRanIn(t *testing.T) {
	restore := slog.Default()
	t.Cleanup(func() { slog.SetDefault(restore) })
	// The tracer provider is what mints the span the ids come from, and
	// newObservability is what installs it.
	_, err := newObservability("garam-test")
	require.NoError(t, err)

	var out lines
	obs := recordingObservability(&out)
	slog.SetDefault(obs.logger)

	mux := http.NewServeMux()
	mux.Handle("GET /agents/{agent}/watch", obs.logRequests(http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) {
			slog.WarnContext(r.Context(), "watch not refreshed")
		})))
	server := httptest.NewServer(otelhttp.NewHandler(mux, "garam-test"))
	t.Cleanup(server.Close)

	resp, err := server.Client().Get(server.URL + "/agents/bot-1/watch")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	written := decodeLines(t, &out)
	require.Len(t, written, 2, "the handler's line and the request's")
	handled, served := written[0], written[1]
	require.Equal(t, "watch not refreshed", handled["msg"])
	require.Equal(t, "http request", served["msg"])
	assert.NotEmpty(t, served["trace_id"], "the request's own line still carries one")
	assert.Equal(t, served["trace_id"], handled["trace_id"],
		"the handler's line joins the request rather than minting an id of its own")
}

// invitationOrgs is the slice of org.Store the invitation read
// reaches: one live invitation and its organization, and a store fault
// for every other token.
type invitationOrgs struct {
	org.Store
	token string
	fault error
}

func (s invitationOrgs) GetInvitation(_ context.Context, token string) (*org.Invitation, error) {
	switch token {
	case s.token:
		return org.NewInvitation(token, grn.New("root", "default", org.ServiceTypeOrg, "acme"),
			org.RoleMember, grn.New("root", "default", user.ServiceType, "owner"), time.Now().Add(time.Hour)), nil
	case "unknown-tok-secret":
		return nil, org.ErrInvitationNotFound
	}
	return nil, s.fault
}

func (invitationOrgs) GetOrg(_ context.Context, g grn.GRN) (*org.Organization, error) {
	return org.NewOrganization(g, "Acme"), nil
}

// TestLogRequests_KeepsAnInvitationTokenOffTheLine drives the
// unauthenticated invitation read through apiHandler, the router `serve
// api` builds, for its three answers: found, unknown, and a store fault
// whose cause the line carries. The path is the token, so the line
// naming the route pattern is what keeps the token out; the operation
// asserted on each line is the control that a line was written for
// this route at all.
func TestLogRequests_KeepsAnInvitationTokenOffTheLine(t *testing.T) {
	tests := []struct {
		name   string
		token  string
		status int
	}{
		{name: "found", token: "live-tok-secret", status: http.StatusOK},
		{name: "unknown", token: "unknown-tok-secret", status: http.StatusNotFound},
		{name: "fault", token: "fault-tok-secret", status: http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out lines
			orgs := invitationOrgs{
				token: "live-tok-secret",
				fault: errorx.Wrap(errorx.New(errorx.KindInternal, "connection reset by peer"), "get invitation"),
			}
			srv := api.NewServer(nil, orgs, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, api.Config{})
			server := httptest.NewServer(recordingObservability(&out).apiHandler(srv))
			t.Cleanup(server.Close)

			resp, err := server.Client().Get(server.URL + "/invitations/" + tc.token)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, tc.status, resp.StatusCode)

			line := decodeLine(t, &out)
			assert.Equal(t, "GET /invitations/{token}", line["operation"])
			assert.Equal(t, float64(tc.status), line["status"])
			assert.NotContains(t, out.String(), tc.token)
		})
	}
}

// TestAPIHandler_KeepsAnInvitationTokenOffTheSpan reads the spans
// apiHandler records for the invitation read: otelhttp takes url.path
// from the concrete path, which is the token, and a configured exporter
// would ship it. The span is found by its route before the search, so
// an empty recording cannot pass for a clean one.
func TestAPIHandler_KeepsAnInvitationTokenOffTheSpan(t *testing.T) {
	recorded := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorded)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	var out lines
	orgs := invitationOrgs{token: "live-tok-secret"}
	srv := api.NewServer(nil, orgs, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, api.Config{})
	server := httptest.NewServer(recordingObservability(&out).apiHandler(srv))
	t.Cleanup(server.Close)

	resp, err := server.Client().Get(server.URL + "/invitations/live-tok-secret")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode)

	spans := recorded.Ended()
	require.Len(t, spans, 1)
	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	assert.Equal(t, "/invitations/{token}", attrs["url.path"])
	for key, value := range attrs {
		assert.NotContains(t, value, "live-tok-secret", "attribute %s", key)
	}
	assert.NotContains(t, spans[0].Name(), "live-tok-secret")
}
