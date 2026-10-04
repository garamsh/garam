package authz_test

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/authz/mocks"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
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

// CurrentStatements answers as Statements does: storage and cache
// agree, so CheckCurrent and Check are asked the same question.
func (p staticPolicies) CurrentStatements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]authz.Statement, error) {
	return p.Statements(ctx, orgGRN, subjects)
}

// staleCache is a PolicySource whose cache still holds what storage
// has since replaced, as an invalidation that has not arrived leaves
// it. currentErr, when set, is storage failing to answer.
type staleCache struct {
	cached     staticPolicies
	current    staticPolicies
	currentErr error
}

func (p staleCache) Statements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]authz.Statement, error) {
	return p.cached.Statements(ctx, orgGRN, subjects)
}

func (p staleCache) CurrentStatements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]authz.Statement, error) {
	if p.currentErr != nil {
		return nil, p.currentErr
	}
	return p.current.Statements(ctx, orgGRN, subjects)
}

// newAuthzClient serves the real engine over an in-memory listener.
func newAuthzClient(t *testing.T, policies authz.PolicySource) authzv1.AuthzServiceClient {
	t.Helper()
	srv := grpc.NewServer()
	authzv1.RegisterAuthzServiceServer(srv, authz.NewGRPCServer(policies))
	return serveAuthz(t, srv)
}

// serveAuthz serves srv over an in-memory listener and returns a
// client of it.
func serveAuthz(t *testing.T, srv *grpc.Server) authzv1.AuthzServiceClient {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
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
		// alice is the request fixture's member of acme; without this
		// the scope check refuses the call before any statement runs.
		SubjectOrg: "grn:root:default:org:acme",
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

// TestGRPCServer_CheckInvalidInput reads the unmutated request against
// the six spoiled ones: each of those is that request with one field
// spoiled, so a Check that refuses everything stops satisfying the
// first row while still satisfying the rest.
func TestGRPCServer_CheckInvalidInput(t *testing.T) {
	client := newAuthzClient(t, staticPolicies{})

	cases := []struct {
		name   string
		mutate func(r *authzv1.CheckRequest)
		want   codes.Code
	}{
		{"unmutated", func(*authzv1.CheckRequest) {}, codes.OK},
		{"bad subject grn", func(r *authzv1.CheckRequest) { r.SubjectGrn = "nope" }, codes.InvalidArgument},
		{"bad object grn", func(r *authzv1.CheckRequest) { r.ObjectGrn = "nope" }, codes.InvalidArgument},
		{"empty action", func(r *authzv1.CheckRequest) { r.Action = "" }, codes.InvalidArgument},
		{"unspecified effect", func(r *authzv1.CheckRequest) { r.Statements[0].Effect = authzv1.Effect_EFFECT_UNSPECIFIED }, codes.InvalidArgument},
		{"empty patterns", func(r *authzv1.CheckRequest) { r.Statements[0].Action = nil }, codes.InvalidArgument},
		{"bad subject group grn", func(r *authzv1.CheckRequest) { r.SubjectGroups = []string{"nope"} }, codes.InvalidArgument},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := checkRequest(allowStatement("s1", []string{"org:*"}, []string{"grn:root:default:org:*"}))
			c.mutate(req)
			_, err := client.Check(context.Background(), req)
			assert.Equal(t, c.want, status.Code(err))
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
		SubjectOrg: "grn:root:default:org:acme",
	})
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed(), "explicit deny wins")

	traces := map[string]*authzv1.StatementTrace{}
	for _, tr := range resp.GetTraces() {
		traces[tr.GetSid()] = tr
	}
	require.Len(t, traces, 3)
	// The trace has to be present before its Matched is read: a sid the
	// engine mislabelled is absent from the map, and an absent trace
	// reports Matched false exactly as a present one that did not match.
	require.Contains(t, traces, "wrong-action")
	assert.True(t, traces["allow-all"].GetMatched())
	assert.False(t, traces["wrong-action"].GetMatched())
	assert.True(t, traces["deny-delete"].GetMatched())
	assert.Equal(t, authzv1.Effect_EFFECT_DENY, traces["deny-delete"].GetEffect())

	// The control for the refusal above: where only an Allow matches,
	// Explain reports Allowed. The request above matches an Allow and a
	// Deny, which sets both flags, so it reports false whichever effect
	// the engine counts as a deny and only this case reads the branch.
	allowOnly, err := client.Explain(context.Background(), &authzv1.ExplainRequest{
		SubjectGrn: "grn:root:default:user:alice",
		ObjectGrn:  "grn:root:default:org:acme",
		Action:     "org:delete",
		Statements: []*authzv1.Statement{
			allowStatement("allow-all", []string{"org:*"}, []string{"grn:root:default:org:*"}),
			{Sid: "deny-other-org", Effect: authzv1.Effect_EFFECT_DENY, Action: []string{"org:delete"}, Resource: []string{"grn:root:default:org:globex"}},
		},
		SubjectOrg: "grn:root:default:org:acme",
	})
	require.NoError(t, err)
	assert.True(t, allowOnly.GetAllowed(), "an Allow matches and the Deny does not")
}

// TestGRPCServer_ExplainAllowsWhatOnlyAnAllowMatches covers the answer
// the two tests around it cannot: both assert a refusal, and a trace
// that read every effect backwards would still refuse both of them —
// the allow reads as a deny and the deny as an allow, and
// `allowed && !denied` comes back false either way. Explain exists to
// say why a decision went as it did, so the decision it reports has to
// be right in both directions.
func TestGRPCServer_ExplainAllowsWhatOnlyAnAllowMatches(t *testing.T) {
	client := newAuthzClient(t, staticPolicies{})

	resp, err := client.Explain(context.Background(), &authzv1.ExplainRequest{
		SubjectGrn: "grn:root:default:user:alice",
		ObjectGrn:  "grn:root:default:org:acme",
		Action:     "org:delete",
		Statements: []*authzv1.Statement{
			allowStatement("allow-delete", []string{"org:delete"}, []string{"grn:root:default:org:acme"}),
			// The control: a statement that matches nothing, so what
			// admits the call is the allow above rather than an engine
			// that admits whatever it is handed.
			allowStatement("wrong-action", []string{"org:read"}, []string{"grn:root:default:org:acme"}),
		},
		SubjectOrg: "grn:root:default:org:acme",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed(), "an allow matched and nothing denied")

	traces := map[string]*authzv1.StatementTrace{}
	for _, tr := range resp.GetTraces() {
		traces[tr.GetSid()] = tr
	}
	require.Contains(t, traces, "allow-delete")
	require.Contains(t, traces, "wrong-action")
	assert.True(t, traces["allow-delete"].GetMatched())
	assert.False(t, traces["wrong-action"].GetMatched())
	assert.Equal(t, authzv1.Effect_EFFECT_ALLOW, traces["allow-delete"].GetEffect())
}

func orgGRN(id string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "org", id)
}

func TestGRPCServer_CheckPersistedPolicies(t *testing.T) {
	acme := orgGRN("acme")
	allowUpdate := authz.Statement{
		Sid:      "persisted-allow",
		Effect:   wire.EffectAllow,
		Action:   []string{"org:update"},
		Resource: []string{"grn:root:default:org:acme", "grn:acme:*"},
	}
	denyUpdate := authz.Statement{
		Sid:      "persisted-deny",
		Effect:   wire.EffectDeny,
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
		Effect:   wire.EffectAllow,
		Action:   []string{"org:update"},
		Resource: []string{"grn:root:default:org:acme"},
	}
	denyUpdate := authz.Statement{
		Sid:      "attached-deny",
		Effect:   wire.EffectDeny,
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
		Effect:   wire.EffectDeny,
		Action:   []string{"org:update"},
		Resource: []string{"grn:root:default:org:acme"},
	}}})

	resp, err := client.Explain(context.Background(), &authzv1.ExplainRequest{
		SubjectGrn: "grn:root:default:user:alice",
		ObjectGrn:  "grn:root:default:org:acme",
		Action:     "org:update",
		Statements: []*authzv1.Statement{allowStatement("org-role-owner", []string{"org:*"}, []string{"grn:root:default:org:acme"})},
		SubjectOrg: "grn:root:default:org:acme",
	})
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed())
	require.Len(t, resp.GetTraces(), 2)
	assert.Equal(t, "persisted-deny", resp.GetTraces()[0].GetSid())
	assert.True(t, resp.GetTraces()[0].GetMatched())
}

// TestGRPCServer_RefusesForeignSubjectWhenSubjectOrgUnset pins the
// boundary ADR-0025 deferred and ADR-0038 closed: an unattached Allow
// in another organization must not admit a subject whose membership
// is not established, and the engine is where the refusal lives. The
// cross-package fixture (#296) named the same property from the api
// side; the engine now holds it directly, so the engine suite asserts
// it.
//
// Three cases together bound the property: an unset subject_org
// refuses the call, a foreign subject_org refuses the call, and a
// matching subject_org is evaluated normally — so an engine stuck open
// fails the first two rather than passing all three. The unattached
// Allow in globex is the case that needed no new document: an
// organization holding a wildcard policy written before the engine's
// scope check moved into it is exposed the day the check is removed,
// not the day a Deny is written.
func TestGRPCServer_RefusesForeignSubjectWhenSubjectOrgUnset(t *testing.T) {
	acme := orgGRN("acme")
	globex := orgGRN("globex")
	// An unattached Allow in globex, scoped to a globex resource. No
	// attachment stands between this statement and a subject of any
	// org, which is the precondition for #265.
	openDoor := authz.Statement{
		Sid:      "open-door",
		Effect:   wire.EffectAllow,
		Action:   []string{"*"},
		Resource: []string{"grn:globex:default:project:web"},
	}
	client := newAuthzClient(t, staticPolicies{globex: {openDoor}})

	subject := "grn:root:default:user:alice"
	object := "grn:globex:default:project:web"

	t.Run("subject_org unset refuses before any statement runs", func(t *testing.T) {
		_, err := client.Check(context.Background(), &authzv1.CheckRequest{
			SubjectGrn: subject,
			ObjectGrn:  object,
			Action:     "project:read",
		})
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.Contains(t, err.Error(), "subject_org is required")
	})

	t.Run("subject_org set to a foreign org refuses", func(t *testing.T) {
		_, err := client.Check(context.Background(), &authzv1.CheckRequest{
			SubjectGrn: subject,
			ObjectGrn:  object,
			Action:     "project:read",
			// The subject was established as belonging to acme, not
			// globex: the engine still refuses the cross-org call.
			SubjectOrg: acme.String(),
		})
		require.Error(t, err)
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
		assert.Contains(t, err.Error(), "subject is not a member")
	})

	t.Run("subject_org matching the object's org is evaluated normally", func(t *testing.T) {
		resp, err := client.Check(context.Background(), &authzv1.CheckRequest{
			SubjectGrn: "grn:root:default:user:globex-admin",
			ObjectGrn:  object,
			Action:     "project:read",
			SubjectOrg: globex.String(),
		})
		require.NoError(t, err)
		assert.True(t, resp.GetAllowed(), "the unattached Allow in globex reaches a globex subject")
	})
}

// TestGRPCServer_RefusesFromAPolicyArchivedDuringAFill is what the
// cache's lost invalidation costs at the surface that answers: the
// engine allowed an action from a policy that had been archived, and
// went on allowing it because nothing dropped the entry a second time.
//
// The archive lands while a check is inside the store, which is the one
// interleaving that reaches it — PolicyGRPCServer.ArchivePolicy
// invalidates after its commit, and the same process serves checks
// against that cache. The decision the in-flight check returned is the
// control: the policy did allow before it was archived, so the refusal
// afterwards is the archive rather than an engine that refuses
// everything.
func TestGRPCServer_RefusesFromAPolicyArchivedDuringAFill(t *testing.T) {
	acme := orgGRN("acme")
	gate := newGatedFill()
	live := authz.ActivePolicy{Document: authz.PolicyDocument{
		Version: "2026-07-25",
		Statement: []authz.Statement{{
			Sid:      "archived-allow",
			Effect:   wire.EffectAllow,
			Action:   []string{"org:update"},
			Resource: []string{"grn:root:default:org:acme"},
		}},
	}}

	store := mocks.NewMockStore(t)
	store.EXPECT().ActivePolicies(mock.Anything, acme).
		RunAndReturn(gate.answer([]authz.ActivePolicy{live}))

	cache := authz.NewPolicyCache(store)
	engine := authz.NewGRPCServer(cache)

	var allowedBefore bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := engine.Check(context.Background(), checkRequest())
		assert.NoError(t, err)
		allowedBefore = resp.GetAllowed()
	}()

	<-gate.inStore
	cache.Invalidate(acme)
	close(gate.release)
	wg.Wait()
	assert.True(t, allowedBefore, "the policy allowed while it was still active")

	resp, err := engine.Check(context.Background(), checkRequest())
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed(), "the archived policy still allows")
}

// currentRequest asks CheckCurrent what req asks Check.
func currentRequest(req *authzv1.CheckRequest) *authzv1.CheckCurrentRequest {
	return &authzv1.CheckCurrentRequest{
		SubjectGrn:    req.GetSubjectGrn(),
		ObjectGrn:     req.GetObjectGrn(),
		Action:        req.GetAction(),
		Statements:    req.GetStatements(),
		SubjectGroups: req.GetSubjectGroups(),
		SubjectOrg:    req.GetSubjectOrg(),
	}
}

// TestGRPCServer_CheckCurrentDecidesAsCheckDoes asks both RPCs the same
// question over a source whose cache and storage agree. Each case also
// names its expected answer, so two RPCs wrong the same way still fail.
func TestGRPCServer_CheckCurrentDecidesAsCheckDoes(t *testing.T) {
	acme := orgGRN("acme")
	eng := grn.New("acme", grn.DefaultProject, "group", "eng")
	allowUpdate := authz.Statement{
		Sid:      "persisted-allow",
		Effect:   wire.EffectAllow,
		Action:   []string{"org:update"},
		Resource: []string{"grn:root:default:org:acme"},
	}
	denyUpdate := authz.Statement{
		Sid:      "persisted-deny",
		Effect:   wire.EffectDeny,
		Action:   []string{"org:update"},
		Resource: []string{"grn:root:default:org:acme"},
	}
	callerAllow := allowStatement("org-role-owner", []string{"org:*"}, []string{"grn:root:default:org:acme"})

	cases := []struct {
		name     string
		policies staticPolicies
		mutate   func(r *authzv1.CheckRequest)
		code     codes.Code
		allowed  bool
	}{
		{"persisted allow", staticPolicies{acme: {allowUpdate}}, func(*authzv1.CheckRequest) {}, codes.OK, true},
		{"no statement matches", staticPolicies{}, func(*authzv1.CheckRequest) {}, codes.OK, false},
		{"persisted deny overrides a caller allow", staticPolicies{acme: {denyUpdate}}, func(r *authzv1.CheckRequest) {
			r.Statements = []*authzv1.Statement{callerAllow}
		}, codes.OK, false},
		{"group-attached deny overrides a caller allow", staticPolicies{eng: {denyUpdate}}, func(r *authzv1.CheckRequest) {
			r.Statements = []*authzv1.Statement{callerAllow}
			r.SubjectGroups = []string{eng.String()}
		}, codes.OK, false},
		{"another org's policies do not apply", staticPolicies{orgGRN("other"): {allowUpdate}}, func(*authzv1.CheckRequest) {}, codes.OK, false},
		{"subject of another org is refused", staticPolicies{acme: {allowUpdate}}, func(r *authzv1.CheckRequest) {
			r.SubjectOrg = "grn:root:default:org:globex"
		}, codes.PermissionDenied, false},
		{"unset subject org is refused", staticPolicies{acme: {allowUpdate}}, func(r *authzv1.CheckRequest) {
			r.SubjectOrg = ""
		}, codes.PermissionDenied, false},
		{"empty action is invalid", staticPolicies{acme: {allowUpdate}}, func(r *authzv1.CheckRequest) {
			r.Action = ""
		}, codes.InvalidArgument, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := newAuthzClient(t, c.policies)
			req := checkRequest()
			c.mutate(req)

			checked, err := client.Check(context.Background(), req)
			assert.Equal(t, c.code, status.Code(err), "Check")
			assert.Equal(t, c.allowed, checked.GetAllowed(), "Check")

			current, err := client.CheckCurrent(context.Background(), currentRequest(req))
			assert.Equal(t, c.code, status.Code(err), "CheckCurrent")
			assert.Equal(t, c.allowed, current.GetAllowed(), "CheckCurrent")
		})
	}
}

// TestGRPCServer_CheckCurrentDecidesFromStorage is the stale cache a
// held-back invalidation leaves: it still holds the Allow storage has
// since replaced with a Deny. Check allowing is the control that the
// cache is stale, so CheckCurrent refusing is the storage read.
func TestGRPCServer_CheckCurrentDecidesFromStorage(t *testing.T) {
	acme := orgGRN("acme")
	update := func(sid string, effect wire.Effect) authz.Statement {
		return authz.Statement{Sid: sid, Effect: effect, Action: []string{"org:update"}, Resource: []string{"grn:root:default:org:acme"}}
	}
	client := newAuthzClient(t, staleCache{
		cached:  staticPolicies{acme: {update("allow", wire.EffectAllow)}},
		current: staticPolicies{acme: {update("allow", wire.EffectAllow), update("deny", wire.EffectDeny)}},
	})

	checked, err := client.Check(context.Background(), checkRequest())
	require.NoError(t, err)
	assert.True(t, checked.GetAllowed(), "control: the cache still allows")

	current, err := client.CheckCurrent(context.Background(), currentRequest(checkRequest()))
	require.NoError(t, err)
	assert.False(t, current.GetAllowed(), "the persisted Deny decides")
}

// TestGRPCServer_CheckCurrentRefusesWhenStorageFails holds a cache
// that allows: Check answering from it is the control, and the error
// CheckCurrent returns instead is storage's, with no decision beside
// it.
func TestGRPCServer_CheckCurrentRefusesWhenStorageFails(t *testing.T) {
	acme := orgGRN("acme")
	allow := authz.Statement{Sid: "allow", Effect: wire.EffectAllow, Action: []string{"org:update"}, Resource: []string{"grn:root:default:org:acme"}}
	client := newAuthzClient(t, staleCache{
		cached:     staticPolicies{acme: {allow}},
		currentErr: errorx.New(errorx.KindUnavailable, "database unreachable"),
	})

	checked, err := client.Check(context.Background(), checkRequest())
	require.NoError(t, err)
	assert.True(t, checked.GetAllowed(), "control: the cache allows")

	current, err := client.CheckCurrent(context.Background(), currentRequest(checkRequest()))
	assert.Equal(t, codes.Unavailable, status.Code(err))
	assert.Nil(t, current)
}

// allowingEngine allows every Check it is asked.
type allowingEngine struct {
	authzv1.UnimplementedAuthzServiceServer
}

func (allowingEngine) Check(context.Context, *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	return &authzv1.CheckResponse{Allowed: true}, nil
}

// TestAuthzService_AnEngineWithoutCheckCurrentRefusesIt serves the
// service as an engine predating CheckCurrent registered it: the
// methods it knew, and no CheckCurrent among them. That engine allows
// every Check, which is the control — a request it read as a Check
// would come back allowed — and CheckCurrent reaches no handler at all.
func TestAuthzService_AnEngineWithoutCheckCurrentRefusesIt(t *testing.T) {
	older := authzv1.AuthzService_ServiceDesc
	older.Methods = nil
	for _, m := range authzv1.AuthzService_ServiceDesc.Methods {
		if m.MethodName != "CheckCurrent" {
			older.Methods = append(older.Methods, m)
		}
	}
	require.Len(t, older.Methods, 2, "the older engine serves Check and Explain")
	srv := grpc.NewServer()
	srv.RegisterService(&older, allowingEngine{})
	client := serveAuthz(t, srv)

	checked, err := client.Check(context.Background(), checkRequest())
	require.NoError(t, err)
	assert.True(t, checked.GetAllowed(), "control: the older engine allows a Check")

	current, err := client.CheckCurrent(context.Background(), currentRequest(checkRequest()))
	assert.Equal(t, codes.Unimplemented, status.Code(err))
	assert.Nil(t, current)
}
