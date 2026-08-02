package authz

import (
	"context"
	"encoding/json"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// PolicyGRPCServer serves the policy lifecycle over gRPC (ADR-0002).
// Every write that changes what an active policy evaluates drops the
// org's cached statements before it returns.
type PolicyGRPCServer struct {
	authzv1.UnimplementedPolicyServiceServer
	store Store
	cache *PolicyCache
}

// Compile-time check that PolicyGRPCServer satisfies the generated service.
var _ authzv1.PolicyServiceServer = (*PolicyGRPCServer)(nil)

// NewPolicyGRPCServer constructs the policy lifecycle service over
// store, invalidating cache on write.
func NewPolicyGRPCServer(store Store, cache *PolicyCache) *PolicyGRPCServer {
	return &PolicyGRPCServer{store: store, cache: cache}
}

// CreatePolicy stores a draft policy with its first version. A draft
// does not contribute to checks until it is activated.
func (s *PolicyGRPCServer) CreatePolicy(ctx context.Context, req *authzv1.CreatePolicyRequest) (*authzv1.CreatePolicyResponse, error) {
	orgGRN, err := grn.Parse(req.GetOrgGrn())
	if err != nil {
		return nil, grpcError(errorx.Wrap(err, "org_grn"))
	}
	createdBy, err := grn.Parse(req.GetCreatedBy())
	if err != nil {
		return nil, grpcError(errorx.Wrap(err, "created_by"))
	}
	if req.GetName() == "" {
		return nil, grpcError(errorx.New(errorx.KindInvalidArgument, "name is required"))
	}
	doc, err := ParsePolicyDocument([]byte(req.GetDocument()))
	if err != nil {
		return nil, grpcError(err)
	}
	g, err := NewPolicyGRN(orgGRN)
	if err != nil {
		return nil, grpcError(err)
	}
	p, err := s.store.CreatePolicy(ctx, NewPolicy(g, orgGRN, req.GetName()), doc, createdBy)
	if err != nil {
		return nil, grpcError(err)
	}
	s.cache.Invalidate(orgGRN)
	return &authzv1.CreatePolicyResponse{Policy: policyToProto(p)}, nil
}

// AddPolicyVersion appends a version. The policy keeps evaluating its
// default version, so no cache entry goes stale.
func (s *PolicyGRPCServer) AddPolicyVersion(ctx context.Context, req *authzv1.AddPolicyVersionRequest) (*authzv1.AddPolicyVersionResponse, error) {
	g, err := grn.Parse(req.GetPolicyGrn())
	if err != nil {
		return nil, grpcError(errorx.Wrap(err, "policy_grn"))
	}
	createdBy, err := grn.Parse(req.GetCreatedBy())
	if err != nil {
		return nil, grpcError(errorx.Wrap(err, "created_by"))
	}
	doc, err := ParsePolicyDocument([]byte(req.GetDocument()))
	if err != nil {
		return nil, grpcError(err)
	}
	v, err := s.store.AddPolicyVersion(ctx, g, doc, createdBy)
	if err != nil {
		return nil, grpcError(err)
	}
	version, err := policyVersionToProto(v)
	if err != nil {
		return nil, grpcError(err)
	}
	return &authzv1.AddPolicyVersionResponse{Version: version}, nil
}

// ListPolicies lists an org's policies.
func (s *PolicyGRPCServer) ListPolicies(ctx context.Context, req *authzv1.ListPoliciesRequest) (*authzv1.ListPoliciesResponse, error) {
	orgGRN, err := grn.Parse(req.GetOrgGrn())
	if err != nil {
		return nil, grpcError(errorx.Wrap(err, "org_grn"))
	}
	policies, err := s.store.ListPolicies(ctx, orgGRN)
	if err != nil {
		return nil, grpcError(err)
	}
	out := make([]*authzv1.Policy, 0, len(policies))
	for _, p := range policies {
		out = append(out, policyToProto(p))
	}
	return &authzv1.ListPoliciesResponse{Policies: out}, nil
}

// ListPolicyVersions lists a policy's stored versions, oldest first.
func (s *PolicyGRPCServer) ListPolicyVersions(ctx context.Context, req *authzv1.ListPolicyVersionsRequest) (*authzv1.ListPolicyVersionsResponse, error) {
	g, err := grn.Parse(req.GetPolicyGrn())
	if err != nil {
		return nil, grpcError(errorx.Wrap(err, "policy_grn"))
	}
	versions, err := s.store.ListPolicyVersions(ctx, g)
	if err != nil {
		return nil, grpcError(err)
	}
	out := make([]*authzv1.PolicyVersion, 0, len(versions))
	for _, v := range versions {
		p, err := policyVersionToProto(v)
		if err != nil {
			return nil, grpcError(err)
		}
		out = append(out, p)
	}
	return &authzv1.ListPolicyVersionsResponse{Versions: out}, nil
}

// ActivatePolicy makes the policy count in evaluation.
func (s *PolicyGRPCServer) ActivatePolicy(ctx context.Context, req *authzv1.ActivatePolicyRequest) (*authzv1.ActivatePolicyResponse, error) {
	g, err := grn.Parse(req.GetPolicyGrn())
	if err != nil {
		return nil, grpcError(errorx.Wrap(err, "policy_grn"))
	}
	p, err := s.store.SetPolicyStatus(ctx, g, PolicyStatusActive)
	if err != nil {
		return nil, grpcError(err)
	}
	s.cache.Invalidate(p.Organization())
	return &authzv1.ActivatePolicyResponse{Policy: policyToProto(p)}, nil
}

// ArchivePolicy retires the policy: it and its versions stay stored,
// but it stops contributing statements to checks.
func (s *PolicyGRPCServer) ArchivePolicy(ctx context.Context, req *authzv1.ArchivePolicyRequest) (*authzv1.ArchivePolicyResponse, error) {
	g, err := grn.Parse(req.GetPolicyGrn())
	if err != nil {
		return nil, grpcError(errorx.Wrap(err, "policy_grn"))
	}
	p, err := s.store.SetPolicyStatus(ctx, g, PolicyStatusArchived)
	if err != nil {
		return nil, grpcError(err)
	}
	s.cache.Invalidate(p.Organization())
	return &authzv1.ArchivePolicyResponse{Policy: policyToProto(p)}, nil
}

// RollbackPolicy points the policy at an earlier stored version.
func (s *PolicyGRPCServer) RollbackPolicy(ctx context.Context, req *authzv1.RollbackPolicyRequest) (*authzv1.RollbackPolicyResponse, error) {
	g, err := grn.Parse(req.GetPolicyGrn())
	if err != nil {
		return nil, grpcError(errorx.Wrap(err, "policy_grn"))
	}
	if req.GetVersion() < firstPolicyVersion {
		return nil, grpcError(errorx.New(errorx.KindInvalidArgument, "version must be positive"))
	}
	p, err := s.store.SetPolicyDefaultVersion(ctx, g, int(req.GetVersion()))
	if err != nil {
		return nil, grpcError(err)
	}
	s.cache.Invalidate(p.Organization())
	return &authzv1.RollbackPolicyResponse{Policy: policyToProto(p)}, nil
}

// AttachPolicy binds the policy to a subject, narrowing it from an
// org-wide grant to that subject and the members of that group
// (ADR-0008).
func (s *PolicyGRPCServer) AttachPolicy(ctx context.Context, req *authzv1.AttachPolicyRequest) (*authzv1.AttachPolicyResponse, error) {
	g, subject, err := parseAttachment(req.GetPolicyGrn(), req.GetSubjectGrn())
	if err != nil {
		return nil, grpcError(err)
	}
	p, err := s.store.GetPolicy(ctx, g)
	if err != nil {
		return nil, grpcError(err)
	}
	a, err := s.store.AttachPolicy(ctx, g, subject)
	if err != nil {
		return nil, grpcError(err)
	}
	s.cache.Invalidate(p.Organization())
	return &authzv1.AttachPolicyResponse{Attachment: attachmentToProto(a)}, nil
}

// DetachPolicy removes one attachment. A policy left with none applies
// org-wide again.
func (s *PolicyGRPCServer) DetachPolicy(ctx context.Context, req *authzv1.DetachPolicyRequest) (*authzv1.DetachPolicyResponse, error) {
	g, subject, err := parseAttachment(req.GetPolicyGrn(), req.GetSubjectGrn())
	if err != nil {
		return nil, grpcError(err)
	}
	p, err := s.store.GetPolicy(ctx, g)
	if err != nil {
		return nil, grpcError(err)
	}
	if err := s.store.DetachPolicy(ctx, g, subject); err != nil {
		return nil, grpcError(err)
	}
	s.cache.Invalidate(p.Organization())
	return &authzv1.DetachPolicyResponse{}, nil
}

// ListPolicyAttachments lists a policy's attachments.
func (s *PolicyGRPCServer) ListPolicyAttachments(ctx context.Context, req *authzv1.ListPolicyAttachmentsRequest) (*authzv1.ListPolicyAttachmentsResponse, error) {
	g, err := grn.Parse(req.GetPolicyGrn())
	if err != nil {
		return nil, grpcError(errorx.Wrap(err, "policy_grn"))
	}
	attachments, err := s.store.ListPolicyAttachments(ctx, g)
	if err != nil {
		return nil, grpcError(err)
	}
	out := make([]*authzv1.PolicyAttachment, 0, len(attachments))
	for _, a := range attachments {
		out = append(out, attachmentToProto(a))
	}
	return &authzv1.ListPolicyAttachmentsResponse{Attachments: out}, nil
}

// parseAttachment parses the pair every attachment RPC addresses.
func parseAttachment(policyGRN, subjectGRN string) (grn.GRN, grn.GRN, error) {
	g, err := grn.Parse(policyGRN)
	if err != nil {
		return grn.GRN{}, grn.GRN{}, errorx.Wrap(err, "policy_grn")
	}
	subject, err := grn.Parse(subjectGRN)
	if err != nil {
		return grn.GRN{}, grn.GRN{}, errorx.Wrap(err, "subject_grn")
	}
	return g, subject, nil
}

func policyToProto(p *Policy) *authzv1.Policy {
	return &authzv1.Policy{
		Grn:            p.GRN().String(),
		OrgGrn:         p.Organization().String(),
		Name:           p.Name(),
		Status:         policyStatusToProto(p.Status()),
		DefaultVersion: int32(p.DefaultVersion()),
		CreatedAt:      timestamppb.New(p.CreatedAt()),
		UpdatedAt:      timestamppb.New(p.UpdatedAt()),
	}
}

func policyVersionToProto(v *PolicyVersion) (*authzv1.PolicyVersion, error) {
	raw, err := json.Marshal(v.Document())
	if err != nil {
		return nil, errorx.Wrap(err, "encode policy document")
	}
	return &authzv1.PolicyVersion{
		PolicyGrn: v.Policy().String(),
		Version:   int32(v.Version()),
		Document:  string(raw),
		CreatedBy: v.CreatedBy().String(),
		CreatedAt: timestamppb.New(v.CreatedAt()),
	}, nil
}

func attachmentToProto(a *PolicyAttachment) *authzv1.PolicyAttachment {
	return &authzv1.PolicyAttachment{
		PolicyGrn:  a.Policy().String(),
		SubjectGrn: a.Subject().String(),
		CreatedAt:  timestamppb.New(a.CreatedAt()),
	}
}

func policyStatusToProto(s PolicyStatus) authzv1.PolicyStatus {
	switch s {
	case PolicyStatusDraft:
		return authzv1.PolicyStatus_POLICY_STATUS_DRAFT
	case PolicyStatusActive:
		return authzv1.PolicyStatus_POLICY_STATUS_ACTIVE
	case PolicyStatusArchived:
		return authzv1.PolicyStatus_POLICY_STATUS_ARCHIVED
	}
	return authzv1.PolicyStatus_POLICY_STATUS_UNSPECIFIED
}
