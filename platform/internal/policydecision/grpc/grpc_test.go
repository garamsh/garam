package grpc_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/policydecision"
	decisiongrpc "github.com/garamsh/garam/platform/internal/policydecision/grpc"
	feedgrpc "github.com/garamsh/garam/platform/internal/policyfeed/grpc"
	"github.com/garamsh/garam/platform/internal/signer"
)

// fakeService answers the questions it is given with answers and
// records what it was asked and the token it was asked under.
type fakeService struct {
	policyv1.UnimplementedPolicyServiceServer
	mu      sync.Mutex
	answers []*policyv1.EvaluateResponse
	evals   []*policyv1.EvaluateRequest
	batches []*policyv1.EvaluationsRequest
	tokens  []string
	// refuse, when set, answers Evaluate with the error it returns for
	// a request, as the service refuses a question.
	refuse func(*policyv1.EvaluateRequest) error
}

func (f *fakeService) note(ctx context.Context) {
	md, _ := metadata.FromIncomingContext(ctx)
	f.tokens = append(f.tokens, strings.Join(md.Get("authorization"), ""))
}

func (f *fakeService) Evaluate(
	ctx context.Context, req *policyv1.EvaluateRequest,
) (*policyv1.EvaluateResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note(ctx)
	f.evals = append(f.evals, req)
	if f.refuse != nil {
		if err := f.refuse(req); err != nil {
			return nil, err
		}
	}
	return f.answers[0], nil
}

func (f *fakeService) Evaluations(
	ctx context.Context, req *policyv1.EvaluationsRequest,
) (*policyv1.EvaluationsResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.note(ctx)
	f.batches = append(f.batches, req)
	return &policyv1.EvaluationsResponse{Evaluations: f.answers}, nil
}

func reasons(ids ...string) *policyv1.DecisionContext {
	ctx := &policyv1.DecisionContext{}
	for _, id := range ids {
		ctx.Reasons = append(ctx.Reasons, &policyv1.Reason{PolicyId: id})
	}
	return ctx
}

// start runs the fake over TLS on a loopback port and answers a client
// of it dialled the way serve api dials the policy service.
func start(t *testing.T, f *fakeService) *decisiongrpc.Client {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "policy"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		IsCA:         true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer(grpc.Creds(credentials.NewServerTLSFromCert(
		&tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv})))
	policyv1.RegisterPolicyServiceServer(srv, f)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return dial(t, lis.Addr().String(), caFile)
}

func dial(t *testing.T, addr, caFile string) *decisiongrpc.Client {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	require.NoError(t, err)
	key, err := signer.ParseKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)
	conn, err := feedgrpc.Dial(addr, caFile, "https://issuer.test", key)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return decisiongrpc.New(policyv1.NewPolicyServiceClient(conn))
}

func claimsOf(t *testing.T, bearer string) map[string]any {
	t.Helper()
	parts := strings.Split(strings.TrimPrefix(bearer, "Bearer "), ".")
	require.Len(t, parts, 3, "the call carried no signed token")
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(raw, &claims))
	return claims
}

var (
	ada   = grn.New("root", "user", "ada")
	acme  = grn.New("root", "org", "acme")
	later = grn.New("acme", "project", "later")
)

func TestClient_AsksAsTheCallerPlatform(t *testing.T) {
	f := &fakeService{answers: []*policyv1.EvaluateResponse{{Decision: true, Context: reasons("p")}}}
	c := start(t, f)

	_, err := c.Evaluate(context.Background(),
		policydecision.Question{Subject: ada, Action: "iam:org.read", Resource: acme})
	require.NoError(t, err)
	_, err = c.Evaluations(context.Background(),
		[]policydecision.Question{{Subject: ada, Action: "iam:org.read", Resource: acme}})
	require.NoError(t, err)

	require.Len(t, f.tokens, 2)
	for _, token := range f.tokens {
		claims := claimsOf(t, token)
		assert.Equal(t, "platform", claims["sub"])
		assert.Equal(t, "policy", claims["aud"])
	}
}

func TestClient_StatesProbeWhereItsCallerSaysSo(t *testing.T) {
	f := &fakeService{answers: []*policyv1.EvaluateResponse{{}, {}}}
	c := start(t, f)
	q := policydecision.Question{Subject: ada, Action: "iam:org.read", Resource: acme}
	probe := q
	probe.Probe = true

	_, err := c.Evaluate(context.Background(), probe)
	require.NoError(t, err)
	_, err = c.Evaluate(context.Background(), q)
	require.NoError(t, err)
	_, err = c.Evaluations(context.Background(), []policydecision.Question{probe, q})
	require.NoError(t, err)

	require.Len(t, f.evals, 2)
	assert.True(t, f.evals[0].GetProbe(), "a probe asked as an attempt")
	assert.False(t, f.evals[1].GetProbe(), "an attempt asked as a probe")
	asked := f.batches[0].GetEvaluations()
	require.Len(t, asked, 2)
	require.NotNil(t, asked[0].Probe)
	assert.True(t, *asked[0].Probe)
	require.NotNil(t, asked[1].Probe)
	assert.False(t, *asked[1].Probe, "an attempt asked as a probe")
}

func TestClient_NamesTheEntityTypesItsGRNsAre(t *testing.T) {
	f := &fakeService{answers: []*policyv1.EvaluateResponse{{}}}
	c := start(t, f)

	_, err := c.Evaluate(context.Background(),
		policydecision.Question{Subject: ada, Action: "iam:project.read", Resource: later})
	require.NoError(t, err)

	req := f.evals[0]
	assert.Equal(t, "Iam::User", req.GetSubject().GetType())
	assert.Equal(t, ada.String(), req.GetSubject().GetId())
	assert.Equal(t, "Iam::Project", req.GetResource().GetType())
	assert.Equal(t, later.String(), req.GetResource().GetId())
	assert.Equal(t, "iam:project.read", req.GetAction().GetName())
}

func TestClient_ReturnsTheDecisionWithItsReasonsAsASet(t *testing.T) {
	f := &fakeService{answers: []*policyv1.EvaluateResponse{
		{Decision: true, Context: reasons("role-org-owner", "role-org-owner", "other")},
		{Decision: false, Context: &policyv1.DecisionContext{Reasons: []*policyv1.Reason{
			{Error: "refused"}}}},
		{},
	}}
	c := start(t, f)

	got, err := c.Evaluations(context.Background(), []policydecision.Question{
		{Subject: ada, Action: "iam:org.read", Resource: acme},
		{Subject: ada, Action: "iam:org.read", Resource: acme},
		{Subject: ada, Action: "iam:org.read", Resource: acme},
	})
	require.NoError(t, err)

	require.Len(t, got, 3)
	assert.True(t, got[0].Allowed)
	assert.Equal(t, policydecision.Reasons{
		{PolicyID: "role-org-owner"}: {}, {PolicyID: "other"}: {},
	}, got[0].Reasons, "a repeated reason is one member")
	assert.False(t, got[1].Allowed)
	assert.Equal(t, policydecision.Reasons{{Error: "refused"}: {}}, got[1].Reasons)
	assert.False(t, got[2].Allowed)
	assert.Empty(t, got[2].Reasons)

	one, err := c.Evaluate(context.Background(),
		policydecision.Question{Subject: ada, Action: "iam:org.read", Resource: acme})
	require.NoError(t, err)
	assert.True(t, one.Allowed)
	assert.Len(t, one.Reasons, 2)
}

func TestClient_FailsClosedWhenTheServiceCannotBeReached(t *testing.T) {
	f := &fakeService{answers: []*policyv1.EvaluateResponse{{Decision: true}}}
	live := start(t, f)
	// The control: the same question, asked of a service that is there,
	// is allowed, so the denial below is the unreachable service's.
	got, err := live.Evaluate(context.Background(),
		policydecision.Question{Subject: ada, Action: "iam:org.read", Resource: acme})
	require.NoError(t, err)
	require.True(t, got.Allowed)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := lis.Addr().String()
	require.NoError(t, lis.Close())
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificate(rand.Reader,
		&x509.Certificate{SerialNumber: big.NewInt(1), NotAfter: time.Now().Add(time.Hour)},
		&x509.Certificate{SerialNumber: big.NewInt(1), NotAfter: time.Now().Add(time.Hour)},
		&priv.PublicKey, priv)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(caFile,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	down := dial(t, addr, caFile)

	q := policydecision.Question{Subject: ada, Action: "iam:org.read", Resource: acme}
	one, err := down.Evaluate(context.Background(), q)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), err.Error())
	assert.False(t, one.Allowed)

	many, err := down.Evaluations(context.Background(), []policydecision.Question{q})
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), err.Error())
	assert.Empty(t, many)
}

func TestClient_RefusesWhatNoEntityTypeNames(t *testing.T) {
	f := &fakeService{answers: []*policyv1.EvaluateResponse{{}}}
	c := start(t, f)

	_, err := c.Evaluate(context.Background(), policydecision.Question{
		Subject: ada, Action: "iam:org.read", Resource: grn.GRN{Service: "iam", ServiceType: "nothing"}})
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
	_, err = c.Evaluations(context.Background(), nil)
	require.Error(t, err)
	assert.Empty(t, f.evals, "a question no type names was asked of the service")
}

func TestClient_AnswerOfTheWrongLengthIsAnError(t *testing.T) {
	f := &fakeService{answers: []*policyv1.EvaluateResponse{{Decision: true}}}
	c := start(t, f)
	q := policydecision.Question{Subject: ada, Action: "iam:org.read", Resource: acme}

	_, err := c.Evaluations(context.Background(), []policydecision.Question{q, q})

	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInternal))
}

// refusal is how the service refuses a question: INVALID_ARGUMENT, with
// an ErrorInfo naming reason when it names a cause.
func refusal(t *testing.T, reason string) error {
	t.Helper()
	st := status.New(codes.InvalidArgument, "the evaluation does not fit the schema")
	if reason == "" {
		return st.Err()
	}
	st, err := st.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: "garam.policy.v1"})
	require.NoError(t, err)
	return st.Err()
}

func TestClient_ReadsAPrincipalTheActionDoesNotApplyToAsADenial(t *testing.T) {
	q := policydecision.Question{Subject: ada, Action: "iam:agent.read", Resource: acme}
	refused := &policyv1.EvaluateResponse{Context: &policyv1.DecisionContext{Reasons: []*policyv1.Reason{
		{Error: "the evaluation does not fit the schema"}}}}
	f := &fakeService{
		answers: []*policyv1.EvaluateResponse{refused},
		refuse:  func(*policyv1.EvaluateRequest) error { return refusal(t, "PRINCIPAL_NOT_APPLICABLE") },
	}
	c := start(t, f)

	one, err := c.Evaluate(context.Background(), q)
	require.NoError(t, err)
	assert.False(t, one.Allowed)
	assert.NotNil(t, one.Reasons)
	assert.Empty(t, one.Reasons)

	probe := q
	probe.Probe = true
	many, err := c.Evaluations(context.Background(), []policydecision.Question{probe})
	require.NoError(t, err)
	require.Len(t, many, 1)
	assert.False(t, many[0].Allowed)
	assert.NotNil(t, many[0].Reasons)
	assert.Empty(t, many[0].Reasons, "the refusal is read as a denial, not kept as a reason")
	require.Len(t, f.evals, 2, "the refused entry was asked again alone")
	assert.True(t, f.evals[1].GetProbe(), "the question was asked again as its caller stated it")
}

func TestClient_AnyOtherRefusalStaysAnError(t *testing.T) {
	q := policydecision.Question{Subject: ada, Action: "iam:org.rename", Resource: acme}
	for name, reason := range map[string]string{
		"no cause named": "",
		"another cause":  "SOMETHING_ELSE",
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeService{
				answers: []*policyv1.EvaluateResponse{{}},
				refuse:  func(*policyv1.EvaluateRequest) error { return refusal(t, reason) },
			}
			c := start(t, f)

			_, err := c.Evaluate(context.Background(), q)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), err.Error())
		})
	}
}

func TestClient_AnEntryRefusedForAnotherCauseKeepsItsReason(t *testing.T) {
	text := "the evaluation does not fit the schema"
	f := &fakeService{
		answers: []*policyv1.EvaluateResponse{{Context: &policyv1.DecisionContext{
			Reasons: []*policyv1.Reason{{Error: text}}}}},
		refuse: func(*policyv1.EvaluateRequest) error { return refusal(t, "") },
	}
	c := start(t, f)

	got, err := c.Evaluations(context.Background(), []policydecision.Question{
		{Subject: ada, Action: "iam:org.rename", Resource: acme}})

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.False(t, got[0].Allowed)
	assert.Equal(t, policydecision.Reasons{{Error: text}: {}}, got[0].Reasons)
}

func TestClient_AnEntryAskedAgainWhoseServiceFailsFailsTheCall(t *testing.T) {
	f := &fakeService{
		answers: []*policyv1.EvaluateResponse{{Context: &policyv1.DecisionContext{
			Reasons: []*policyv1.Reason{{Error: "refused"}}}}},
		refuse: func(*policyv1.EvaluateRequest) error { return status.Error(codes.Unavailable, "down") },
	}
	c := start(t, f)

	got, err := c.Evaluations(context.Background(), []policydecision.Question{
		{Subject: ada, Action: "iam:agent.read", Resource: acme}})

	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), err.Error())
	assert.Empty(t, got)
}
