package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gaon/internal/authz"
	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
)

const defaultPolicy = `package gaon.authz

default allow := false

allow if {
    some role in input.subject.roles
    role == "admin"
}

allow if {
    some role in input.subject.roles
    role == "viewer"
    input.action == "doc:read"
}
`

type stubSubject struct {
	grn_     grn.GRN
	typ      string
	roles    []string
	policies []string
}

func (s stubSubject) GRN() grn.GRN       { return s.grn_ }
func (s stubSubject) Type() string       { return s.typ }
func (s stubSubject) Roles() []string    { return s.roles }
func (s stubSubject) Policies() []string { return s.policies }

type stubObject struct {
	grn_ grn.GRN
	typ  string
}

func (o stubObject) GRN() grn.GRN { return o.grn_ }
func (o stubObject) Type() string { return o.typ }

func userGRN(id string) grn.GRN {
	return grn.New("garam", "default", "user", id)
}

func docGRN(id string) grn.GRN {
	return grn.New("garam", "default", "doc", id)
}

func mustEvaluator(t *testing.T, source string) authz.Evaluator {
	t.Helper()
	ev, err := authz.NewRegoEvaluator(context.Background(), source)
	require.NoError(t, err)
	return ev
}

func TestRegoEvaluator_AdminAllow(t *testing.T) {
	ev := mustEvaluator(t, defaultPolicy)
	ctx := context.Background()

	sub := stubSubject{grn_: userGRN("alice"), typ: "user", roles: []string{"admin"}}
	obj := stubObject{grn_: docGRN("readme"), typ: "doc"}

	assert.Equal(t, authz.DecisionAllow, ev.Evaluate(ctx, authz.Context{
		Subject: sub,
		Object:  obj,
		Action:  "doc:write",
	}))
}

func TestRegoEvaluator_ViewerReadOnly(t *testing.T) {
	ev := mustEvaluator(t, defaultPolicy)
	ctx := context.Background()

	sub := stubSubject{grn_: userGRN("alice"), typ: "user", roles: []string{"viewer"}}
	obj := stubObject{grn_: docGRN("readme"), typ: "doc"}

	assert.Equal(t, authz.DecisionAllow, ev.Evaluate(ctx, authz.Context{
		Subject: sub,
		Object:  obj,
		Action:  "doc:read",
	}))
	assert.Equal(t, authz.DecisionDeny, ev.Evaluate(ctx, authz.Context{
		Subject: sub,
		Object:  obj,
		Action:  "doc:write",
	}))
}

func TestRegoEvaluator_NoRolesDeny(t *testing.T) {
	ev := mustEvaluator(t, defaultPolicy)
	ctx := context.Background()

	sub := stubSubject{grn_: userGRN("bob"), typ: "user"}
	obj := stubObject{grn_: docGRN("readme"), typ: "doc"}

	assert.Equal(t, authz.DecisionDeny, ev.Evaluate(ctx, authz.Context{
		Subject: sub,
		Object:  obj,
		Action:  "doc:read",
	}))
}

func TestRoleStore_AssignAndQuery(t *testing.T) {
	ctx := context.Background()
	store := authz.NewMemoryRoleStore()

	alice := userGRN("alice")
	doc := docGRN("readme")

	require.NoError(t, store.Assign(ctx, authz.RoleAssignment{
		SubjectGRN: alice,
		Role:       "viewer",
		Scope:      doc,
	}))

	assert.True(t, errorx.IsKind(store.Assign(ctx, authz.RoleAssignment{
		SubjectGRN: grn.GRN{}, Role: "x", Scope: doc,
	}), errorx.KindInvalidArgument))

	assigns, err := store.RolesOf(ctx, alice)
	require.NoError(t, err)
	require.Len(t, assigns, 1)
	assert.Equal(t, "viewer", assigns[0].Role)

	subjects, err := store.RolesOn(ctx, doc, "viewer")
	require.NoError(t, err)
	assert.Equal(t, []grn.GRN{alice}, subjects)

	require.NoError(t, store.Unassign(ctx, assigns[0]))
	assert.True(t, errorx.IsKind(store.Unassign(ctx, assigns[0]), errorx.KindNotFound))
}

func TestChecker_RoleEnrichment(t *testing.T) {
	ctx := context.Background()
	roles := authz.NewMemoryRoleStore()
	ev := mustEvaluator(t, defaultPolicy)
	checker := authz.NewChecker(roles, ev)

	alice := userGRN("alice")
	doc := docGRN("readme")

	require.NoError(t, roles.Assign(ctx, authz.RoleAssignment{
		SubjectGRN: alice,
		Role:       "viewer",
		Scope:      doc,
	}))

	sub := stubSubject{grn_: alice, typ: "user"}
	obj := stubObject{grn_: doc, typ: "doc"}

	ok, err := checker.Allow(ctx, sub, obj, "doc:read")
	require.NoError(t, err)
	assert.True(t, ok)

	ok, err = checker.Allow(ctx, sub, obj, "doc:write")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestChecker_NilSubjectOrObject(t *testing.T) {
	ctx := context.Background()
	roles := authz.NewMemoryRoleStore()
	ev := mustEvaluator(t, defaultPolicy)
	checker := authz.NewChecker(roles, ev)

	_, err := checker.Allow(ctx, nil, stubObject{grn_: docGRN("r")}, "doc:read")
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))

	_, err = checker.Allow(ctx, stubSubject{grn_: userGRN("u")}, nil, "doc:read")
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

func TestManager_GrantRevokeChanges(t *testing.T) {
	ctx := context.Background()
	roles := authz.NewMemoryRoleStore()
	mgr := authz.NewMemoryManager(roles)

	alice := userGRN("alice")
	doc := docGRN("readme")

	require.NoError(t, mgr.Grant(ctx, authz.Change{
		Granter: userGRN("root"),
		Subject: alice,
		Object:  doc,
		Role:    "viewer",
		Reason:  "for project X",
	}))

	assigns, err := roles.RolesOf(ctx, alice)
	require.NoError(t, err)
	assert.Len(t, assigns, 1)

	changes, err := mgr.ListChanges(ctx, authz.ChangeFilter{Subject: alice})
	require.NoError(t, err)
	require.Len(t, changes, 1)
	assert.Equal(t, authz.ActionGrant, changes[0].Action)

	require.NoError(t, mgr.Revoke(ctx, authz.Change{
		Granter: userGRN("root"),
		Subject: alice,
		Object:  doc,
		Role:    "viewer",
	}))

	assigns, _ = roles.RolesOf(ctx, alice)
	assert.Empty(t, assigns)

	changes, _ = mgr.ListChanges(ctx, authz.ChangeFilter{Subject: alice})
	assert.Len(t, changes, 2)
	assert.Equal(t, authz.ActionRevoke, changes[1].Action)
}

func TestManager_Explain(t *testing.T) {
	ctx := context.Background()
	roles := authz.NewMemoryRoleStore()
	mgr := authz.NewMemoryManager(roles)

	alice := userGRN("alice")
	doc := docGRN("readme")

	require.NoError(t, mgr.Grant(ctx, authz.Change{
		Granter: userGRN("root"),
		Subject: alice,
		Object:  doc,
		Role:    "viewer",
		Reason:  "explicit grant",
	}))

	exp, err := mgr.Explain(ctx, alice, doc, "doc:read")
	require.NoError(t, err)
	assert.True(t, exp.Allowed)
	assert.NotEmpty(t, exp.Traces)
}

func TestPolicyStore_GetAndList(t *testing.T) {
	ctx := context.Background()
	store := authz.NewMemoryPolicyStore()
	store.Put("default", "default policy", defaultPolicy)

	got, err := store.Store.Get(ctx, "default")
	require.NoError(t, err)
	assert.Equal(t, defaultPolicy, got)

	policies, err := store.Store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, policies, 1)
	assert.Equal(t, authz.PolicyID("default"), policies[0].ID)

	_, err = store.Store.Get(ctx, "missing")
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
}
