package wire

import authzv1 "github.com/garamsh/garam/platform/proto/gen/garam/authz/v1"

// PolicyStatus is a stored policy's lifecycle state (ADR-0002). One
// declaration serves the engine that stores it and the surface that
// reads it back; the engine owns the semantics.
type PolicyStatus string

const (
	PolicyStatusDraft  PolicyStatus = "draft"
	PolicyStatusActive PolicyStatus = "active"
)

// PolicyStatusToProto renders a PolicyStatus on the wire. A status
// that is none of the two constants renders as unspecified.
func PolicyStatusToProto(s PolicyStatus) authzv1.PolicyStatus {
	switch s {
	case PolicyStatusDraft:
		return authzv1.PolicyStatus_POLICY_STATUS_DRAFT
	case PolicyStatusActive:
		return authzv1.PolicyStatus_POLICY_STATUS_ACTIVE
	}
	return authzv1.PolicyStatus_POLICY_STATUS_UNSPECIFIED
}

// PolicyStatusFromProto maps the wire enum back to a PolicyStatus. An
// unspecified status keeps the zero value, which no handler renders as
// a lifecycle state.
func PolicyStatusFromProto(s authzv1.PolicyStatus) PolicyStatus {
	switch s {
	case authzv1.PolicyStatus_POLICY_STATUS_DRAFT:
		return PolicyStatusDraft
	case authzv1.PolicyStatus_POLICY_STATUS_ACTIVE:
		return PolicyStatusActive
	}
	return ""
}
