package authz

import (
	"context"

	"github.com/garamsh/gaon/internal/grn"
)

// Store persists policies and their append-only versions (ADR-0002).
// A stored version is never rewritten: activation and rollback move
// the policy's default-version pointer instead.
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

	SetPolicyStatus(ctx context.Context, g grn.GRN, status PolicyStatus) (*Policy, error)
	// SetPolicyDefaultVersion points the policy at an existing
	// version, or fails with ErrPolicyVersionNotFound.
	SetPolicyDefaultVersion(ctx context.Context, g grn.GRN, version int) (*Policy, error)

	// ActiveDocuments returns the document every active policy of the
	// org currently evaluates.
	ActiveDocuments(ctx context.Context, orgGRN grn.GRN) ([]PolicyDocument, error)
}
