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

	"github.com/garamsh/garam/internal/api"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// stubAuthzServer is a canned AuthzService double for client tests. got
// keeps the last request so a test can read what reached the wire, and
// stays nil when nothing did.
type stubAuthzServer struct {
	authzv1.UnimplementedAuthzServiceServer
	allowed bool
	err     error
	got     *authzv1.CheckRequest
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
	return evalInputWith(api.EffectAllow)
}

func evalInputWith(effect api.Effect) api.EvalInput {
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

// TestGRPCEvaluator_UnrecognisedEffectIsRefused holds the refusal
// against the two effects that must still reach the engine intact:
// alone, the refusal would hold for any reason the plumbing supplied.
func TestGRPCEvaluator_UnrecognisedEffectIsRefused(t *testing.T) {
	recognised := []struct {
		effect api.Effect
		want   authzv1.Effect
	}{
		{api.EffectAllow, authzv1.Effect_EFFECT_ALLOW},
		{api.EffectDeny, authzv1.Effect_EFFECT_DENY},
	}
	for _, c := range recognised {
		stub := &stubAuthzServer{allowed: true}
		ev := newGRPCEvaluator(t, stub)
		allowed, err := ev.Evaluate(context.Background(), evalInputWith(c.effect))
		require.NoError(t, err, "effect %q", c.effect)
		assert.True(t, allowed, "effect %q", c.effect)
		require.Len(t, stub.got.GetStatements(), 1)
		assert.Equal(t, c.want, stub.got.GetStatements()[0].GetEffect(), "effect %q", c.effect)
	}

	stub := &stubAuthzServer{allowed: true}
	ev := newGRPCEvaluator(t, stub)
	allowed, err := ev.Evaluate(context.Background(), evalInputWith(api.Effect("Maybe")))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v", err)
	assert.False(t, allowed)
	assert.Nil(t, stub.got, "an unrecognised Effect reached the engine")
}

func TestGRPCEvaluator_StatusCarriesKind(t *testing.T) {
	cases := []struct {
		code codes.Code
		kind errorx.Kind
	}{
		{codes.InvalidArgument, errorx.KindInvalidArgument},
		{codes.Unavailable, errorx.KindUnavailable},
		{codes.Internal, errorx.KindInternal},
	}
	for _, c := range cases {
		t.Run(c.code.String(), func(t *testing.T) {
			ev := newGRPCEvaluator(t, &stubAuthzServer{err: status.Error(c.code, "boom")})
			_, err := ev.Evaluate(context.Background(), evalInput())
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, c.kind), "got %v", err)
		})
	}
}
