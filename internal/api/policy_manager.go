package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
)

// Policy is an org's versioned rule set as the engine reports it.
type Policy struct {
	GRN            grn.GRN
	OrgGRN         grn.GRN
	Name           string
	Status         wire.PolicyStatus
	DefaultVersion int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PolicyVersion is one immutable revision of a policy's document. The
// document travels as raw JSON: the engine owns the model and rejects
// what does not parse.
type PolicyVersion struct {
	PolicyGRN grn.GRN
	Version   int
	Document  json.RawMessage
	CreatedBy grn.GRN
	CreatedAt time.Time
}

// PolicyAttachment binds a policy to the subject it applies to
// (ADR-0008).
type PolicyAttachment struct {
	PolicyGRN  grn.GRN
	SubjectGRN grn.GRN
	CreatedAt  time.Time
}

// PolicyManager is the policy-lifecycle seam between the api server
// and the authz engine (ADR-0003). The gRPC client implements it; the
// api package never imports the engine.
type PolicyManager interface {
	CreatePolicy(ctx context.Context, orgGRN grn.GRN, name string, document json.RawMessage, createdBy grn.GRN) (*Policy, error)
	ListPolicies(ctx context.Context, orgGRN grn.GRN) ([]*Policy, error)
	AddPolicyVersion(ctx context.Context, policyGRN grn.GRN, document json.RawMessage, createdBy grn.GRN) (*PolicyVersion, error)
	ListPolicyVersions(ctx context.Context, policyGRN grn.GRN) ([]*PolicyVersion, error)
	ActivatePolicy(ctx context.Context, policyGRN grn.GRN) (*Policy, error)
	RollbackPolicy(ctx context.Context, policyGRN grn.GRN, version int32) (*Policy, error)
	AttachPolicy(ctx context.Context, policyGRN, subjectGRN grn.GRN) (*PolicyAttachment, error)
	DetachPolicy(ctx context.Context, policyGRN, subjectGRN grn.GRN) error
	DeletePolicy(ctx context.Context, policyGRN grn.GRN) error
	ListPolicyAttachments(ctx context.Context, policyGRN grn.GRN) ([]*PolicyAttachment, error)
}
