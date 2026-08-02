package authz

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
)

// Policy sentinel errors.
var (
	ErrPolicyNotFound        = errorx.New(errorx.KindNotFound, "policy not found")
	ErrPolicyVersionNotFound = errorx.New(errorx.KindNotFound, "policy version not found")
	ErrPolicyAlreadyExists   = errorx.New(errorx.KindAlreadyExists, "policy already exists")
)

// ServiceTypePolicy is the GRN service type of a stored policy.
const ServiceTypePolicy = "policy"

// PolicyStatus is a policy's lifecycle state (ADR-0002).
type PolicyStatus string

const (
	PolicyStatusDraft    PolicyStatus = "draft"
	PolicyStatusActive   PolicyStatus = "active"
	PolicyStatusArchived PolicyStatus = "archived"
)

// ParsePolicyStatus parses a PolicyStatus from its stored form.
func ParsePolicyStatus(s string) (PolicyStatus, error) {
	switch p := PolicyStatus(s); p {
	case PolicyStatusDraft, PolicyStatusActive, PolicyStatusArchived:
		return p, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "authz: unknown policy status")
}

// Policy is an org-managed, versioned rule set. Only an active policy
// contributes statements to a check, and only at its default version.
type Policy struct {
	grn            grn.GRN
	orgGRN         grn.GRN
	name           string
	status         PolicyStatus
	defaultVersion int
	createdAt      time.Time
	updatedAt      time.Time
}

// NewPolicy constructs a draft Policy owned by orgGRN, pointing at the
// first version it will be stored with.
func NewPolicy(g, orgGRN grn.GRN, name string) *Policy {
	now := time.Now().UTC()
	return &Policy{
		grn:            g,
		orgGRN:         orgGRN,
		name:           name,
		status:         PolicyStatusDraft,
		defaultVersion: firstPolicyVersion,
		createdAt:      now,
		updatedAt:      now,
	}
}

func (p *Policy) GRN() grn.GRN          { return p.grn }
func (p *Policy) Organization() grn.GRN { return p.orgGRN }
func (p *Policy) Name() string          { return p.name }
func (p *Policy) Status() PolicyStatus  { return p.status }
func (p *Policy) DefaultVersion() int   { return p.defaultVersion }
func (p *Policy) CreatedAt() time.Time  { return p.createdAt }
func (p *Policy) UpdatedAt() time.Time  { return p.updatedAt }

// PolicyVersion is one immutable revision of a policy's document.
type PolicyVersion struct {
	policyGRN grn.GRN
	version   int
	document  PolicyDocument
	createdBy grn.GRN
	createdAt time.Time
}

func (v *PolicyVersion) Policy() grn.GRN          { return v.policyGRN }
func (v *PolicyVersion) Version() int             { return v.version }
func (v *PolicyVersion) Document() PolicyDocument { return v.document }
func (v *PolicyVersion) CreatedBy() grn.GRN       { return v.createdBy }
func (v *PolicyVersion) CreatedAt() time.Time     { return v.createdAt }

// firstPolicyVersion is the version a policy is created with; versions
// are append-only and numbered from it.
const firstPolicyVersion = 1

// NewPolicyGRN mints the GRN of a policy owned by orgGRN. Policies
// belong to the org, not to a project (ADR-0001).
func NewPolicyGRN(orgGRN grn.GRN) (grn.GRN, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return grn.GRN{}, errorx.Wrap(err, "policy id")
	}
	return grn.New(orgGRN.Identifier, grn.DefaultProject, ServiceTypePolicy, hex.EncodeToString(b)), nil
}

// ParsePolicyDocument parses and validates a policy document (ADR-0002)
// so stored documents are known-good before they reach evaluation.
func ParsePolicyDocument(raw []byte) (PolicyDocument, error) {
	var doc PolicyDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return PolicyDocument{}, errorx.Wrap(err, "policy document", errorx.KindInvalidArgument)
	}
	if doc.Version == "" {
		return PolicyDocument{}, errorx.New(errorx.KindInvalidArgument, "policy document: Version is required")
	}
	if len(doc.Statement) == 0 {
		return PolicyDocument{}, errorx.New(errorx.KindInvalidArgument, "policy document: Statement must not be empty")
	}
	for _, s := range doc.Statement {
		if s.Effect != EffectAllow && s.Effect != EffectDeny {
			return PolicyDocument{}, errorx.New(errorx.KindInvalidArgument, "policy document: statement "+s.Sid+": Effect must be Allow or Deny")
		}
		if len(s.Action) == 0 || len(s.Resource) == 0 {
			return PolicyDocument{}, errorx.New(errorx.KindInvalidArgument, "policy document: statement "+s.Sid+": Action and Resource are required")
		}
	}
	return doc, nil
}
