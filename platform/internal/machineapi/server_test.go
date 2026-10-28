package machineapi_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/watch"
)

func operatorGRN(org string) grn.GRN {
	return grn.New(org, grn.DefaultProject, agent.ServiceTypeOperator, "one")
}

func agentGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, agent.ServiceType, id)
}

func groupGRN(org, name string) grn.GRN {
	return grn.New(org, grn.DefaultProject, "group", name)
}

// call is one thing the surface asked the agent service for.
type call struct{ operator, subject grn.GRN }

// stateReport is one state report the surface passed on: who reported,
// about which agent, at what epoch, and the state it parsed. The state
// is kept as its string so one shape serves both kinds; which kind it
// was is the field the report was recorded in.
type stateReport struct {
	call
	epoch int
	state string
}

// agentService is an agent.Service recording what the surface passed
// it.
type agentService struct {
	err          error
	calls        []call
	provisioning []stateReport
	runtime      []stateReport
	// audiences is what each mint asked for.
	audiences []*grn.GRN
}

// MintAccessToken answers a token for the agent named, recording the
// caller and audience it was asked with.
func (s *agentService) MintAccessToken(_ context.Context, caller, agentGRN grn.GRN, audience *grn.GRN) (*agent.AccessToken, error) {
	s.calls = append(s.calls, call{operator: caller, subject: agentGRN})
	s.audiences = append(s.audiences, audience)
	if s.err != nil {
		return nil, s.err
	}
	return &agent.AccessToken{Value: agent.AccessTokenPrefix + strings.Repeat("a", 64), Agent: agentGRN,
		Assignee: caller, Epoch: 3, Audience: audience, ServerRootPEM: []byte("server root"),
		ExpiresAt: time.Date(2026, time.October, 9, 13, 0, 0, 0, time.UTC)}, nil
}

func (s *agentService) ReportProvisioningState(_ context.Context, operator, agentGRN grn.GRN, epoch int, state agent.ProvisioningState) error {
	s.provisioning = append(s.provisioning, stateReport{
		call: call{operator: operator, subject: agentGRN}, epoch: epoch, state: string(state)})
	return s.err
}

func (s *agentService) ReportRuntimeState(_ context.Context, caller, agentGRN grn.GRN, epoch int, state agent.RuntimeState, _ agent.Fencing) error {
	s.runtime = append(s.runtime, stateReport{
		call: call{operator: caller, subject: agentGRN}, epoch: epoch, state: string(state)})
	return s.err
}

// memberGroups is the machineapi.Groups seam in memory: which groups
// each identity belongs to, and the answer a store that cannot read
// them gives.
type memberGroups struct {
	byMember map[grn.GRN][]grn.GRN
	err      error
	asked    []grn.GRN
}

func (m *memberGroups) List(_ context.Context, subject grn.GRN) ([]grn.GRN, error) {
	m.asked = append(m.asked, subject)
	if m.err != nil {
		return nil, m.err
	}
	return m.byMember[subject], nil
}

// consents is the message.ConsentStore the surface reads, in memory:
// the organization pairs whose relation stands, and the answer a store
// that cannot read one gives. Stands is the whole of what the surface
// asks; the rest panics through the embedded interface.
type consents struct {
	message.ConsentStore
	pairs map[string]bool
	err   error
	asked [][2]grn.GRN
}

// consenting builds a consents in which the relation stands between
// each named pair of organization identifiers and no other.
func consenting(pairs ...[2]string) *consents {
	c := &consents{pairs: map[string]bool{}}
	for _, p := range pairs {
		c.stand(p[0], p[1])
	}
	return c
}

// stand moves the pair's relation up, as two grants would.
func (c *consents) stand(orgA, orgB string) {
	a, b := organization(orgA).String(), organization(orgB).String()
	if b < a {
		a, b = b, a
	}
	c.pairs[a+"|"+b] = true
}

// fall moves the pair's relation down, as a withdraw would.
func (c *consents) fall(orgA, orgB string) {
	a, b := organization(orgA).String(), organization(orgB).String()
	if b < a {
		a, b = b, a
	}
	delete(c.pairs, a+"|"+b)
}

func (c *consents) Stands(_ context.Context, a, b grn.GRN) (bool, error) {
	c.asked = append(c.asked, [2]grn.GRN{a, b})
	if c.err != nil {
		return false, c.err
	}
	x, y := a.String(), b.String()
	if y < x {
		x, y = y, x
	}
	return c.pairs[x+"|"+y], nil
}

// organization returns the GRN of the org the identifier names. Orgs
// are root-owned (ADR-0001).
func organization(identifier string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "org", identifier)
}

// listenerCertificate is what garam presents, as the listener asks for
// it (machine.ServerCertificate).
type listenerCertificate tls.Certificate

// loopback is the certificate the listener terminates TLS with. What
// garam presents is not what these tests are about, so it is
// self-signed and the client below does not verify it.
func loopback(t *testing.T) listenerCertificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "garam machine listener"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return listenerCertificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// Get answers every handshake with the one certificate above. The
// listener asks per handshake so a replaced certificate is served
// without a restart (ADR-0040); nothing replaces this one.
func (c listenerCertificate) Get() (*tls.Certificate, error) {
	certificate := tls.Certificate(c)
	return &certificate, nil
}

// listener serves the surface behind the machine listener and returns
// its URL with a client presenting a token for the caller. Everything
// below the surface is real except the services it answers from: the
// identity a handler reads is the one the listener resolved the token
// to.
func listener(t *testing.T, agents agent.Service, operators operator.Service, messages message.Service, standing message.ConsentStore, watches watch.Service, groups machineapi.Groups, engine machineapi.Evaluator, caller grn.GRN) (string, *http.Client) {
	t.Helper()
	return serveTLS(t, machineapi.NewServer(agents, operators, messages, standing, watches, groups, engine, nil, nil, nil, nil, nil, nil, stubKeyring{}), caller)
}

// stubKeyID is the one key stubSigner publishes.
const stubKeyID message.KeyID = "stub-key"

// stubKeyring stands in for the message keyring, whose signatures and
// keys are exercised against the real queue and keys in
// tests/integration. It answers fixed values, so a test here reads the
// surface's rendering of them.
type stubKeyring struct{}

func (stubKeyring) Signer(context.Context) (message.Signer, error) { return stubKeyring{}, nil }

func (stubKeyring) Sign(context.Context, *message.Claim) (message.Signed, error) {
	return message.Signed{Content: "stub content", Signature: "stub.signature.value"}, nil
}

func (stubKeyring) PublishedKeys(context.Context) ([]message.PublishedKey, error) {
	return []message.PublishedKey{{ID: stubKeyID, X: make([]byte, 32), Y: make([]byte, 32)}}, nil
}

// serveTLS serves srv behind the machine listener, as listener does,
// for a test that composes the server itself, inside middlewares where
// a test names them.
func serveTLS(t *testing.T, srv *machineapi.Server, caller grn.GRN, middlewares ...machinegen.MiddlewareFunc) (string, *http.Client) {
	t.Helper()
	return serveTLSTokens(t, srv, caller, presentedTokens{}, middlewares...)
}

// presentedTokens is the machine.Tokens seam in memory: the GRN each
// live token names. One no entry names is refused.
type presentedTokens map[string]grn.GRN

func (k presentedTokens) Resolve(_ context.Context, token string) (machine.Bearer, error) {
	subject, ok := k[token]
	if !ok {
		return machine.Bearer{}, errorx.New(errorx.KindUnauthenticated, "token not live")
	}
	// An agent's token carries a serial the fence reads, as the agent
	// store answers one; an operator's carries none.
	serial := ""
	if subject.ServiceType == agent.ServiceType {
		serial = "t" + strings.Repeat("0", 32)
	}
	return machine.Bearer{Subject: subject, Serial: serial, IssuedAt: time.Now().Add(-time.Minute),
		ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// callerToken is the token the test client presents for the caller,
// on every request whose Authorization a test does not set itself.
const callerToken = "garam_at_caller"

// callerTokens answers callerToken for caller and every other token
// from base.
type callerTokens struct {
	base   machine.Tokens
	caller grn.GRN
}

func (c callerTokens) Resolve(ctx context.Context, token string) (machine.Bearer, error) {
	if token != callerToken {
		return c.base.Resolve(ctx, token)
	}
	return presentedTokens{callerToken: c.caller}.Resolve(ctx, token)
}

// presentingCaller sets the caller's token on a request that carries no
// Authorization of its own, as a caller's client does on every call.
type presentingCaller struct{ base http.RoundTripper }

func (p presentingCaller) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Authorization") == "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", machine.TokenScheme+" "+callerToken)
	}
	return p.base.RoundTrip(r)
}

// serveTLSTokens is serveTLS answering a presented token from tokens.
func serveTLSTokens(t *testing.T, srv *machineapi.Server, caller grn.GRN, tokens machine.Tokens, middlewares ...machinegen.MiddlewareFunc) (string, *http.Client) {
	t.Helper()
	served := machine.NewServer(
		callerTokens{base: tokens, caller: caller},
		nil,
		loopback(t),
		machinegen.HandlerWithOptions(srv, machinegen.StdHTTPServerOptions{
			ErrorHandlerFunc: machineapi.BindingError, Middlewares: middlewares,
		}),
	)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = served.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = served.Close() })

	transport := &http.Transport{TLSClientConfig: &tls.Config{
		//nolint:gosec // the listener's own certificate is not under test.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}}
	client := &http.Client{Transport: presentingCaller{base: transport}}
	t.Cleanup(transport.CloseIdleConnections)
	return "https://" + lis.Addr().String(), client
}

// do performs one request and returns the status and the body.
func do(t *testing.T, client *http.Client, method, url string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// TestServer_AnswersNothingToAConnectionThatAuthenticatedNobody keeps
// every handler behind the identity the listener resolved. The answer
// through the listener sits beside the refusals, because a 401 from
// every route holds as readily for a surface that answers nothing at
// all.
func TestServer_AnswersNothingToAConnectionThatAuthenticatedNobody(t *testing.T) {
	operator := operatorGRN("acme")
	agents := &agentService{}
	messages := &messageService{}
	groups := &memberGroups{}
	policies := &engine{}
	url, client := listener(t, agents, nil, messages, consenting(), unwatched(t), groups, policies, operator)

	status, body := do(t, client, http.MethodGet, url+"/identity")
	require.Equal(t, http.StatusOK, status)
	require.JSONEq(t, `{"grn":"`+operator.String()+`"}`, body)

	// The same surface served any other way carries no identity, and no
	// handler invents one.
	relay := &announced{}
	plain := httptest.NewServer(machinegen.Handler(machineapi.NewServer(agents, nil, messages, consenting(), watching(t, relay), groups, policies, nil, nil, nil, nil, nil, nil, nil)))
	t.Cleanup(plain.Close)
	bot := agentGRN("acme", "0a1b2c3d4e5f6071").String()
	routes := map[string]struct{ method, path string }{
		"identity": {http.MethodGet, "/identity"},
		"post":     {http.MethodPost, "/messages"},
		"claim":    {http.MethodPost, "/agents/" + bot + "/claims"},
		"renew":    {http.MethodPost, "/agents/" + bot + "/claims/claim-1/renewal"},
		"complete": {http.MethodPost, "/agents/" + bot + "/claims/claim-1/completion"},
		"forward":  {http.MethodPost, "/agents/" + bot + "/turn-events"},
	}
	for name, route := range routes {
		t.Run(name, func(t *testing.T) {
			status, body := do(t, plain.Client(), route.method, plain.URL+route.path)
			assert.Equal(t, http.StatusUnauthorized, status)
			assert.JSONEq(t, `{"kind":"unauthenticated","message":"no machine identity"}`, body)
		})
	}
	assert.Empty(t, agents.calls, "a request that authenticated nobody reached the service")
	assert.Empty(t, messages.calls, "a request that authenticated nobody reached the service")
	assert.Empty(t, groups.asked, "a request that authenticated nobody was looked up in a group")
	assert.Empty(t, policies.asked, "a request that authenticated nobody was decided by policy")
	assert.Empty(t, relay.made(), "a request that authenticated nobody forwarded a turn event")
}
