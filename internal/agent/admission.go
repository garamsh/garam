package agent

import (
	"time"

	"github.com/google/uuid"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// AdmissionContractVersion names the common enrollment contract both
// placements share (ADR-0102 §Common agent enrollment), carried in the
// Garam-Contract-Version header. It is served to a token alone.
const AdmissionContractVersion = "agent-enrollment.v1"

// Placement is where an admitted agent's runtime runs.
type Placement string

const (
	// PlacementManaged is a runtime the Operator provisions.
	PlacementManaged Placement = "managed"
	// PlacementDirect is a runtime running independently of the
	// Operator. It is refused until #1355 decides its writer proof.
	PlacementDirect Placement = "direct"
)

// ParsePlacement reads a placement off the wire.
func ParsePlacement(s string) (Placement, error) {
	switch p := Placement(s); p {
	case PlacementManaged, PlacementDirect:
		return p, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "placement is managed or direct")
}

// Common enrollment sentinels (ADR-0102). Each refusal the contract
// names a reason for is one of them.
var (
	// ErrUnsupportedAdmissionContract refuses a request carrying no
	// contract version, or another.
	ErrUnsupportedAdmissionContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: this route serves "+AdmissionContractVersion)
	// ErrDirectPlacementGated refuses direct placement until #1355
	// decides how a directly running runtime's writer is proven.
	ErrDirectPlacementGated = errorx.New(errorx.KindFailedPrecondition,
		"direct placement is refused until its writer proof is decided (#1355)")
	// ErrReplacementUnavailable refuses a replacement: no reference
	// kind yet authorizes moving a managed agent to another assignee.
	ErrReplacementUnavailable = errorx.New(errorx.KindFailedPrecondition,
		"no reference authorizes replacing a managed agent's assignment")
	// ErrApprovalBindingConflict refuses a binding other than the one
	// an approval under the same request identifier holds, and a
	// reference minted for another assignee or request identifier.
	ErrApprovalBindingConflict = errorx.New(errorx.KindAlreadyExists,
		"the binding conflicts with the approval prepared under this request identifier")
	// ErrApprovalSpent refuses admitting a spent approval for another
	// binding, and an approval whose reference another creation spent.
	ErrApprovalSpent = errorx.New(errorx.KindAlreadyExists, "the approval was spent on another admission")
	// ErrAdmissionSuperseded refuses the retry of an admission whose
	// agent has moved since.
	ErrAdmissionSuperseded = errorx.New(errorx.KindFailedPrecondition,
		"the admitted agent is no longer where admission put it")
	// ErrApprovalNotFound answers an approval the caller's
	// organization does not hold, or one prepared by another audience.
	ErrApprovalNotFound = errorx.New(errorx.KindNotFound, "agent enrollment approval not found")
)

// ApprovalID names one agent-enrollment.v1 approval.
type ApprovalID uuid.UUID

func (id ApprovalID) String() string { return uuid.UUID(id).String() }

// ParseApprovalID reads an approval identifier off the wire.
func ParseApprovalID(s string) (ApprovalID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return ApprovalID{}, errorx.Wrap(err, "approval id", errorx.KindInvalidArgument)
	}
	return ApprovalID(u), nil
}

// EnrollmentBinding is what an approval binds and what admitting it
// repeats: the request identifier, the placement, the assignee and the
// member's agent:create reference. Replaces is the assignment a
// replacement expects, and nil for a new agent.
type EnrollmentBinding struct {
	RequestID    RequestID
	Placement    Placement
	Assignee     grn.GRN
	OperationRef string
	Replaces     *Replacement
}

// admissible refuses what the contract models and does not admit yet,
// before any authority is read.
func (b EnrollmentBinding) admissible() error {
	switch {
	case b.Placement == PlacementDirect:
		return ErrDirectPlacementGated
	case b.Replaces != nil:
		return ErrReplacementUnavailable
	}
	return nil
}

// EnrollmentApproval is an approval as stored: the binding the
// audience prepared and, once admission spent it, the managed creation
// it admitted.
type EnrollmentApproval struct {
	ID        ApprovalID
	Org       grn.GRN
	Audience  grn.GRN
	Binding   EnrollmentBinding
	CreatedAt time.Time
	// Admitted is nil until admission spends the approval.
	Admitted *ManagedCreation
}

// binds reports whether b is the binding the approval holds.
func (a *EnrollmentApproval) binds(b EnrollmentBinding) bool {
	held := a.Binding
	return held.RequestID == b.RequestID && held.Placement == b.Placement && held.Assignee == b.Assignee &&
		held.OperationRef == b.OperationRef && held.Replaces == nil && b.Replaces == nil
}

// IdentityAdmission is what admitting an identity writes, under
// whichever contract admits it (ADR-0102 §Common agent enrollment): a
// new agent with its first assignment to Assignee, or, where Replaces
// is set, the move of an existing one from exactly the assignment it
// expects.
// Either way the agent's execution is fenced in Lineage, which holds
// no credential yet, with no active generation, and nothing outside
// garam is provisioned.
type IdentityAdmission struct {
	Agent    grn.GRN
	Org      grn.GRN
	Assignee grn.GRN
	Lineage  Lineage
	Replaces *Replacement
}
