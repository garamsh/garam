package machineapi_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/operator"
)

// operatorService is an operator.Service recording what the surface
// passed it: the credential it read off the connection, and the token
// and request it read off an enrollment body. Minting out of band is a
// command's path (ADR-0032) and reaches no handler, so it panics
// through the embedded interface.
type operatorService struct {
	operator.Service
	certificate *operator.Certificate
	err         error
	renewed     []operator.Credential
	enrolled    []enrolment
}

// enrolment is one call the enrollment route made, as the domain saw
// it.
type enrolment struct {
	token   string
	request operator.CertificateRequest
}

func (s *operatorService) Renew(_ context.Context, c operator.Credential) (*operator.IssuedCertificate, error) {
	s.renewed = append(s.renewed, c)
	if s.err != nil {
		return nil, s.err
	}
	return &operator.IssuedCertificate{
		Certificate:   *s.certificate,
		PrivateKeyPEM: []byte("private key"),
	}, nil
}

func (s *operatorService) Enroll(_ context.Context, token string, request operator.CertificateRequest) (*operator.Certificate, error) {
	s.enrolled = append(s.enrolled, enrolment{token: token, request: request})
	if s.err != nil {
		return nil, s.err
	}
	return s.certificate, nil
}

// anonymous is a client presenting no certificate at all, which is
// what an operator that has never been issued one has.
func anonymous(t *testing.T) *http.Client {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		//nolint:gosec // the listener's own certificate is not what is under test.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}}}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// TestServer_EnrolsWithoutACertificateAndAnswersNoPrivateKey is the
// enrollment route seen from the wire: a caller holding no certificate
// reaches it, what it passes on is the token and the request from the
// body, and what comes back has no private key in it — garam signed a
// key it never held.
func TestServer_EnrolsWithoutACertificateAndAnswersNoPrivateKey(t *testing.T) {
	enrolling := operatorGRN("acme")
	notAfter := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	operators := &operatorService{certificate: &operator.Certificate{
		Subject:        enrolling,
		Serial:         "first",
		CertificatePEM: []byte("certificate"),
		IssuerPEM:      []byte("issuer"),
		ServerRootPEM:  []byte("server root"),
		NotAfter:       notAfter,
	}}
	url, _ := listener(t, &agentService{}, operators, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, enrolling)

	status, body := send(t, anonymous(t), http.MethodPost, url+"/enrollment",
		`{"token":"a-token","certificateRequestPem":"a request"}`)
	require.Equal(t, http.StatusCreated, status)
	assert.JSONEq(t, `{
		"grn": "`+enrolling.String()+`",
		"certificatePem": "certificate",
		"issuerPem": "issuer",
		"serverRootPem": "server root",
		"notAfter": "2026-08-26T12:00:00Z"
	}`, body)
	assert.NotContains(t, body, "privateKeyPem")

	assert.Equal(t, []enrolment{{token: "a-token", request: operator.CertificateRequest("a request")}},
		operators.enrolled)
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

// enrollmentRoute is the one operation reached without a certificate
// (ADR-0064). It is named here so that what a reader checks is the
// exemption rather than the routes that are not exempt, which are
// whatever is left.
const enrollmentRoute = http.MethodPost + " /enrollment"

// TestServer_EnrollmentIsTheOnlyRouteAnUncertifiedCallerReaches is the
// control on the one above: the same client that enrols is refused
// everywhere else, so what admitted it was the route and not a listener
// that stopped checking.
func TestServer_EnrollmentIsTheOnlyRouteAnUncertifiedCallerReaches(t *testing.T) {
	enrolling := operatorGRN("acme")
	operators := &operatorService{certificate: &operator.Certificate{Subject: enrolling}}
	url, _ := listener(t, &agentService{}, operators, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, enrolling)
	client := anonymous(t)

	// Every operation api/machine.yaml declares, read off the generated
	// registration rather than listed, so a route added without the
	// guard is caught here rather than in production: the listener
	// stopped making this refusal centrally, and each handler makes it
	// now.
	routes := machineRoutes(t, map[string]string{
		"agent":   agentGRN("acme", "0a1b2c3d4e5f6071").String(),
		"claim":   "c-1",
		"message": "m-1",
	})
	enrol, ok := routes[enrollmentRoute]
	require.True(t, ok, "the exemption names no operation the specification declares")
	delete(routes, enrollmentRoute)

	status, _ := send(t, client, enrol.method, url+enrol.path,
		`{"token":"a-token","certificateRequestPem":"a request"}`)
	require.Equal(t, http.StatusCreated, status)

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

// presentedBy returns the certificate the client authenticates with,
// which is what the surface has to read the renewal off.
func presentedBy(t *testing.T, client *http.Client) *x509.Certificate {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.Len(t, transport.TLSClientConfig.Certificates, 1)
	leaf, err := x509.ParseCertificate(transport.TLSClientConfig.Certificates[0].Certificate[0])
	require.NoError(t, err)
	return leaf
}

// TestServer_RenewsTheCertificateTheConnectionAuthenticatedWith covers
// what the surface reads off the handshake and what comes back. The
// route names no subject, so what it passes on is the certificate
// presented — the identity it names, and which certificate it is.
func TestServer_RenewsTheCertificateTheConnectionAuthenticatedWith(t *testing.T) {
	holder := operatorGRN("acme")
	notAfter := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	operators := &operatorService{certificate: &operator.Certificate{
		Subject:        holder,
		Serial:         "successor",
		CertificatePEM: []byte("certificate"),
		IssuerPEM:      []byte("issuer"),
		ServerRootPEM:  []byte("server root"),
		NotAfter:       notAfter,
	}}
	url, client := listener(t, &agentService{}, operators, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, holder)

	status, body := do(t, client, http.MethodPost, url+"/identity/certificate")
	require.Equal(t, http.StatusCreated, status)
	assert.JSONEq(t, `{
		"grn": "`+holder.String()+`",
		"certificatePem": "certificate",
		"privateKeyPem": "private key",
		"issuerPem": "issuer",
		"serverRootPem": "server root",
		"notAfter": "2026-08-26T12:00:00Z"
	}`, body)

	presented := presentedBy(t, client)
	require.Len(t, operators.renewed, 1)
	assert.Equal(t, operator.Credential{
		Subject:   holder,
		Serial:    operator.Serial(presented.SerialNumber.String()),
		NotBefore: presented.NotBefore,
		NotAfter:  presented.NotAfter,
	}, operators.renewed[0], "the renewal names the certificate the connection presented")
}

// TestServer_RenewalRefusalsCarryTheirKind holds each refusal the rule
// makes against the answer above: a caller reads a renewal it may ask
// for again off a different kind from one only a mint out of band
// recovers, and neither is invented at this boundary.
func TestServer_RenewalRefusalsCarryTheirKind(t *testing.T) {
	holder := operatorGRN("acme")
	operators := &operatorService{}
	url, client := listener(t, &agentService{}, operators, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, holder)

	refusals := map[string]struct {
		err    error
		status int
		kind   string
	}{
		"too early":       {operator.ErrRenewalTooEarly, http.StatusConflict, "failed_precondition"},
		"superseded":      {operator.ErrSuperseded, http.StatusConflict, "already_exists"},
		"expired":         {operator.ErrCertificateExpired, http.StatusForbidden, "permission_denied"},
		"not an operator": {operator.ErrNotAnOperator, http.StatusForbidden, "permission_denied"},
	}
	for name, refusal := range refusals {
		t.Run(name, func(t *testing.T) {
			operators.err = refusal.err
			status, body := do(t, client, http.MethodPost, url+"/identity/certificate")
			assert.Equal(t, refusal.status, status)
			assert.JSONEq(t, `{"kind":"`+refusal.kind+`","message":"`+refusal.err.Error()+`"}`, body)
		})
	}
}
