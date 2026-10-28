package agent

import (
	"time"

	"github.com/google/uuid"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
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
	// PlacementStandalone is a runtime a standalone host's supervisor
	// starts as its three containers (ADR-0085).
	PlacementStandalone Placement = "standalone"
	// PlacementDirect is a runtime running independently of the
	// Operator, which a standalone host's supervisor attaches to and
	// starts nothing of (ADR-0111).
	PlacementDirect Placement = "direct"
)

// ParsePlacement reads a placement off the wire.
func ParsePlacement(s string) (Placement, error) {
	switch p := Placement(s); p {
	case PlacementManaged, PlacementStandalone, PlacementDirect:
		return p, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "placement is managed, standalone or direct")
}

// Common enrollment sentinels (ADR-0102). Each refusal the contract
// names a reason for is one of them.
var (
	// ErrUnsupportedAdmissionContract refuses a request carrying no
	// contract version, or another.
	ErrUnsupportedAdmissionContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: this route serves "+AdmissionContractVersion)
	// ErrDirectPlacementRequiresAdministratorApproval refuses direct
	// placement under this contract, permanently: a direct agent's
	// authority is an administrator's agent:enroll approval and a
	// supervisor registered for that one agent, never an operator
	// (ADR-0111). The wire keeps the reason v1 shipped it under,
	// direct_placement_gated.
	ErrDirectPlacementRequiresAdministratorApproval = errorx.New(errorx.KindFailedPrecondition,
		"direct placement is admitted by an administrator's approval of a standalone host, not under "+
			AdmissionContractVersion)
	// ErrApprovalBindingConflict refuses a binding other than the one
	// an approval under the same request identifier holds, and a
	// reference minted for another assignee or request identifier, or
	// for a replacement another agent or expected assignment.
	ErrApprovalBindingConflict = errorx.New(errorx.KindAlreadyExists,
		"the binding conflicts with the approval prepared under this request identifier")
	// ErrApprovalSpent refuses admitting a spent approval for another
	// binding, and an approval whose reference another creation spent.
	ErrApprovalSpent = errorx.New(errorx.KindAlreadyExists, "the approval was spent on another admission")
	// ErrAdmissionSuperseded refuses the retry of an admission whose
	// agent has moved since, and a replacement whose agent left the
	// assignment it expected before admission spent it.
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
// member's reference, agent:create for a new agent. Replaces is the
// agent a replacement moves and the assignment it expects, and nil for
// a new agent; its reference is agent:replace (ADR-0107). Name is the
// name a new agent is given, nil for none (ADR-0119).
type EnrollmentBinding struct {
	RequestID    RequestID
	Placement    Placement
	Assignee     grn.GRN
	OperationRef string
	Replaces     *Replacement
	Name         *Name
}

// ErrReplacementKeepsName refuses a name on a replacement: it moves an
// agent that already has whatever name it has (ADR-0119).
var ErrReplacementKeepsName = errorx.New(errorx.KindInvalidArgument,
	"a replacement keeps the agent's name; rename it on the browser surface")

// admissible refuses what the contract does not admit, before any
// authority is read: it admits managed placement alone.
func (b EnrollmentBinding) admissible() error {
	if b.Replaces != nil && b.Name != nil {
		return ErrReplacementKeepsName
	}
	switch b.Placement {
	case PlacementManaged:
		return nil
	case PlacementDirect:
		return ErrDirectPlacementRequiresAdministratorApproval
	}
	return errorx.New(errorx.KindInvalidArgument, "placement is managed or direct")
}

// EnrollmentApproval is an approval as stored: the binding the
// audience prepared and, once admission spent it, what it admitted: the
// managed creation of a new agent, or the agent a replacement moved
// with the controller, epoch and lineage it moved it to.
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
	sameReplaced := held.Replaces == nil && b.Replaces == nil ||
		held.Replaces != nil && b.Replaces != nil && *held.Replaces == *b.Replaces
	return held.RequestID == b.RequestID && held.Placement == b.Placement && held.Assignee == b.Assignee &&
		held.OperationRef == b.OperationRef && sameReplaced && sameName(held.Name, b.Name)
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
	// Name is a new agent's name, nil for none.
	Name *Name
}
