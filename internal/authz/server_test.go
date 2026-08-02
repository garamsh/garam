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

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/grn"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// staticPolicies is a PolicySource over a fixed per-org statement set,
// standing in for the cache the engine reads in production. Statements
// keyed by a subject GRN stand for a policy attached to that subject.
type staticPolicies map[grn.GRN][]authz.Statement

func (p staticPolicies) Statements(_ context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]authz.Statement, error) {
	var statements []authz.Statement
	statements = append(statements, p[orgGRN]...)
	for _, s := range subjects {
		statements = append(statements, p[s]...)
	}
	return statements, nil
}

// newAuthzClient serves the real engine over an in-memory listener.
func newAuthzClient(t *testing.T, policies staticPolicies) authzv1.AuthzServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	authzv1.RegisterAuthzServiceServer(srv, authz.NewGRPCServer(policies))
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
	client := newAuthzClient(t, staticPolicies{})

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
	client := newAuthzClient(t, staticPolicies{})

	cases := []struct {
		name   string
		mutate func(r *authzv1.CheckRequest)
	}{
		{"bad subject grn", func(r *authzv1.CheckRequest) { r.SubjectGrn = "nope" }},
		{"bad object grn", func(r *authzv1.CheckRequest) { r.ObjectGrn = "nope" }},
		{"empty action", func(r *authzv1.CheckRequest) { r.Action = "" }},
		{"unspecified effect", func(r *authzv1.CheckRequest) { r.Statements[0].Effect = authzv1.Effect_EFFECT_UNSPECIFIED }},
		{"empty patterns", func(r *authzv1.CheckRequest) { r.Statements[0].Action = nil }},
		{"bad subject group grn", func(r *authzv1.CheckRequest) { r.SubjectGroups = []string{"nope"} }},
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
	client := newAuthzClient(t, staticPolicies{})

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

func orgGRN(id string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "org", id)
}

func TestGRPCServer_CheckPersistedPolicies(t *testing.T) {
	acme := orgGRN("acme")
	allowUpdate := authz.Statement{
		Sid:      "persisted-allow",
		Effect:   authz.EffectAllow,
		Action:   []string{"org:update"},
		Resource: []string{"grn:root:default:org:acme", "grn:acme:*"},
	}
	denyUpdate := authz.Statement{
		Sid:      "persisted-deny",
		Effect:   authz.EffectDeny,
		Action:   []string{"org:update"},
		Resource: []string{"grn:root:default:org:acme", "grn:acme:*"},
	}
	callerAllow := allowStatement("org-role-owner", []string{"org:*"}, []string{"grn:root:default:org:acme", "grn:acme:*"})

	cases := []struct {
		name      string
		policies  staticPolicies
		object    string
		statement *authzv1.Statement
		want      bool
	}{
		{
			"persisted allow decides without caller statements",
			staticPolicies{acme: {allowUpdate}},
			"grn:root:default:org:acme",
			nil,
			true,
		},
		{
			"persisted deny overrides a caller allow",
			staticPolicies{acme: {denyUpdate}},
			"grn:root:default:org:acme",
			callerAllow,
			false,
		},
		{
			"another org's policies do not apply",
			staticPolicies{orgGRN("other"): {allowUpdate}},
			"grn:root:default:org:acme",
			nil,
			false,
		},
		{
			"a resource under the org resolves to the org's policies",
			staticPolicies{acme: {denyUpdate}},
			"grn:acme:default:group:eng",
			callerAllow,
			false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := checkRequest()
			req.ObjectGrn = c.object
			if c.statement != nil {
				req.Statements = []*authzv1.Statement{c.statement}
			}
			resp, err := newAuthzClient(t, c.policies).Check(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, c.want, resp.GetAllowed())
		})
	}
}

// TestGRPCServer_CheckAttachedPolicies covers the attachment sources a
// check unions (ADR-0008): the subject itself and the groups it is
// reported to belong to. Deny-overrides holds across them.
func TestGRPCServer_CheckAttachedPolicies(t *testing.T) {
	alice := grn.New(grn.RootOwner, grn.DefaultProject, "user", "alice")
	eng := grn.New("acme", grn.DefaultProject, "group", "eng")
	allowUpdate := authz.Statement{
		Sid:      "attached-allow",
		Effect:   authz.EffectAllow,
		Action:   []string{"org:update"},
		Resource: []string{"grn:root:default:org:acme"},
	}
	denyUpdate := authz.Statement{
		Sid:      "attached-deny",
		Effect:   authz.EffectDeny,
		Action:   []string{"org:update"},
		Resource: []string{"grn:root:default:org:acme"},
	}
	callerAllow := allowStatement("org-role-owner", []string{"org:*"}, []string{"grn:root:default:org:acme"})

	cases := []struct {
		name     string
		policies staticPolicies
		groups   []string
		caller   *authzv1.Statement
		want     bool
	}{
		{"attached to the subject", staticPolicies{alice: {allowUpdate}}, nil, nil, true},
		{"attached to the subject's group", staticPolicies{eng: {allowUpdate}}, []string{eng.String()}, nil, true},
		{"attached to a group the subject is not in", staticPolicies{eng: {allowUpdate}}, nil, nil, false},
		{"a group deny overrides a caller allow", staticPolicies{eng: {denyUpdate}}, []string{eng.String()}, callerAllow, false},
		{"a subject deny overrides a caller allow", staticPolicies{alice: {denyUpdate}}, nil, callerAllow, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := checkRequest()
			req.SubjectGroups = c.groups
			if c.caller != nil {
				req.Statements = []*authzv1.Statement{c.caller}
			}
			resp, err := newAuthzClient(t, c.policies).Check(context.Background(), req)
			require.NoError(t, err)
			assert.Equal(t, c.want, resp.GetAllowed())
		})
	}
}

func TestGRPCServer_ExplainTracesPersistedStatements(t *testing.T) {
	client := newAuthzClient(t, staticPolicies{orgGRN("acme"): {{
		Sid:      "persisted-deny",
		Effect:   authz.EffectDeny,
		Action:   []string{"org:update"},
		Resource: []string{"grn:root:default:org:acme"},
	}}})

	resp, err := client.Explain(context.Background(), &authzv1.ExplainRequest{
		SubjectGrn: "grn:root:default:user:alice",
		ObjectGrn:  "grn:root:default:org:acme",
		Action:     "org:update",
		Statements: []*authzv1.Statement{allowStatement("org-role-owner", []string{"org:*"}, []string{"grn:root:default:org:acme"})},
	})
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed())
	require.Len(t, resp.GetTraces(), 2)
	assert.Equal(t, "persisted-deny", resp.GetTraces()[0].GetSid())
	assert.True(t, resp.GetTraces()[0].GetMatched())
}
