package authz_test

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

	"github.com/garamsh/gaon/internal/authz"
	authzv1 "github.com/garamsh/gaon/proto/gen/gaon/authz/v1"
)

// newAuthzClient serves the real engine over an in-memory listener.
func newAuthzClient(t *testing.T) authzv1.AuthzServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	authzv1.RegisterAuthzServiceServer(srv, authz.NewGRPCServer())
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
	return authzv1.NewAuthzServiceClient(conn)
}

func allowStatement(sid string, actions, resources []string) *authzv1.Statement {
	return &authzv1.Statement{
		Sid:      sid,
		Effect:   authzv1.Effect_EFFECT_ALLOW,
		Action:   actions,
		Resource: resources,
	}
}

func checkRequest(statements ...*authzv1.Statement) *authzv1.CheckRequest {
	return &authzv1.CheckRequest{
		SubjectGrn: "grn:root:default:user:alice",
		ObjectGrn:  "grn:root:default:org:acme",
		Action:     "org:update",
		Statements: statements,
	}
}

func TestGRPCServer_Check(t *testing.T) {
	client := newAuthzClient(t)

	cases := []struct {
		name       string
		statements []*authzv1.Statement
		want       bool
	}{
		{
			"matching allow",
			[]*authzv1.Statement{allowStatement("s1", []string{"org:*"}, []string{"grn:root:default:org:*"})},
			true,
		},
		{
			"default deny",
			[]*authzv1.Statement{allowStatement("s1", []string{"org:read"}, []string{"grn:root:default:org:*"})},
			false,
		},
		{
			"explicit deny wins",
			[]*authzv1.Statement{
				allowStatement("s1", []string{"org:*"}, []string{"grn:root:default:org:*"}),
				{Sid: "s2", Effect: authzv1.Effect_EFFECT_DENY, Action: []string{"org:update"}, Resource: []string{"grn:root:default:org:acme"}},
			},
			false,
		},
		{
			"no statements deny",
			nil,
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp, err := client.Check(context.Background(), checkRequest(c.statements...))
			require.NoError(t, err)
			assert.Equal(t, c.want, resp.GetAllowed())
		})
	}
}

func TestGRPCServer_CheckInvalidInput(t *testing.T) {
	client := newAuthzClient(t)

	cases := []struct {
		name   string
		mutate func(r *authzv1.CheckRequest)
	}{
		{"bad subject grn", func(r *authzv1.CheckRequest) { r.SubjectGrn = "nope" }},
		{"bad object grn", func(r *authzv1.CheckRequest) { r.ObjectGrn = "nope" }},
		{"empty action", func(r *authzv1.CheckRequest) { r.Action = "" }},
		{"unspecified effect", func(r *authzv1.CheckRequest) { r.Statements[0].Effect = authzv1.Effect_EFFECT_UNSPECIFIED }},
		{"empty patterns", func(r *authzv1.CheckRequest) { r.Statements[0].Action = nil }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := checkRequest(allowStatement("s1", []string{"org:*"}, []string{"grn:root:default:org:*"}))
			c.mutate(req)
			_, err := client.Check(context.Background(), req)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

func TestGRPCServer_Explain(t *testing.T) {
	client := newAuthzClient(t)

	resp, err := client.Explain(context.Background(), &authzv1.ExplainRequest{
		SubjectGrn: "grn:root:default:user:alice",
		ObjectGrn:  "grn:root:default:org:acme",
		Action:     "org:delete",
		Statements: []*authzv1.Statement{
			allowStatement("allow-all", []string{"org:*"}, []string{"grn:root:default:org:*"}),
			allowStatement("wrong-action", []string{"org:read"}, []string{"grn:root:default:org:*"}),
			{Sid: "deny-delete", Effect: authzv1.Effect_EFFECT_DENY, Action: []string{"org:delete"}, Resource: []string{"grn:root:default:org:acme"}},
		},
	})
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed(), "explicit deny wins")

	traces := map[string]*authzv1.StatementTrace{}
	for _, tr := range resp.GetTraces() {
		traces[tr.GetSid()] = tr
	}
	require.Len(t, traces, 3)
	assert.True(t, traces["allow-all"].GetMatched())
	assert.False(t, traces["wrong-action"].GetMatched())
	assert.True(t, traces["deny-delete"].GetMatched())
	assert.Equal(t, authzv1.Effect_EFFECT_DENY, traces["deny-delete"].GetEffect())
}
