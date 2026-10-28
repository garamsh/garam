package api_test

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/wire"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
)

// stubAuthzServer is a canned AuthzService double for client tests. got
// keeps the last request so a test can read what reached the wire, and
// stays nil when nothing did.
type stubAuthzServer struct {
	authzv1.UnimplementedAuthzServiceServer
	allowed bool
	err     error
	got     *authzv1.CheckRequest
	// explained is what Explain answers.
	explained *authzv1.ExplainResponse
}

func (s *stubAuthzServer) Explain(context.Context, *authzv1.ExplainRequest) (*authzv1.ExplainResponse, error) {
	return s.explained, nil
}

func (s *stubAuthzServer) Check(_ context.Context, req *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	s.got = req
	if s.err != nil {
		return nil, s.err
	}
	return &authzv1.CheckResponse{Allowed: s.allowed}, nil
}

// newGRPCEvaluator wires the client to the stub over an in-memory
// listener.
func newGRPCEvaluator(t *testing.T, stub *stubAuthzServer) *api.GRPCEvaluator {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	authzv1.RegisterAuthzServiceServer(srv, stub)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return api.NewGRPCEvaluator(conn)
}

func evalInput() api.EvalInput {
	return evalInputWith(wire.EffectAllow)
}

func evalInputWith(effect wire.Effect) api.EvalInput {
	return api.EvalInput{
		Subject: grn.New("root", "default", "user", "alice"),
		Object:  grn.New("root", "default", "org", "acme"),
		Action:  "org:update",
		Statements: []api.Statement{{
			Sid:      "org-role-owner",
			Effect:   effect,
			Action:   []string{"org:*"},
			Resource: []string{"grn:root:default:org:acme"},
		}},
	}
}

func TestGRPCEvaluator_Decision(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		ev := newGRPCEvaluator(t, &stubAuthzServer{allowed: allowed})
		got, err := ev.Evaluate(context.Background(), evalInput())
		require.NoError(t, err)
		assert.Equal(t, allowed, got)
	}
}

// TestGRPCEvaluator_SendsTheQuestionAndReadsTheEnginesRefusal holds what
// this client adds over the conversions internal/wire tests: the
// question reaches the engine as a Check carrying its statement, a
// statement the conversion refuses never reaches it, and a status the
// engine answers comes back as its kind. The accepted question is the
// control on the refusal.
func TestGRPCEvaluator_SendsTheQuestionAndReadsTheEnginesRefusal(t *testing.T) {
	stub := &stubAuthzServer{allowed: true}
	allowed, err := newGRPCEvaluator(t, stub).Evaluate(context.Background(), evalInput())
	require.NoError(t, err)
	assert.True(t, allowed)
	require.Len(t, stub.got.GetStatements(), 1)
	assert.Equal(t, "org-role-owner", stub.got.GetStatements()[0].GetSid())
	assert.Equal(t, authzv1.Effect_EFFECT_ALLOW, stub.got.GetStatements()[0].GetEffect())
	assert.Equal(t, "grn:root:default:user:alice", stub.got.GetSubjectGrn())

	refused := &stubAuthzServer{allowed: true}
	allowed, err = newGRPCEvaluator(t, refused).Evaluate(context.Background(), evalInputWith(wire.Effect("Maybe")))
	assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v", err)
	assert.False(t, allowed)
	assert.Nil(t, refused.got, "an unrecognised Effect reached the engine")

	_, err = newGRPCEvaluator(t, &stubAuthzServer{err: status.Error(codes.Unavailable, "boom")}).
		Evaluate(context.Background(), evalInput())
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "got %v", err)
}

// TestGRPCEvaluator_ExplainParsesEachTrace holds the trace a policy
// statement comes back as: its policy and the subjects it reached are
// parsed into GRNs, and a statement the caller sent comes back naming
// neither. A trace whose policy GRN does not parse is the engine's
// fault and fails the call as internal; the trace beside it that
// parses is the control.
func TestGRPCEvaluator_ExplainParsesEachTrace(t *testing.T) {
	policy := grn.New("acme", grn.DefaultProject, "policy", "p1")
	eng := grn.New("acme", grn.DefaultProject, "group", "eng")
	traced := []*authzv1.StatementTrace{
		{Sid: "deny", Matched: true, Effect: authzv1.Effect_EFFECT_DENY, PolicyGrn: policy.String(), AttachedTo: []string{eng.String()}},
		{Sid: "org-role-owner", Matched: true, Effect: authzv1.Effect_EFFECT_ALLOW},
	}
	ex, err := newGRPCEvaluator(t, &stubAuthzServer{explained: &authzv1.ExplainResponse{Traces: traced}}).
		Explain(context.Background(), evalInput())
	require.NoError(t, err)
	assert.False(t, ex.Allowed)
	assert.Equal(t, []api.Trace{
		{Sid: "deny", Matched: true, Effect: wire.EffectDeny, Policy: policy, AttachedTo: []grn.GRN{eng}},
		{Sid: "org-role-owner", Matched: true, Effect: wire.EffectAllow, AttachedTo: []grn.GRN{}},
	}, ex.Traces)

	corrupt := &authzv1.ExplainResponse{Traces: []*authzv1.StatementTrace{
		traced[1],
		{Sid: "deny", Matched: true, Effect: authzv1.Effect_EFFECT_DENY, PolicyGrn: "not-a-grn"},
	}}
	_, err = newGRPCEvaluator(t, &stubAuthzServer{explained: corrupt}).Explain(context.Background(), evalInput())
	assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v", err)
}
