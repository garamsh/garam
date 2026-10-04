package integration_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// currentCheckAppName is what the engine's proxied connections call
// themselves, so pg_stat_activity tells them from every other one.
const currentCheckAppName = "garam-current-check-integration"

// servedEngine serves the authz engine over gRPC on a TCP port, reading
// its policies through cache, and returns a client of it.
func servedEngine(t *testing.T, cache *authz.PolicyCache) authzv1.AuthzServiceClient {
	t.Helper()
	srv := grpc.NewServer()
	authzv1.RegisterAuthzServiceServer(srv, authz.NewGRPCServer(cache))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return authzv1.NewAuthzServiceClient(conn)
}

// activateStatement stores and activates an org-wide policy granting
// or denying project:read across acme, and returns its GRN.
func activateStatement(t *testing.T, s *authz.PostgresStore, orgGRN grn.GRN, sid string, effect wire.Effect) grn.GRN {
	t.Helper()
	ctx := context.Background()
	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, sid), statementDoc(sid, effect, "project:read", "grn:acme:*"), grnOf("user", "ada"))
	require.NoError(t, err)
	_, err = s.SetPolicyStatus(ctx, g, authz.PolicyStatusActive)
	require.NoError(t, err)
	return g
}

// readProject is ada, a member of acme, asking to read one of its
// projects. It carries no caller statements, so the persisted policies
// alone decide.
func readProject() *authzv1.CheckRequest {
	return &authzv1.CheckRequest{
		SubjectGrn: grnOf("user", "ada").String(),
		ObjectGrn:  grnOf("project", "web").String(),
		Action:     "project:read",
		SubjectOrg: grnOf("org", "acme").String(),
	}
}

// readProjectCurrent asks CheckCurrent what readProject asks Check.
func readProjectCurrent() *authzv1.CheckCurrentRequest {
	r := readProject()
	return &authzv1.CheckCurrentRequest{
		SubjectGrn: r.GetSubjectGrn(),
		ObjectGrn:  r.GetObjectGrn(),
		Action:     r.GetAction(),
		SubjectOrg: r.GetSubjectOrg(),
	}
}

// TestCheckCurrent_SeesAWithdrawalNoInvalidationReported caches an
// allow and withdraws it in Postgres with no invalidation delivered:
// the engine runs no listener, which is the state a held-back or lost
// notification leaves. Check still allowing afterwards is the control:
// it is the documented cached answer. CheckCurrent refusing is then
// the persisted state and not a cache that happened to reload.
//
// No listener runs because one makes the control racy: it drops every
// org once LISTEN registers, and Postgres reports the session before
// that drop has happened, so the drop can land after the fill.
func TestCheckCurrent_SeesAWithdrawalNoInvalidationReported(t *testing.T) {
	withdrawals := []struct {
		name     string
		withdraw func(t *testing.T, s *authz.PostgresStore, orgGRN, allow grn.GRN)
	}{
		{"a persisted Deny", func(t *testing.T, s *authz.PostgresStore, orgGRN, _ grn.GRN) {
			activateStatement(t, s, orgGRN, "deny-read", wire.EffectDeny)
		}},
		{"the Allow revoked", func(t *testing.T, s *authz.PostgresStore, _, allow grn.GRN) {
			_, err := s.SetPolicyStatus(context.Background(), allow, authz.PolicyStatusArchived)
			require.NoError(t, err)
		}},
	}
	for _, w := range withdrawals {
		t.Run(w.name, func(t *testing.T) {
			ctx := context.Background()
			p := pool(t)
			s := authz.NewPostgresStore(p)
			orgGRN := newPolicyOrg(t, p)
			allow := activateStatement(t, s, orgGRN, "allow-read", wire.EffectAllow)
			client := servedEngine(t, authz.NewPolicyCache(s))

			checked, err := client.Check(ctx, readProject())
			require.NoError(t, err)
			require.True(t, checked.GetAllowed(), "the Allow is cached")
			current, err := client.CheckCurrent(ctx, readProjectCurrent())
			require.NoError(t, err)
			require.True(t, current.GetAllowed(), "control: storage allows before the withdrawal")

			w.withdraw(t, s, orgGRN, allow)

			checked, err = client.Check(ctx, readProject())
			require.NoError(t, err)
			assert.True(t, checked.GetAllowed(), "control: no invalidation arrived, so Check answers from the cache")
			current, err = client.CheckCurrent(ctx, readProjectCurrent())
			require.NoError(t, err)
			assert.False(t, current.GetAllowed(), "CheckCurrent decides from what Postgres holds")
		})
	}
}

// TestCheckCurrent_RefusesWhenStorageIsUnreachable cuts the engine's
// own connection to Postgres while its cache allows. Check answering
// from the cache is the control; CheckCurrent fails with no decision.
// The status code is not asserted: the store classifies a dropped
// connection as no kind at all, and that is every store's, not this
// RPC's.
func TestCheckCurrent_RefusesWhenStorageIsUnreachable(t *testing.T) {
	p := pool(t)
	orgGRN := newPolicyOrg(t, p)
	activateStatement(t, authz.NewPostgresStore(p), orgGRN, "allow-read", wire.EffectAllow)

	proxy := newPGProxy(t, databaseAddr(t))
	cache := authz.NewPolicyCache(authz.NewPostgresStore(listenerPool(t, proxy, currentCheckAppName)))
	client := servedEngine(t, cache)

	checked, err := client.Check(context.Background(), readProject())
	require.NoError(t, err)
	require.True(t, checked.GetAllowed(), "the Allow is cached")
	current, err := client.CheckCurrent(context.Background(), readProjectCurrent())
	require.NoError(t, err)
	require.True(t, current.GetAllowed(), "control: reachable storage allows")

	proxy.cut()

	checked, err = client.Check(context.Background(), readProject())
	require.NoError(t, err)
	assert.True(t, checked.GetAllowed(), "control: Check answers from the cache")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	current, err = client.CheckCurrent(ctx, readProjectCurrent())
	require.Error(t, err, "CheckCurrent decides nothing without storage")
	assert.Nil(t, current)
}
