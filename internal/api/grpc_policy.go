package api

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// GRPCPolicyManager is the PolicyManager seam over gRPC (ADR-0003
// stage 2). Every method is a pass-through to PolicyService; the
// engine validates documents and owns the lifecycle rules.
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

// CreatePolicy stores a draft policy with document as its first
// version.
func (m *GRPCPolicyManager) CreatePolicy(ctx context.Context, orgGRN grn.GRN, name string, document json.RawMessage, createdBy grn.GRN) (*Policy, error) {
	resp, err := m.client.CreatePolicy(ctx, &authzv1.CreatePolicyRequest{
		OrgGrn:    orgGRN.String(),
		Name:      name,
		Document:  string(document),
		CreatedBy: createdBy.String(),
	})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	return policyFromProto(resp.GetPolicy())
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

// AddPolicyVersion appends a version, leaving the default version
// pointer where it is.
func (m *GRPCPolicyManager) AddPolicyVersion(ctx context.Context, policyGRN grn.GRN, document json.RawMessage, createdBy grn.GRN) (*PolicyVersion, error) {
	resp, err := m.client.AddPolicyVersion(ctx, &authzv1.AddPolicyVersionRequest{
		PolicyGrn: policyGRN.String(),
		Document:  string(document),
		CreatedBy: createdBy.String(),
	})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	return policyVersionFromProto(resp.GetVersion())
}

// ListPolicyVersions lists the policy's versions, oldest first.
func (m *GRPCPolicyManager) ListPolicyVersions(ctx context.Context, policyGRN grn.GRN) ([]*PolicyVersion, error) {
	resp, err := m.client.ListPolicyVersions(ctx, &authzv1.ListPolicyVersionsRequest{PolicyGrn: policyGRN.String()})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	out := make([]*PolicyVersion, 0, len(resp.GetVersions()))
	for _, v := range resp.GetVersions() {
		version, err := policyVersionFromProto(v)
		if err != nil {
			return nil, err
		}
		out = append(out, version)
	}
	return out, nil
}

// ActivatePolicy makes the policy count in evaluation.
func (m *GRPCPolicyManager) ActivatePolicy(ctx context.Context, policyGRN grn.GRN) (*Policy, error) {
	resp, err := m.client.ActivatePolicy(ctx, &authzv1.ActivatePolicyRequest{PolicyGrn: policyGRN.String()})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	return policyFromProto(resp.GetPolicy())
}

// ArchivePolicy retires the policy without deleting it.
func (m *GRPCPolicyManager) ArchivePolicy(ctx context.Context, policyGRN grn.GRN) (*Policy, error) {
	resp, err := m.client.ArchivePolicy(ctx, &authzv1.ArchivePolicyRequest{PolicyGrn: policyGRN.String()})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	return policyFromProto(resp.GetPolicy())
}

// RollbackPolicy points the policy at any stored version, older or
// newer than its current default. The version is the width the request
// carried it in, so nothing here narrows it.
func (m *GRPCPolicyManager) RollbackPolicy(ctx context.Context, policyGRN grn.GRN, version int32) (*Policy, error) {
	resp, err := m.client.RollbackPolicy(ctx, &authzv1.RollbackPolicyRequest{
		PolicyGrn: policyGRN.String(),
		Version:   version,
	})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	return policyFromProto(resp.GetPolicy())
}

// AttachPolicy binds the policy to a subject.
func (m *GRPCPolicyManager) AttachPolicy(ctx context.Context, policyGRN, subjectGRN grn.GRN) (*PolicyAttachment, error) {
	resp, err := m.client.AttachPolicy(ctx, &authzv1.AttachPolicyRequest{
		PolicyGrn:  policyGRN.String(),
		SubjectGrn: subjectGRN.String(),
	})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	return attachmentFromProto(resp.GetAttachment())
}

// DetachPolicy removes one attachment.
func (m *GRPCPolicyManager) DetachPolicy(ctx context.Context, policyGRN, subjectGRN grn.GRN) error {
	_, err := m.client.DetachPolicy(ctx, &authzv1.DetachPolicyRequest{
		PolicyGrn:  policyGRN.String(),
		SubjectGrn: subjectGRN.String(),
	})
	if err != nil {
		return wire.ErrorOf(err)
	}
	return nil
}

// ListPolicyAttachments lists the policy's attachments in ascending
// subject GRN order.
func (m *GRPCPolicyManager) ListPolicyAttachments(ctx context.Context, policyGRN grn.GRN) ([]*PolicyAttachment, error) {
	resp, err := m.client.ListPolicyAttachments(ctx, &authzv1.ListPolicyAttachmentsRequest{PolicyGrn: policyGRN.String()})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	out := make([]*PolicyAttachment, 0, len(resp.GetAttachments()))
	for _, a := range resp.GetAttachments() {
		attachment, err := attachmentFromProto(a)
		if err != nil {
			return nil, err
		}
		out = append(out, attachment)
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

func policyVersionFromProto(v *authzv1.PolicyVersion) (*PolicyVersion, error) {
	g, err := grn.Parse(v.GetPolicyGrn())
	if err != nil {
		return nil, errorx.Wrap(err, "policy grn", errorx.KindInternal)
	}
	createdBy, err := grn.Parse(v.GetCreatedBy())
	if err != nil {
		return nil, errorx.Wrap(err, "policy version created_by", errorx.KindInternal)
	}
	return &PolicyVersion{
		PolicyGRN: g,
		Version:   int(v.GetVersion()),
		Document:  json.RawMessage(v.GetDocument()),
		CreatedBy: createdBy,
		CreatedAt: v.GetCreatedAt().AsTime(),
	}, nil
}

func attachmentFromProto(a *authzv1.PolicyAttachment) (*PolicyAttachment, error) {
	g, err := grn.Parse(a.GetPolicyGrn())
	if err != nil {
		return nil, errorx.Wrap(err, "policy grn", errorx.KindInternal)
	}
	subject, err := grn.Parse(a.GetSubjectGrn())
	if err != nil {
		return nil, errorx.Wrap(err, "attachment subject grn", errorx.KindInternal)
	}
	return &PolicyAttachment{PolicyGRN: g, SubjectGRN: subject, CreatedAt: a.GetCreatedAt().AsTime()}, nil
}
