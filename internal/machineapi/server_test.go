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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
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
// it. Create answers no assignment: one is constructed by
// internal/agent alone, and what the real service returns for a
// created agent is the round trip in tests/integration.
type agentService struct {
	certificate  *agent.Certificate
	err          error
	calls        []call
	states       []*agent.DefinitionState
	assignment   *agent.Assignment
	provisioning []stateReport
	runtime      []stateReport
}

func (s *agentService) Create(_ context.Context, operator grn.GRN) (*agent.Assignment, error) {
	s.calls = append(s.calls, call{operator: operator})
	if s.err == nil {
		return nil, errorx.New(errorx.KindUnimplemented, "an assignment is internal/agent's to construct")
	}
	return nil, s.err
}

func (s *agentService) IssueCertificate(_ context.Context, operator, agentGRN grn.GRN) (*agent.Certificate, error) {
	s.calls = append(s.calls, call{operator: operator, subject: agentGRN})
	if s.err != nil {
		return nil, s.err
	}
	return s.certificate, nil
}

func (s *agentService) Definitions(_ context.Context, operator grn.GRN) ([]*agent.DefinitionState, error) {
	s.calls = append(s.calls, call{operator: operator})
	if s.err != nil {
		return nil, s.err
	}
	return s.states, nil
}

func (s *agentService) Claim(_ context.Context, operator, agentGRN grn.GRN) (*agent.Assignment, error) {
	s.calls = append(s.calls, call{operator: operator, subject: agentGRN})
	if s.err != nil {
		return nil, s.err
	}
	return s.assignment, nil
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

// authorities is a ca.Store in memory. The tests need a real
// certificate authority per organization to sign a caller's
// certificate, and nothing else a store does.
type authorities map[grn.GRN]*ca.Authority

func (a authorities) Create(_ context.Context, authority *ca.Authority) error {
	if _, held := a[authority.Organization]; held {
		return ca.ErrAuthorityAlreadyExists
	}
	a[authority.Organization] = authority
	return nil
}

func (a authorities) Get(_ context.Context, orgGRN grn.GRN) (*ca.Authority, error) {
	authority, held := a[orgGRN]
	if !held {
		return nil, ca.ErrAuthorityNotFound
	}
	return authority, nil
}

// trustRoots reads what a presented certificate must chain to out of
// the authorities it was issued from — the adapter the composition
// root builds over ca.Store, in the form a test can hold.
// everyRecordHeld admits every identity its authority signed for.
// What a withdrawn record does to a handshake is the machine
// package's, and these tests are about what the surface answers a
// caller that has one (ADR-0070).
type everyRecordHeld struct{}

func (everyRecordHeld) Holds(context.Context, grn.GRN) error { return nil }

type trustRoots struct{ store authorities }

func (r trustRoots) Get(ctx context.Context, subject grn.GRN) ([]byte, error) {
	authority, err := r.store.Get(ctx,
		grn.New(grn.RootOwner, grn.DefaultProject, "org", subject.Organization))
	if err != nil {
		return nil, err
	}
	return authority.Certificate, nil
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
// its URL with a client holding the certificate the caller's own
// organization signed. Everything below the surface is real except the
// services it answers from: a request arrives over a handshake garam
// verified, so the identity a handler reads is the one a certificate
// carried.
func listener(t *testing.T, agents agent.Service, operators operator.Service, messages message.Service, standing message.ConsentStore, watches watch.Service, groups machineapi.Groups, engine machineapi.Evaluator, caller grn.GRN) (string, *http.Client) {
	t.Helper()
	return serveTLS(t, machineapi.NewServer(agents, operators, messages, standing, watches, groups, engine, nil, nil, nil, nil, nil, stubKeyring{}), caller)
}

// stubKeyID is the one key stubSigner publishes.
const stubKeyID message.KeyID = "stub-key"

// stubKeyring stands in for the message keyring, whose signatures and
// keys are exercised against the real queue and keys in
// tests/integration. It answers fixed values, so a test here reads the
// surface's rendering of them.
type stubKeyring struct{}

func (stubKeyring) Signer(context.Context) (message.Signer, error) { return stubKeyring{}, nil }

func (stubKeyring) Sign(*message.Claim) (message.Signed, error) {
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

func (k presentedTokens) Resolve(_ context.Context, token string) (grn.GRN, time.Time, error) {
	subject, ok := k[token]
	if !ok {
		return grn.GRN{}, time.Time{}, errorx.New(errorx.KindUnauthenticated, "token not live")
	}
	return subject, time.Now().Add(time.Hour), nil
}

// serveTLSTokens is serveTLS answering a presented token from tokens.
func serveTLSTokens(t *testing.T, srv *machineapi.Server, caller grn.GRN, tokens machine.Tokens, middlewares ...machinegen.MiddlewareFunc) (string, *http.Client) {
	t.Helper()
	store := authorities{}
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	issuer, err := ca.NewService(store, sealer, ca.Config{})
	require.NoError(t, err)
	issued, err := issuer.Issue(context.Background(), caller)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair(issued.CertificatePEM(), issued.PrivateKeyPEM())
	require.NoError(t, err)

	served := machine.NewServer(
		machine.NewService(trustRoots{store: store}, everyRecordHeld{}, machine.Config{}),
		tokens,
		loopback(t),
		machinegen.HandlerWithOptions(srv, machinegen.StdHTTPServerOptions{
			ErrorHandlerFunc: machineapi.BindingError, Middlewares: middlewares,
		}),
	)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = served.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = served.Close() })

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		//nolint:gosec // the listener's own certificate is not under test.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{pair},
	}}}
	t.Cleanup(client.CloseIdleConnections)
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

// TestServer_IssuesForTheAgentThePathNames covers what reaches the
// agent service and what comes back. The refusals are asserted beside
// the answer, because a 403 holds for a surface that refuses
// everything and a 400 for one that parses nothing.
func TestServer_IssuesForTheAgentThePathNames(t *testing.T) {
	operator := operatorGRN("acme")
	held := agentGRN("acme", "0a1b2c3d4e5f6071")
	notAfter := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	agents := &agentService{certificate: &agent.Certificate{
		CertificatePEM: []byte("certificate"),
		PrivateKeyPEM:  []byte("private key"),
		IssuerPEM:      []byte("issuer"),
		ServerRootPEM:  []byte("server root"),
		NotAfter:       notAfter,
	}}
	url, client := listener(t, agents, nil, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, operator)

	status, body := do(t, client, http.MethodPost, url+"/agents/"+held.String()+"/certificate")
	require.Equal(t, http.StatusCreated, status)
	assert.JSONEq(t, `{
		"grn": "`+held.String()+`",
		"certificatePem": "certificate",
		"privateKeyPem": "private key",
		"issuerPem": "issuer",
		"serverRootPem": "server root",
		"notAfter": "2026-08-17T12:00:00Z"
	}`, body)
	assert.Equal(t, []call{{operator: operator, subject: held}}, agents.calls,
		"the caller is what the certificate named and the subject is what the path named")

	// What the service refuses is a refusal here, and it carries the
	// kind rather than a status invented at the boundary.
	agents.err = agent.ErrNotAssigned
	status, body = do(t, client, http.MethodPost, url+"/agents/"+held.String()+"/certificate")
	assert.Equal(t, http.StatusForbidden, status)
	assert.JSONEq(t, `{"kind":"permission_denied","message":"`+agent.ErrNotAssigned.Error()+`"}`, body)

	// A path segment that is not a GRN names no agent, and the service
	// is never asked about one.
	before := len(agents.calls)
	status, body = do(t, client, http.MethodPost, url+"/agents/bot-1/certificate")
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Contains(t, body, `"kind":"invalid_argument"`)
	assert.Len(t, agents.calls, before, "an unparsed subject reached the service")
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
	plain := httptest.NewServer(machinegen.Handler(machineapi.NewServer(agents, nil, messages, consenting(), watching(t, relay), groups, policies, nil, nil, nil, nil, nil, nil)))
	t.Cleanup(plain.Close)
	bot := agentGRN("acme", "0a1b2c3d4e5f6071").String()
	routes := map[string]struct{ method, path string }{
		"identity":    {http.MethodGet, "/identity"},
		"renewal":     {http.MethodPost, "/identity/certificate"},
		"create":      {http.MethodPost, "/agents"},
		"certificate": {http.MethodPost, "/agents/" + bot + "/certificate"},
		"definitions": {http.MethodGet, "/definitions"},
		"claim agent": {http.MethodPost, "/definitions/" + bot + "/claim"},
		"post":        {http.MethodPost, "/messages"},
		"claim":       {http.MethodPost, "/agents/" + bot + "/claims"},
		"renew":       {http.MethodPost, "/agents/" + bot + "/claims/claim-1/renewal"},
		"complete":    {http.MethodPost, "/agents/" + bot + "/claims/claim-1/completion"},
		"forward":     {http.MethodPost, "/agents/" + bot + "/turn-events"},
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
