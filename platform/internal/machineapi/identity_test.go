package machineapi_test

import (
	"crypto/tls"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/machineapi"
)

// anonymous is a client presenting no token at all.
func anonymous(t *testing.T) *http.Client {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		//nolint:gosec // the listener's own certificate is not under test.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}}}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// routeRecorder is a machinegen.ServeMux that records the pattern of
// every route registered on it. The registration is the whole of what
// it is for and nothing is served through it, so ServeHTTP panics
// through the embedded interface.
type routeRecorder struct {
	http.Handler
	patterns []string
}

func (m *routeRecorder) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	m.patterns = append(m.patterns, pattern)
}

// machineRoute is one operation as a caller sends it: the method, and
// the path with every parameter the specification names filled in.
type machineRoute struct{ method, path string }

// machineRoutes returns every route the generated server registers,
// keyed by the pattern it was registered under and with each path
// parameter replaced by what values gives for it. The set is read off
// that registration rather than listed: api/machine.yaml is what the
// generator reads, so an operation added there arrives here without
// anyone remembering to add it.
//
// How many there are is read a second way, off the generated
// interface, which declares one method per operation. A recording that
// dropped a registration would otherwise leave a smaller set that
// still passes, which is the shape of the defect this derivation is
// here to remove.
func machineRoutes(t *testing.T, values map[string]string) map[string]machineRoute {
	t.Helper()
	// The handler is never called, only wrapped, so there is nothing
	// for it to answer from.
	recorder := &routeRecorder{}
	machinegen.HandlerWithOptions((*machineapi.Server)(nil),
		machinegen.StdHTTPServerOptions{BaseRouter: recorder})

	routes := make(map[string]machineRoute, len(recorder.patterns))
	for _, pattern := range recorder.patterns {
		method, path, ok := strings.Cut(pattern, " ")
		require.True(t, ok, "the pattern %q names no method", pattern)
		routes[pattern] = machineRoute{method: method, path: fillPath(t, path, values)}
	}
	operations := reflect.TypeOf((*machinegen.ServerInterface)(nil)).Elem().NumMethod()
	require.Len(t, routes, operations,
		"every operation the generated interface declares registers a route")
	return routes
}

// fillPath replaces each {parameter} in path with what values gives
// for it, and fails when it gives nothing. A parameter nothing names
// is a route this test would send somewhere else, and a route sent
// somewhere else is refused for the wrong reason.
func fillPath(t *testing.T, path string, values map[string]string) string {
	t.Helper()
	for {
		opened := strings.Index(path, "{")
		if opened < 0 {
			return path
		}
		closed := strings.Index(path[opened:], "}")
		require.Positive(t, closed, "the path %q opens a parameter it never closes", path)
		name := path[opened+1 : opened+closed]
		value, ok := values[name]
		require.True(t, ok, "the path parameter %q in %q has no value here", name, path)
		path = path[:opened] + value + path[opened+closed+1:]
	}
}

// standaloneEnrollmentRoute, the two exchanges and keySetRoute are the
// operations reached without a token (ADR-0082, ADR-0100,
// ADR-0101, ADR-0102). They are named here so that what a reader
// checks is the exemption rather than the routes that are not exempt,
// which are whatever is left.
const (
	standaloneEnrollmentRoute = http.MethodPost + " /agent-enrollments"
	tokenExchangeRoute        = http.MethodPost + " /tokens"
	assertionExchangeRoute    = http.MethodPost + " /oauth/token"
	keySetRoute               = http.MethodGet + " /message-signing-keys"
)

// TestServer_TheExemptRoutesAreTheOnlyOnesAnUnauthenticatedCallerReaches
// holds that a client presenting no token reaches the standalone
// enrollment, the two exchanges and the key set, each refusing on its
// own credential or answering, and is refused everywhere else, so what
// admitted it was the route and not a listener that stopped checking.
// No route enrols an operator (ADR-0117).
func TestServer_TheExemptRoutesAreTheOnlyOnesAnUnauthenticatedCallerReaches(t *testing.T) {
	caller := operatorGRN("acme")
	url, _ := listener(t, &agentService{}, nil, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, caller)
	client := anonymous(t)

	// Every operation api/machine.yaml declares, read off the generated
	// registration rather than listed, so a route added without the
	// guard is caught here rather than in production: the listener
	// stopped making this refusal centrally, and each handler makes it
	// now.
	routes := machineRoutes(t, map[string]string{
		"agent":      agentGRN("acme", "0a1b2c3d4e5f6071").String(),
		"claim":      "c-1",
		"controller": operatorGRN("acme").String(),
		"message":    "m-1",
		"ref":        "ref-1",
		"activation": "00000000-0000-0000-0000-000000000001",
		"approval":   "00000000-0000-0000-0000-000000000002",
	})
	// The standalone enrollment refuses on its own credential, the
	// bootstrap secret, rather than on the missing token.
	standalone, ok := routes[standaloneEnrollmentRoute]
	require.True(t, ok, "the exemption names no operation the specification declares")
	delete(routes, standaloneEnrollmentRoute)
	status, body := send(t, client, standalone.method, url+standalone.path, "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, "standalone bootstrap")

	// So is the token exchange, which refuses on its own credential,
	// a supervisor's, rather than on the missing token.
	exchange, ok := routes[tokenExchangeRoute]
	require.True(t, ok, "the exemption names no operation the specification declares")
	delete(routes, tokenExchangeRoute)
	status, body = send(t, client, exchange.method, url+exchange.path, "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, "Garam-Credential")

	// So is the client assertion exchange, which refuses on its own
	// form, carrying the assertion, rather than on a token.
	assertion, ok := routes[assertionExchangeRoute]
	require.True(t, ok, "the exemption names no operation the specification declares")
	delete(routes, assertionExchangeRoute)
	status, body = send(t, client, assertion.method, url+assertion.path, "")
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, body, `"error":"invalid_request"`)

	// The key set is public: a verifier anywhere reads it (ADR-0101).
	keySet, ok := routes[keySetRoute]
	require.True(t, ok, "the exemption names no operation the specification declares")
	delete(routes, keySetRoute)
	status, body = send(t, client, keySet.method, url+keySet.path, "")
	assert.Equal(t, http.StatusOK, status)
	assert.Contains(t, body, `"kid":"`+string(stubKeyID)+`"`)

	// No body is sent, because a handler that reads one before it reads
	// the identity has already done work for a caller it has not
	// identified.
	for pattern, route := range routes {
		t.Run(pattern, func(t *testing.T) {
			status, body := send(t, client, route.method, url+route.path, "")
			assert.Equal(t, http.StatusUnauthorized, status)
			assert.Contains(t, body, "no machine identity")
		})
	}
}
