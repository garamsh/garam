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

// stubAuthzServer is a canned AuthzService double for client tests.
type stubAuthzServer struct {
	authzv1.UnimplementedAuthzServiceServer
	allowed bool
	err     error
}

func (s *stubAuthzServer) Check(_ context.Context, _ *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
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
	return api.EvalInput{
		Subject: grn.New("root", "default", "user", "alice"),
		Object:  grn.New("root", "default", "org", "acme"),
		Action:  "org:update",
		Statements: []api.Statement{{
			Sid:      "org-role-owner",
			Effect:   api.EffectAllow,
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
