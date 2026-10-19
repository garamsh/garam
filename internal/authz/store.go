package authz

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
)

// Store persists policies and their append-only versions (ADR-0002).
// A stored version is never rewritten: rollback moves the policy's
// default-version pointer instead, activation changes only its status,
// and a version goes only with the policy that owns it.
type Store interface {
	// CreatePolicy stores a draft policy together with its first
	// version, atomically.
	CreatePolicy(ctx context.Context, p *Policy, doc PolicyDocument, createdBy grn.GRN) (*Policy, error)
	GetPolicy(ctx context.Context, g grn.GRN) (*Policy, error)
	ListPolicies(ctx context.Context, orgGRN grn.GRN) ([]*Policy, error)

	// AddPolicyVersion appends a version, leaving the default-version
	// pointer where it is.
	AddPolicyVersion(ctx context.Context, g grn.GRN, doc PolicyDocument, createdBy grn.GRN) (*PolicyVersion, error)
	ListPolicyVersions(ctx context.Context, g grn.GRN) ([]*PolicyVersion, error)

	// DeletePolicy removes the policy with its versions and
	// attachments and answers what it removed, or fails with
	// ErrPolicyNotFound.
	DeletePolicy(ctx context.Context, g grn.GRN) (*Policy, error)

	SetPolicyStatus(ctx context.Context, g grn.GRN, status wire.PolicyStatus) (*Policy, error)
	// SetPolicyDefaultVersion points the policy at an existing
	// version, or fails with ErrPolicyVersionNotFound.
	SetPolicyDefaultVersion(ctx context.Context, g grn.GRN, version int) (*Policy, error)

	// AttachPolicy binds the policy to a subject of the same org
	// (ADR-0008), narrowing it from an org-wide grant to that subject.
	AttachPolicy(ctx context.Context, policyGRN, subjectGRN grn.GRN) (*PolicyAttachment, error)
	// DetachPolicy removes one attachment, or fails with
	// ErrAttachmentNotFound. Removing an active policy's last
	// attachment fails with ErrLastAttachment instead: left with none,
	// the policy would apply org-wide.
	DetachPolicy(ctx context.Context, policyGRN, subjectGRN grn.GRN) error
	ListPolicyAttachments(ctx context.Context, policyGRN grn.GRN) ([]*PolicyAttachment, error)

	// ActivePolicies returns what every active policy of the org
	// currently evaluates, with the subjects it is attached to.
	ActivePolicies(ctx context.Context, orgGRN grn.GRN) ([]ActivePolicy, error)
}
