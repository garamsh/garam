package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/policyrelay"
	relaygrpc "github.com/garamsh/garam/platform/internal/policyrelay/grpc"
)

// stubClient answers GetPolicy and ListPolicies with what it holds; any
// other call panics on the nil embedded client, which no test makes.
type stubClient struct {
	policyv1.PolicyServiceClient
	get  *policyv1.GetPolicyResponse
	list *policyv1.ListPoliciesResponse

	audit   *policyv1.ListAuditRecordsResponse
	auditIn *[]*policyv1.ListAuditRecordsRequest
}

func (s stubClient) ListAuditRecords(
	_ context.Context, in *policyv1.ListAuditRecordsRequest, _ ...grpc.CallOption,
) (*policyv1.ListAuditRecordsResponse, error) {
	*s.auditIn = append(*s.auditIn, in)
	return s.audit, nil
}

func (s stubClient) GetPolicy(
	context.Context, *policyv1.GetPolicyRequest, ...grpc.CallOption,
) (*policyv1.GetPolicyResponse, error) {
	return s.get, nil
}

func (s stubClient) ListPolicies(
	context.Context, *policyv1.ListPoliciesRequest, ...grpc.CallOption,
) (*policyv1.ListPoliciesResponse, error) {
	return s.list, nil
}

func summary(kind policyv1.PolicyKind, status policyv1.PolicyStatus) *policyv1.PolicySummary {
	return &policyv1.PolicySummary{
		Policy: "grn:acme/iam/policy/p-1", Name: "n", Kind: kind, Status: status, DefaultVersion: 2,
	}
}

func roleLinkBody(role policyv1.Role, scope policyv1.RoleScope) *policyv1.PolicyBody {
	return &policyv1.PolicyBody{Body: &policyv1.PolicyBody_RoleLink{RoleLink: &policyv1.RoleLink{
		Role: role, Scope: scope,
		Principal: &policyv1.EntityRef{Type: "Iam::Agent", Id: "grn:acme/iam/agent/bot"},
		Resource:  &policyv1.EntityRef{Type: "Iam::Org", Id: "grn:root/iam/org/acme"},
	}}}
}

var (
	reader = grn.GRN{Organization: "root", Service: "iam", ServiceType: "user", Identifier: "ada"}
	write  = policyrelay.Write{Writer: reader, Org: grn.Org("acme"), ID: "p-1"}
)

func TestList_ReadsEveryKnownKindAndStatus(t *testing.T) {
	client := relaygrpc.New(stubClient{list: &policyv1.ListPoliciesResponse{Policies: []*policyv1.PolicySummary{
		summary(policyv1.PolicyKind_POLICY_KIND_ROLE_LINK, policyv1.PolicyStatus_POLICY_STATUS_DRAFT),
		summary(policyv1.PolicyKind_POLICY_KIND_GUARDRAIL, policyv1.PolicyStatus_POLICY_STATUS_ACTIVE),
		summary(policyv1.PolicyKind_POLICY_KIND_PERMIT, policyv1.PolicyStatus_POLICY_STATUS_ACTIVE),
	}}})

	got, err := client.List(context.Background(), reader, grn.Org("acme"))

	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, policyrelay.KindRoleLink, got[0].Kind)
	assert.Equal(t, policyrelay.StatusDraft, got[0].Status)
	assert.Equal(t, policyrelay.KindGuardrail, got[1].Kind)
	assert.Equal(t, policyrelay.StatusActive, got[1].Status)
	assert.Equal(t, policyrelay.KindPermit, got[2].Kind)
	assert.Equal(t, "p-1", got[0].ID)
	assert.Equal(t, int32(2), got[0].DefaultVersion)
}

func TestList_AnUnspecifiedOrUnknownEnumIsTheServicesFault(t *testing.T) {
	for name, p := range map[string]*policyv1.PolicySummary{
		"no kind":           summary(policyv1.PolicyKind_POLICY_KIND_UNSPECIFIED, policyv1.PolicyStatus_POLICY_STATUS_DRAFT),
		"an unknown kind":   summary(policyv1.PolicyKind(99), policyv1.PolicyStatus_POLICY_STATUS_DRAFT),
		"no status":         summary(policyv1.PolicyKind_POLICY_KIND_PERMIT, policyv1.PolicyStatus_POLICY_STATUS_UNSPECIFIED),
		"an unknown status": summary(policyv1.PolicyKind_POLICY_KIND_PERMIT, policyv1.PolicyStatus(99)),
	} {
		t.Run(name, func(t *testing.T) {
			client := relaygrpc.New(stubClient{list: &policyv1.ListPoliciesResponse{
				Policies: []*policyv1.PolicySummary{p},
			}})

			_, err := client.List(context.Background(), reader, grn.Org("acme"))

			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v", err)
		})
	}
}

func TestGet_ReadsARoleLinkBack(t *testing.T) {
	client := relaygrpc.New(stubClient{get: &policyv1.GetPolicyResponse{
		Policy: summary(policyv1.PolicyKind_POLICY_KIND_ROLE_LINK, policyv1.PolicyStatus_POLICY_STATUS_ACTIVE),
		Versions: []*policyv1.PolicyVersion{{
			Version: 1, Body: roleLinkBody(policyv1.Role_ROLE_ADMIN, policyv1.RoleScope_ROLE_SCOPE_TEAM_OR_PROJECT),
		}},
	}})

	got, err := client.Get(context.Background(), write)

	require.NoError(t, err)
	require.Len(t, got.Versions, 1)
	link := got.Versions[0].Body.Link
	require.NotNil(t, link)
	assert.Equal(t, org.RoleAdmin, link.Role)
	assert.Equal(t, policyrelay.ScopeTeamOrProject, link.Scope)
}

func TestGet_AnUnspecifiedRoleOrScopeIsTheServicesFault(t *testing.T) {
	for name, body := range map[string]*policyv1.PolicyBody{
		"no role":  roleLinkBody(policyv1.Role_ROLE_UNSPECIFIED, policyv1.RoleScope_ROLE_SCOPE_ORGANIZATION),
		"no scope": roleLinkBody(policyv1.Role_ROLE_MEMBER, policyv1.RoleScope_ROLE_SCOPE_UNSPECIFIED),
	} {
		t.Run(name, func(t *testing.T) {
			client := relaygrpc.New(stubClient{get: &policyv1.GetPolicyResponse{
				Policy:   summary(policyv1.PolicyKind_POLICY_KIND_ROLE_LINK, policyv1.PolicyStatus_POLICY_STATUS_ACTIVE),
				Versions: []*policyv1.PolicyVersion{{Version: 1, Body: body}},
			}})

			_, err := client.Get(context.Background(), write)

			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v", err)
		})
	}
}

func TestListAudit_AsksAsTheReaderAndReadsARecordBack(t *testing.T) {
	var asked []*policyv1.ListAuditRecordsRequest
	at := time.Date(2026, 11, 17, 1, 2, 3, 0, time.UTC)
	client := relaygrpc.New(stubClient{auditIn: &asked, audit: &policyv1.ListAuditRecordsResponse{
		NextPageToken: "next",
		Records: []*policyv1.AuditRecord{{
			Id: 7, Kind: policyv1.AuditRecordKind_AUDIT_RECORD_KIND_DENIAL,
			FirstAt: timestamppb.New(at), LastAt: timestamppb.New(at.Add(time.Hour)),
			Count: 3, Service: "cowork", Subject: "grn:root/iam/user/bob",
			Action: "iam:permit.write", Resource: "grn:acme/iam/policy/p-1",
			Outcome: "deny", PolicyIds: []string{"p-1"},
		}, {
			Id: 8, Kind: policyv1.AuditRecordKind_AUDIT_RECORD_KIND_WRITE,
			FirstAt: timestamppb.New(at), LastAt: timestamppb.New(at),
			Count: 1, Outcome: "version-added", VersionFrom: 1, VersionTo: 2,
		}, {
			Id: 9, Kind: policyv1.AuditRecordKind_AUDIT_RECORD_KIND_DENIAL_OVERFLOW,
			FirstAt: timestamppb.New(at), LastAt: timestamppb.New(at), Count: 40,
			Day: timestamppb.New(at.Truncate(24 * time.Hour)),
		}},
	}})

	got, err := client.ListAudit(context.Background(), reader, grn.Org("acme"), 20, "tok")

	require.NoError(t, err)
	require.Len(t, asked, 1)
	assert.Equal(t, reader.String(), asked[0].GetWriter().GetId())
	assert.Equal(t, "grn:root/iam/org/acme", asked[0].GetOrg())
	assert.Equal(t, int32(20), asked[0].GetPageSize())
	assert.Equal(t, "tok", asked[0].GetPageToken())
	assert.Equal(t, "next", got.NextPageToken)
	require.Len(t, got.Records, 3)
	assert.Equal(t, policyrelay.AuditDenial, got.Records[0].Kind)
	assert.Equal(t, int64(3), got.Records[0].Count)
	assert.Equal(t, []string{"p-1"}, got.Records[0].PolicyIDs)
	assert.Equal(t, at.Add(time.Hour), got.Records[0].LastAt)
	assert.True(t, got.Records[0].Day.IsZero())
	assert.Equal(t, policyrelay.AuditWrite, got.Records[1].Kind)
	assert.Equal(t, int32(1), got.Records[1].VersionFrom)
	assert.Equal(t, int32(2), got.Records[1].VersionTo)
	assert.Equal(t, policyrelay.AuditDenialOverflow, got.Records[2].Kind)
	assert.False(t, got.Records[2].Day.IsZero())
}

func TestListAudit_AnUnspecifiedKindIsTheServicesFault(t *testing.T) {
	var asked []*policyv1.ListAuditRecordsRequest
	kinded := relaygrpc.New(stubClient{auditIn: &asked, audit: &policyv1.ListAuditRecordsResponse{
		Records: []*policyv1.AuditRecord{{Id: 1, Kind: policyv1.AuditRecordKind_AUDIT_RECORD_KIND_DENIAL}},
	}})
	// The control: the same record with a kind is read.
	page, err := kinded.ListAudit(context.Background(), reader, grn.Org("acme"), 0, "")
	require.NoError(t, err)
	require.Len(t, page.Records, 1)
	assert.Equal(t, policyrelay.AuditDenial, page.Records[0].Kind)

	client := relaygrpc.New(stubClient{auditIn: &asked, audit: &policyv1.ListAuditRecordsResponse{
		Records: []*policyv1.AuditRecord{{Id: 1}},
	}})

	_, err = client.ListAudit(context.Background(), reader, grn.Org("acme"), 0, "")

	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInternal), "got %v", err)
}
