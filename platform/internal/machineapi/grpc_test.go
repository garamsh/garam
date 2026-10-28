package machineapi_test

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/wire"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
)

// stubAuthzServer is a canned AuthzService double for client tests. got
// keeps the last request so a test can read what reached the wire, and
// stays nil when nothing did.
type stubAuthzServer struct {
	authzv1.UnimplementedAuthzServiceServer
	allowed bool
	got     *authzv1.CheckRequest
}

func (s *stubAuthzServer) Check(_ context.Context, req *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	s.got = req
	return &authzv1.CheckResponse{Allowed: s.allowed}, nil
}

// newGRPCEvaluator wires the client to the stub over an in-memory
// listener.
func newGRPCEvaluator(t *testing.T, stub *stubAuthzServer) *machineapi.GRPCEvaluator {
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
	return machineapi.NewGRPCEvaluator(conn)
}

// evalInputWith is one sending question, carrying a statement of the
// shape this listener collects for it.
func evalInputWith(effect wire.Effect) machineapi.EvalInput {
	sender := agentGRN("acme", "one")
	return machineapi.EvalInput{
		Subject: sender,
		Object:  agentGRN("acme", "two"),
		Action:  message.ActionSend,
		Statements: []machineapi.Statement{{
			Sid:      "machine-own-org",
			Effect:   effect,
			Action:   []string{message.ActionSend},
			Resource: []string{"grn:" + sender.Organization + ":*"},
		}},
	}
}

// TestGRPCEvaluator_SendsTheStatementItCollected holds what this client
// adds over the conversions internal/wire tests: the sending question
// reaches the engine carrying its statement, and a statement the
// conversion refuses never reaches it. The accepted question is the
// control on the refusal.
func TestGRPCEvaluator_SendsTheStatementItCollected(t *testing.T) {
	stub := &stubAuthzServer{allowed: true}
	allowed, err := newGRPCEvaluator(t, stub).Evaluate(context.Background(), evalInputWith(wire.EffectAllow))
	require.NoError(t, err)
	assert.True(t, allowed)
	require.Len(t, stub.got.GetStatements(), 1)
	assert.Equal(t, "machine-own-org", stub.got.GetStatements()[0].GetSid())
	assert.Equal(t, authzv1.Effect_EFFECT_ALLOW, stub.got.GetStatements()[0].GetEffect())

	refused := &stubAuthzServer{allowed: true}
	allowed, err = newGRPCEvaluator(t, refused).Evaluate(context.Background(), evalInputWith(wire.Effect("Maybe")))
	assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v", err)
	assert.False(t, allowed)
	assert.Nil(t, refused.got, "an unrecognised Effect reached the engine")
}
