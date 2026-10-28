package authz

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"time"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/wire"
)

// ServiceTypePolicy is the GRN service type of a stored policy.
const ServiceTypePolicy = "policy"

// ParsePolicyStatus parses a wire.PolicyStatus from its stored form.
func ParsePolicyStatus(s string) (wire.PolicyStatus, error) {
	switch p := wire.PolicyStatus(s); p {
	case wire.PolicyStatusDraft, wire.PolicyStatusActive:
		return p, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "authz: unknown policy status")
}

// Policy is an org-managed, versioned rule set. Only an active policy
// contributes statements to a check, and only at its default version.
// Every field is persisted on the row and exported: the type holds no
// invariant over them, so it is a data carrier (ADR-0069).
type Policy struct {
	GRN            grn.GRN
	Organization   grn.GRN
	Name           string
	Status         wire.PolicyStatus
	DefaultVersion int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewPolicy constructs a draft Policy owned by orgGRN, pointing at the
// first version it will be stored with.
func NewPolicy(g, orgGRN grn.GRN, name string) *Policy {
	now := time.Now().UTC()
	return &Policy{
		GRN:            g,
		Organization:   orgGRN,
		Name:           name,
		Status:         wire.PolicyStatusDraft,
		DefaultVersion: firstPolicyVersion,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// PolicyVersion is one immutable revision of a policy's document: the
// document CreatedBy recorded at CreatedAt, numbered Version. It is a
// data carrier, its fields exported (ADR-0069).
type PolicyVersion struct {
	Policy    grn.GRN
	Version   int
	Document  PolicyDocument
	CreatedBy grn.GRN
	CreatedAt time.Time
}

// PolicyAttachment binds a policy to the subject it applies to
// (ADR-0008). A subject is a user or a group of the policy's org; the
// engine matches its GRN as a string and never resolves it. It is a
// data carrier, its fields exported (ADR-0069).
type PolicyAttachment struct {
	Policy    grn.GRN
	Subject   grn.GRN
	CreatedAt time.Time
}

// ActivePolicy is one active policy's contribution to evaluation: the
// policy, the document it evaluates and the subjects it is attached
// to. No attachment means the policy applies org-wide (ADR-0008).
type ActivePolicy struct {
	GRN      grn.GRN
	Document PolicyDocument
	Subjects []grn.GRN
}

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
// so stored documents are known-good before they reach evaluation. A
// key outside the document's shape is refused here rather than dropped,
// so what a caller wrote is what gets stored.
func ParsePolicyDocument(raw []byte) (PolicyDocument, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc PolicyDocument
	if err := dec.Decode(&doc); err != nil {
		return PolicyDocument{}, locateDecodeRefusal(raw, err)
	}
	// Decode stops at the end of the first value and would leave
	// anything after it unread; io.EOF is what says there was nothing.
	if err := dec.Decode(new(json.RawMessage)); !errors.Is(err, io.EOF) {
		return PolicyDocument{}, documentRefusal(ReasonPolicyDocumentTrailingData, "", "unexpected data after the document")
	}
	if doc.Version == "" {
		return PolicyDocument{}, documentRefusal(ReasonPolicyDocumentVersionMissing, "/Version", "Version is required")
	}
	if len(doc.Statement) == 0 {
		return PolicyDocument{}, documentRefusal(ReasonPolicyDocumentStatementEmpty, "/Statement", "Statement must not be empty")
	}
	for i, s := range doc.Statement {
		at := "/Statement/" + strconv.Itoa(i)
		if s.Effect != wire.EffectAllow && s.Effect != wire.EffectDeny {
			return PolicyDocument{}, documentRefusal(ReasonPolicyDocumentEffectInvalid, at+"/Effect", "statement "+s.Sid+": Effect must be Allow or Deny")
		}
		if len(s.Action) == 0 {
			return PolicyDocument{}, documentRefusal(ReasonPolicyDocumentActionMissing, at+"/Action", "statement "+s.Sid+": Action is required")
		}
		if len(s.Resource) == 0 {
			return PolicyDocument{}, documentRefusal(ReasonPolicyDocumentResourceMissing, at+"/Resource", "statement "+s.Sid+": Resource is required")
		}
	}
	return doc, nil
}
