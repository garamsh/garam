package integration_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/platform/internal/authz"
	authzpg "github.com/garamsh/garam/platform/internal/authz/postgres"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/wire"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
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
func activateStatement(t *testing.T, s *authzpg.Store, orgGRN grn.GRN, sid string, effect wire.Effect) grn.GRN {
	t.Helper()
	ctx := context.Background()
	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, sid), statementDoc(sid, effect, "project:read", "grn:acme:*"), grnOf("user", "ada"))
	require.NoError(t, err)
	_, err = s.SetPolicyStatus(ctx, g, wire.PolicyStatusActive)
	require.NoError(t, err)
	return g
}

// readProject asks whether ada may read an acme project, with no
// caller statements, so the persisted policies alone decide.
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

// TestCheckCurrent_SeesAWithdrawalNoInvalidationReported withdraws a
// cached allow in Postgres with no listener running, so the cache stays
// stale: Check still allows, and CheckCurrent sees the withdrawal.
func TestCheckCurrent_SeesAWithdrawalNoInvalidationReported(t *testing.T) {
	withdrawals := []struct {
		name     string
		withdraw func(t *testing.T, s *authzpg.Store, orgGRN, allow grn.GRN)
	}{
		{"a persisted Deny", func(t *testing.T, s *authzpg.Store, orgGRN, _ grn.GRN) {
			activateStatement(t, s, orgGRN, "deny-read", wire.EffectDeny)
		}},
		{"the Allow revoked", func(t *testing.T, s *authzpg.Store, _, allow grn.GRN) {
			_, err := s.DeletePolicy(context.Background(), allow)
			require.NoError(t, err)
		}},
	}
	for _, w := range withdrawals {
		t.Run(w.name, func(t *testing.T) {
			ctx := context.Background()
			p := pool(t)
			s := authzpg.New(p)
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
// connection to Postgres while its cache allows: Check still allows,
// and CheckCurrent fails with no decision, answered Unavailable (#1165)
// both on the connection the cut ended and on the dial after it, which
// the proxy refuses. The call before the cut is the reachable control.
// The connection is cut two ways: ended, which the driver reads as an
// end of stream, and reset, which it reads as a socket error on its
// next write or read — the form a loaded host met when its read came
// after the reset arrived (#1546).
func TestCheckCurrent_RefusesWhenStorageIsUnreachable(t *testing.T) {
	cuts := map[string]func(*pgProxy){
		"ended": (*pgProxy).cut,
		"reset": (*pgProxy).reset,
	}
	for name, cut := range cuts {
		t.Run(name, func(t *testing.T) {
			p := pool(t)
			orgGRN := newPolicyOrg(t, p)
			activateStatement(t, authzpg.New(p), orgGRN, "allow-read", wire.EffectAllow)

			proxy := newPGProxy(t, databaseAddr(t))
			cache := authz.NewPolicyCache(authzpg.New(listenerPool(t, proxy, currentCheckAppName)))
			client := servedEngine(t, cache)

			checked, err := client.Check(context.Background(), readProject())
			require.NoError(t, err)
			require.True(t, checked.GetAllowed(), "the Allow is cached")
			current, err := client.CheckCurrent(context.Background(), readProjectCurrent())
			require.NoError(t, err)
			require.True(t, current.GetAllowed(), "control: reachable storage allows")

			cut(proxy)

			checked, err = client.Check(context.Background(), readProject())
			require.NoError(t, err)
			assert.True(t, checked.GetAllowed(), "control: Check answers from the cache")
			for _, attempt := range []string{"on the connection the cut ended", "on the dial after it"} {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				current, err = client.CheckCurrent(ctx, readProjectCurrent())
				cancel()
				require.Error(t, err, "CheckCurrent decides nothing without storage, %s", attempt)
				assert.Nil(t, current)
				assert.Equal(t, codes.Unavailable, status.Code(err), "an unreachable store is Unavailable, %s", attempt)
			}
		})
	}
}
