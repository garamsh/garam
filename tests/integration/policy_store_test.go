package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

func TestPolicyStore_LifecycleRoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)
	author := grnOf("user", "ada")

	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	created, err := s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", wire.EffectAllow), author)
	require.NoError(t, err)
	assert.Equal(t, wire.PolicyStatusDraft, created.Status())
	assert.Equal(t, 1, created.DefaultVersion())

	policies, err := s.ListPolicies(ctx, orgGRN)
	require.NoError(t, err)
	require.Len(t, policies, 1)
	assert.Equal(t, "read-only", policies[0].Name())

	// A draft contributes nothing until it is activated.
	active, err := s.ActivePolicies(ctx, orgGRN)
	require.NoError(t, err)
	assert.Empty(t, active)

	activated, err := s.SetPolicyStatus(ctx, g, wire.PolicyStatusActive)
	require.NoError(t, err)
	assert.Equal(t, wire.PolicyStatusActive, activated.Status())
	active, err = s.ActivePolicies(ctx, orgGRN)
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, "v1", active[0].Document.Statement[0].Sid)
	assert.Empty(t, active[0].Subjects, "an unattached policy applies org-wide")

	// Appending a version leaves the default pointer alone.
	v2, err := s.AddPolicyVersion(ctx, g, policyDoc("v2", wire.EffectDeny), author)
	require.NoError(t, err)
	assert.Equal(t, 2, v2.Version())
	active, err = s.ActivePolicies(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, "v1", active[0].Document.Statement[0].Sid)

	rolled, err := s.SetPolicyDefaultVersion(ctx, g, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, rolled.DefaultVersion())
	active, err = s.ActivePolicies(ctx, orgGRN)
	require.NoError(t, err)
	assert.Equal(t, "v2", active[0].Document.Statement[0].Sid)
	assert.Equal(t, wire.EffectDeny, active[0].Document.Statement[0].Effect)

	versions, err := s.ListPolicyVersions(ctx, g)
	require.NoError(t, err)
	require.Len(t, versions, 2)
	assert.Equal(t, author, versions[0].CreatedBy())

	_, err = s.SetPolicyDefaultVersion(ctx, g, 99)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	_, err = s.GetPolicy(ctx, grn.New("acme", "default", authz.ServiceTypePolicy, "missing"))
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	_, err = s.AddPolicyVersion(ctx, grn.New("acme", "default", authz.ServiceTypePolicy, "missing"), policyDoc("x", wire.EffectAllow), author)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
}

// TestPolicyService_ArchiveRoundTrip drives the lifecycle service over
// the real store: activate, then archive, and the org keeps nothing to
// evaluate.
func TestPolicyService_ArchiveRoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(s)
	srv := authz.NewPolicyGRPCServer(s, cache)
	orgGRN := newPolicyOrg(t, p)

	created, err := srv.CreatePolicy(ctx, &authzv1.CreatePolicyRequest{
		OrgGrn:    orgGRN.String(),
		Name:      "read-only",
		Document:  `{"Version":"2026-07-25","Statement":[{"Sid":"v1","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`,
		CreatedBy: grnOf("user", "ada").String(),
	})
	require.NoError(t, err)
	policyGRN := created.GetPolicy().GetGrn()

	_, err = srv.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	statements, err := cache.Statements(ctx, orgGRN, nil)
	require.NoError(t, err)
	require.Len(t, statements, 1)

	archived, err := srv.ArchivePolicy(ctx, &authzv1.ArchivePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	assert.Equal(t, authzv1.PolicyStatus_POLICY_STATUS_ARCHIVED, archived.GetPolicy().GetStatus())

	statements, err = cache.Statements(ctx, orgGRN, nil)
	require.NoError(t, err)
	assert.Empty(t, statements)

	// The policy and its versions survive the transition.
	versions, err := srv.ListPolicyVersions(ctx, &authzv1.ListPolicyVersionsRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	assert.Len(t, versions.GetVersions(), 1)
}

func TestPolicyVersions_AppendOnly(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)

	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", wire.EffectAllow), grnOf("user", "ada"))
	require.NoError(t, err)

	// The trigger raises rather than violating a constraint, so there is
	// no constraint name to read: what names this mechanism is the
	// SQLSTATE plpgsql raises under and the message
	// migrations/000002_policies.up.sql gives it. A refusal by anything
	// else — a missing row, a type error — carries neither.
	_, err = p.Exec(ctx, `UPDATE policy_versions SET document = '{}'::jsonb WHERE policy_grn = $1`, g.String())
	var rewritten *pgconn.PgError
	require.ErrorAs(t, err, &rewritten, "a stored version must not be rewritten")
	assert.Equal(t, "P0001", rewritten.Code, "raised by the append-only trigger")
	assert.Equal(t, "policy_versions is append-only", rewritten.Message)
	_, err = p.Exec(ctx, `DELETE FROM policy_versions WHERE policy_grn = $1`, g.String())
	var deleted *pgconn.PgError
	require.ErrorAs(t, err, &deleted, "a stored version must not be deleted on its own")
	assert.Equal(t, "P0001", deleted.Code, "raised by the append-only trigger")
	assert.Equal(t, "policy_versions is append-only", deleted.Message)

	// Versions do disappear with the policy that owns them.
	require.NoError(t, orgpg.New(p).DeleteOrg(ctx, orgGRN))
	var count int
	require.NoError(t, p.QueryRow(ctx, `SELECT count(*) FROM policy_versions`).Scan(&count))
	assert.Zero(t, count)
}

// TestPolicyVersions_StoreAndReturnTheSubmittedDocument writes a
// document carrying the reserved Condition (ADR-0002) and reads it back
// two ways: out of the append-only column, which is the only copy, and
// out of the lifecycle RPC an administrator asks. Both answer the bytes
// that were submitted. A second version carrying none is appended so a
// Condition surfacing where none was written fails here as well.
func TestPolicyVersions_StoreAndReturnTheSubmittedDocument(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	srv := authz.NewPolicyGRPCServer(s, authz.NewPolicyCache(s))
	orgGRN := newPolicyOrg(t, p)

	const (
		conditioned   = `{"Version":"2026-07-25","Statement":[{"Sid":"OfficeOnly","Effect":"Allow","Action":["org:update"],"Resource":["grn:acme:*"],"Condition":{"IpAddress":{"garam:SourceIp":"203.0.113.0/24"}}}]}`
		unconditioned = `{"Version":"2026-07-25","Statement":[{"Sid":"Anywhere","Effect":"Allow","Action":["org:update"],"Resource":["grn:acme:*"]}]}`
	)

	created, err := srv.CreatePolicy(ctx, &authzv1.CreatePolicyRequest{
		OrgGrn:    orgGRN.String(),
		Name:      "office-only",
		Document:  conditioned,
		CreatedBy: grnOf("user", "ada").String(),
	})
	require.NoError(t, err)
	policyGRN := created.GetPolicy().GetGrn()

	_, err = srv.AddPolicyVersion(ctx, &authzv1.AddPolicyVersionRequest{
		PolicyGrn: policyGRN,
		Document:  unconditioned,
		CreatedBy: grnOf("user", "ada").String(),
	})
	require.NoError(t, err)

	const storedDocument = `SELECT document::text FROM policy_versions WHERE policy_grn = $1 AND version = $2`
	var first, second string
	require.NoError(t, p.QueryRow(ctx, storedDocument, policyGRN, 1).Scan(&first))
	require.NoError(t, p.QueryRow(ctx, storedDocument, policyGRN, 2).Scan(&second))
	assert.JSONEq(t, conditioned, first, "the stored column is the document that was submitted")
	assert.JSONEq(t, unconditioned, second, "the stored column gained no field nobody wrote")

	versions, err := srv.ListPolicyVersions(ctx, &authzv1.ListPolicyVersionsRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	require.Len(t, versions.GetVersions(), 2)
	assert.JSONEq(t, conditioned, versions.GetVersions()[0].GetDocument(), "the version read back is the version written")
	assert.JSONEq(t, unconditioned, versions.GetVersions()[1].GetDocument(), "the version read back gained no field nobody wrote")
}

func TestPolicyCache_InvalidatedByNotify(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)

	cache := authz.NewPolicyCache(s)
	go func() { _ = authz.ListenPolicyChanges(ctx, p, cache) }()

	before, err := cache.Statements(ctx, orgGRN, nil)
	require.NoError(t, err)
	require.Empty(t, before)

	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", wire.EffectAllow), grnOf("user", "ada"))
	require.NoError(t, err)
	_, err = s.SetPolicyStatus(ctx, g, wire.PolicyStatusActive)
	require.NoError(t, err)

	// The listener registers its LISTEN concurrently with the writes
	// above, so the announcement is repeated until it has one to
	// receive. TestPolicyWrite_Notifies covers the write's own
	// announcement.
	assert.Eventually(t, func() bool {
		_, err := p.Exec(ctx, `SELECT pg_notify($1, $2)`, authz.PolicyChannel, orgGRN.String())
		require.NoError(t, err)
		got, err := cache.Statements(ctx, orgGRN, nil)
		return err == nil && len(got) == 1
	}, 5*time.Second, 20*time.Millisecond, "the notify listener should drop the org's cached statements")
}

func TestPolicyWrite_Notifies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)

	conn, err := p.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(ctx, "LISTEN "+authz.PolicyChannel)
	require.NoError(t, err)

	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", wire.EffectAllow), grnOf("user", "ada"))
	require.NoError(t, err)

	n, err := conn.Conn().WaitForNotification(ctx)
	require.NoError(t, err)
	assert.Equal(t, authz.PolicyChannel, n.Channel)
	assert.Equal(t, orgGRN.String(), n.Payload)

	// An attachment write changes what the policy applies to, so it
	// announces the same org.
	_, err = s.AttachPolicy(ctx, g, grnOf("user", "ada"))
	require.NoError(t, err)
	n, err = conn.Conn().WaitForNotification(ctx)
	require.NoError(t, err)
	assert.Equal(t, orgGRN.String(), n.Payload)
}

// TestPolicyAttachments_RoundTrip drives attachment over the real
// store: an attached policy reaches the subject it names and the
// group it names, an archived one reaches nobody, and detaching an
// active policy's last attachment is refused rather than widening the
// policy back to an org-wide grant.
func TestPolicyAttachments_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(s)
	srv := authz.NewPolicyGRPCServer(s, cache)
	orgGRN := newPolicyOrg(t, p)
	ada, bob, eng := grnOf("user", "ada"), grnOf("user", "bob"), grnOf("group", "eng")

	created, err := srv.CreatePolicy(ctx, &authzv1.CreatePolicyRequest{
		OrgGrn:    orgGRN.String(),
		Name:      "read-only",
		Document:  `{"Version":"2026-07-25","Statement":[{"Sid":"v1","Effect":"Allow","Action":["org:read"],"Resource":["grn:acme:*"]}]}`,
		CreatedBy: ada.String(),
	})
	require.NoError(t, err)
	policyGRN := created.GetPolicy().GetGrn()
	_, err = srv.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)

	// Unattached, the policy applies to every subject of the org.
	statements, err := cache.Statements(ctx, orgGRN, []grn.GRN{bob})
	require.NoError(t, err)
	require.Len(t, statements, 1)

	_, err = srv.AttachPolicy(ctx, &authzv1.AttachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: ada.String()})
	require.NoError(t, err)
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{ada})
	require.NoError(t, err)
	require.Len(t, statements, 1)
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{bob})
	require.NoError(t, err)
	assert.Empty(t, statements, "an attached policy stops applying org-wide")

	// A group attachment reaches the members the caller reports.
	_, err = srv.AttachPolicy(ctx, &authzv1.AttachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: eng.String()})
	require.NoError(t, err)
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{bob, eng})
	require.NoError(t, err)
	require.Len(t, statements, 1)

	attachments, err := srv.ListPolicyAttachments(ctx, &authzv1.ListPolicyAttachmentsRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	assert.Len(t, attachments.GetAttachments(), 2)

	// An archived policy is not evaluated even where it is attached.
	_, err = srv.ArchivePolicy(ctx, &authzv1.ArchivePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{ada, eng})
	require.NoError(t, err)
	assert.Empty(t, statements)
	_, err = srv.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)

	// Detaching down to one attachment narrows as before, but the last
	// one is refused: removing it would widen the active policy
	// org-wide, so the policy keeps applying to eng and to eng only.
	_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: ada.String()})
	require.NoError(t, err)
	_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: eng.String()})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{bob})
	require.NoError(t, err)
	assert.Empty(t, statements, "a refused detach leaves the policy where it was")
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{eng})
	require.NoError(t, err)
	require.Len(t, statements, 1)

	// Archiving is what retires the policy, and what nothing evaluates
	// detaches freely.
	_, err = srv.ArchivePolicy(ctx, &authzv1.ArchivePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: eng.String()})
	require.NoError(t, err)

	_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: ada.String()})
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// TestPolicyAttachment_StaysInsideItsOrg covers the schema's
// org-consistency rule: an org-owned subject from another org cannot
// carry the policy out of its org.
func TestPolicyAttachment_StaysInsideItsOrg(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	orgGRN := newPolicyOrg(t, p)

	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "read-only"), policyDoc("v1", wire.EffectAllow), grnOf("user", "ada"))
	require.NoError(t, err)

	_, err = s.AttachPolicy(ctx, g, grn.New("other", "default", "group", "eng"))
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))

	// A root-owned identity exists above every org and is admitted.
	_, err = s.AttachPolicy(ctx, g, grn.New(grn.RootOwner, "default", "user", "ada"))
	require.NoError(t, err)

	_, err = s.AttachPolicy(ctx, grn.New("acme", "default", authz.ServiceTypePolicy, "missing"), grnOf("user", "ada"))
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
}

// TestPolicyAttachment_DenyOverridesAcrossSources covers the union the
// engine evaluates: a policy attached to the subject's group denies
// what the caller's role statements allow.
func TestPolicyAttachment_DenyOverridesAcrossSources(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(s)
	orgGRN := newPolicyOrg(t, p)
	ada, eng := grnOf("user", "ada"), grnOf("group", "eng")

	denyRead := authz.PolicyDocument{
		Version: "2026-07-25",
		Statement: []authz.Statement{{
			Sid:      "deny-read",
			Effect:   wire.EffectDeny,
			Action:   []string{"project:read"},
			Resource: []string{"grn:acme:*"},
		}},
	}
	g, err := authz.NewPolicyGRN(orgGRN)
	require.NoError(t, err)
	_, err = s.CreatePolicy(ctx, authz.NewPolicy(g, orgGRN, "no-read"), denyRead, ada)
	require.NoError(t, err)
	_, err = s.SetPolicyStatus(ctx, g, wire.PolicyStatusActive)
	require.NoError(t, err)
	_, err = s.AttachPolicy(ctx, g, eng)
	require.NoError(t, err)

	req := &authzv1.CheckRequest{
		SubjectGrn: ada.String(),
		ObjectGrn:  grnOf("project", "web").String(),
		Action:     "project:read",
		Statements: []*authzv1.Statement{{
			Sid:      "org-role-owner",
			Effect:   authzv1.Effect_EFFECT_ALLOW,
			Action:   []string{"project:*"},
			Resource: []string{"grn:acme:*"},
		}},
		// ada is a member of acme: the api server populates subject_org
		// the same way the engine now requires (ADR-0038).
		SubjectOrg: grnOf("org", "acme").String(),
	}
	engine := authz.NewGRPCServer(cache)
	resp, err := engine.Check(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.GetAllowed(), "the attachment does not reach a subject outside the group")

	req.SubjectGroups = []string{eng.String()}
	resp, err = engine.Check(ctx, req)
	require.NoError(t, err)
	assert.False(t, resp.GetAllowed(), "a group-attached deny overrides the caller's allow")
}

// TestPolicyAttachment_ADenyDoesNotWidenOnDetach covers the transition
// ADR-0008 left symmetric: a Deny written for one subject, detached
// from it, must not reach the subjects it was never attached to. The
// detach of the last attachment is refused, so the Deny goes on
// deciding for its subject — the passing control beside the refusal —
// and bob, whom a widening would have caught, is never reached.
func TestPolicyAttachment_ADenyDoesNotWidenOnDetach(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := authz.NewPostgresStore(p)
	cache := authz.NewPolicyCache(s)
	srv := authz.NewPolicyGRPCServer(s, cache)
	orgGRN := newPolicyOrg(t, p)
	ada, bob := grnOf("user", "ada"), grnOf("user", "bob")

	created, err := srv.CreatePolicy(ctx, &authzv1.CreatePolicyRequest{
		OrgGrn:    orgGRN.String(),
		Name:      "no-read",
		Document:  `{"Version":"2026-07-25","Statement":[{"Sid":"deny-read","Effect":"Deny","Action":["project:read"],"Resource":["grn:acme:*"]}]}`,
		CreatedBy: ada.String(),
	})
	require.NoError(t, err)
	policyGRN := created.GetPolicy().GetGrn()
	_, err = srv.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN})
	require.NoError(t, err)
	_, err = srv.AttachPolicy(ctx, &authzv1.AttachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: ada.String()})
	require.NoError(t, err)

	_, err = srv.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{PolicyGrn: policyGRN, SubjectGrn: ada.String()})
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))

	statements, err := cache.Statements(ctx, orgGRN, []grn.GRN{bob})
	require.NoError(t, err)
	assert.Empty(t, statements, "a detached Deny reaches no subject it was never attached to")
	statements, err = cache.Statements(ctx, orgGRN, []grn.GRN{ada})
	require.NoError(t, err)
	require.Len(t, statements, 1, "the refused detach leaves the Deny deciding for its subject")
}
