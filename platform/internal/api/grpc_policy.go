package api

import (
	"context"

	"google.golang.org/grpc"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/wire"
	authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"
)

// GRPCPolicyManager is the PolicyManager seam over gRPC (ADR-0003
// stage 2). Every method is a pass-through to PolicyService.
type GRPCPolicyManager struct {
	client authzv1.PolicyServiceClient
}

// Compile-time check that GRPCPolicyManager satisfies PolicyManager.
var _ PolicyManager = (*GRPCPolicyManager)(nil)

// NewGRPCPolicyManager constructs a PolicyManager that calls the
// authz engine over conn.
func NewGRPCPolicyManager(conn grpc.ClientConnInterface) *GRPCPolicyManager {
	return &GRPCPolicyManager{client: authzv1.NewPolicyServiceClient(conn)}
}

// ListPolicies lists the org's policies in ascending GRN order.
func (m *GRPCPolicyManager) ListPolicies(ctx context.Context, orgGRN grn.GRN) ([]*Policy, error) {
	resp, err := m.client.ListPolicies(ctx, &authzv1.ListPoliciesRequest{OrgGrn: orgGRN.String()})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	out := make([]*Policy, 0, len(resp.GetPolicies()))
	for _, p := range resp.GetPolicies() {
		policy, err := policyFromProto(p)
		if err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	return out, nil
}

// SearchPolicies lists the org's policies whose name or identifier
// holds n, in ascending GRN order.
func (m *GRPCPolicyManager) SearchPolicies(ctx context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]*Policy, error) {
	resp, err := m.client.SearchPolicies(ctx, &authzv1.SearchPoliciesRequest{OrgGrn: orgGRN.String(), Query: n.Text()})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	out := make([]*Policy, 0, len(resp.GetPolicies()))
	for _, p := range resp.GetPolicies() {
		policy, err := policyFromProto(p)
		if err != nil {
			return nil, err
		}
		out = append(out, policy)
	}
	return out, nil
}

func policyFromProto(p *authzv1.Policy) (*Policy, error) {
	g, err := grn.Parse(p.GetGrn())
	if err != nil {
		return nil, errorx.Wrap(err, "policy grn", errorx.KindInternal)
	}
	orgGRN, err := grn.Parse(p.GetOrgGrn())
	if err != nil {
		return nil, errorx.Wrap(err, "policy org grn", errorx.KindInternal)
	}
	return &Policy{
		GRN:            g,
		OrgGRN:         orgGRN,
		Name:           p.GetName(),
		Status:         wire.PolicyStatusFromProto(p.GetStatus()),
		DefaultVersion: int(p.GetDefaultVersion()),
		CreatedAt:      p.GetCreatedAt().AsTime(),
		UpdatedAt:      p.GetUpdatedAt().AsTime(),
	}, nil
}
