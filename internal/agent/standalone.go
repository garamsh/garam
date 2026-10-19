package agent

import (
	"time"

	"github.com/google/uuid"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ServiceTypeSupervisor is the GRN service type of the supervisor a
// standalone host runs its agent under (ADR-0082). garam mints the
// identifier and registers the supervisor for exactly one agent.
const ServiceTypeSupervisor = "supervisor"

// CredentialStandaloneContractVersion names the contract the standalone
// enrollment route serves, carried in the Garam-Contract-Version
// header: spending an approval answers the supervisor's credential
// (ADR-0100, ADR-0118).
const CredentialStandaloneContractVersion = "standalone-enrollment.v2"

// MaxEnrollmentLifetime bounds how long an approval may stay spendable.
const MaxEnrollmentLifetime = 24 * time.Hour

// Standalone enrollment sentinels (ADR-0082).
var (
	// ErrUnsupportedStandaloneContract refuses a request carrying no
	// contract version, or another.
	ErrUnsupportedStandaloneContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: this route serves "+CredentialStandaloneContractVersion)
	// ErrBootstrapRefused is the one refusal of a bootstrap secret no
	// approval holds, or one an approval holds that expired or was
	// withdrawn: a caller learns nothing about approvals it cannot
	// spend.
	ErrBootstrapRefused = errorx.New(errorx.KindUnauthenticated, "standalone bootstrap is unknown, expired or withdrawn")
	// ErrEnrollmentConflict refuses an approval request identifier
	// reused with another request, an enrollment request that is not
	// the one an approval was spent with, and withdrawing a spent one.
	ErrEnrollmentConflict = errorx.New(errorx.KindAlreadyExists, "standalone enrollment conflicts with an earlier one")
	// ErrEnrollmentSuperseded refuses a replacement whose expected
	// assignment has moved, and the replay of an enrollment whose
	// registration is no longer the agent's current one.
	ErrEnrollmentSuperseded = errorx.New(errorx.KindFailedPrecondition,
		"the enrollment no longer describes the agent's current assignment")
	// ErrEnrollmentEnded refuses replaying an approval that was
	// withdrawn or expired: it is never revived.
	ErrEnrollmentEnded = errorx.New(errorx.KindFailedPrecondition, "standalone enrollment was withdrawn or has expired")
	// ErrEnrollmentNotFound answers an approval identifier the
	// organization does not hold.
	ErrEnrollmentNotFound = errorx.New(errorx.KindNotFound, "standalone enrollment not found")
	// ErrSupervisorNotFound answers a supervisor the organization does
	// not hold a current registration of.
	ErrSupervisorNotFound = errorx.New(errorx.KindNotFound, "supervisor not found")
	// ErrNotASupervisor refuses a caller that is not a supervisor on a
	// route only a supervisor reaches.
	ErrNotASupervisor = errorx.New(errorx.KindPermissionDenied, "caller is not a supervisor")
	// ErrNotALifecycleAuthority refuses a caller that is neither an
	// operator nor a supervisor on a lifecycle route.
	ErrNotALifecycleAuthority = errorx.New(errorx.KindPermissionDenied, "caller holds no lifecycle authority")
)

// EnrollmentID names one approval.
type EnrollmentID uuid.UUID

func (id EnrollmentID) String() string { return uuid.UUID(id).String() }

// ParseEnrollmentID reads an approval identifier off the wire.
func ParseEnrollmentID(s string) (EnrollmentID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return EnrollmentID{}, errorx.Wrap(err, "enrollment id", errorx.KindInvalidArgument)
	}
	return EnrollmentID(u), nil
}

// Replacement is the assignment a replacement approval expects to
// replace: the supervisor holding the agent and the epoch it holds it
// at when the administrator approved.
type Replacement struct {
	Agent    grn.GRN
	Assignee grn.GRN
	Epoch    int
}

// ApprovalInput is an administrator's approval as received. Every
// value but the actor, the organization, the expiry and the placement
// is one the host generated before asking; Replaces is nil for a new
// agent. Placement is PlacementStandalone or PlacementDirect.
type ApprovalInput struct {
	Actor        grn.GRN
	Org          grn.GRN
	Placement    Placement
	RequestID    RequestID
	HostName     Name
	ExpiresAt    time.Time
	SecretSHA256 [32]byte
	Replaces     *Replacement
}

// Enrollment is an approval as stored: the binding it approved, and
// what spending it answered once it was spent.
type Enrollment struct {
	ID           EnrollmentID
	Org          grn.GRN
	Placement    Placement
	Agent        grn.GRN
	Supervisor   grn.GRN
	HostName     Name
	RequestID    RequestID
	SecretSHA256 [32]byte
	Replaces     *Replacement
	ApprovedBy   grn.GRN
	ExpiresAt    time.Time
	CreatedAt    time.Time
	Withdrawn    bool
	// Spent is nil until the host enrolls.
	Spent *SpentEnrollment
}

// SpentEnrollment is the public answer an enrollment recorded, and the
// digest of the exact request it answered.
type SpentEnrollment struct {
	RequestSHA256 [32]byte
	Result        EnrollmentResult
}

// EnrollmentResult is what an enrollment answers, the first time and
// on an identical retry.
type EnrollmentResult struct {
	Placement  Placement
	Agent      grn.GRN
	Supervisor grn.GRN
	Epoch      int
	Lineage    Lineage
	// Credential is the supervisor's credential, answered by the call
	// that issued it alone: garam keeps its hash.
	Credential    string
	ServerRootPEM string
	// ReplacesActivation is the agent's most recent activation when a
	// replacement moved it, which the new supervisor's first
	// activation names; nil for a new agent and one never activated.
	ReplacesActivation *ActivationID
}

// EnrollmentInput is a host spending its approval, as received. Body
// is the exact bytes the request digest covers.
type EnrollmentInput struct {
	Secret    string
	RequestID RequestID
	Body      []byte
}

// EnrollmentRequest is a spending the service admitted, as the store
// applies it. Credential is what it issued; a retry carries a fresh
// one, which re-issues it.
type EnrollmentRequest struct {
	Enrollment    EnrollmentID
	RequestSHA256 [32]byte
	Lineage       Lineage
	Credential    *SupervisorCredential
}

// Supervisor is a current supervisor registration: the agent it holds,
// by the credential its enrollment answered.
type Supervisor struct {
	GRN        grn.GRN
	Agent      grn.GRN
	Enrollment EnrollmentID
	HostName   Name
}

// EnrollmentState is where an approval stands in a listing.
type EnrollmentState string

const (
	// EnrollmentPending is unspent, unwithdrawn and before its expiry.
	EnrollmentPending EnrollmentState = "pending"
	// EnrollmentSpent is one a host enrolled with.
	EnrollmentSpent EnrollmentState = "spent"
	// EnrollmentWithdrawn is one an administrator withdrew unspent.
	EnrollmentWithdrawn EnrollmentState = "withdrawn"
	// EnrollmentExpired is unspent and past its expiry: never revived.
	EnrollmentExpired EnrollmentState = "expired"
)

// EnrollmentSummary is an approval as an organization lists it. It has
// no field for the bootstrap secret's hash or the request digest, so a
// listing cannot carry them. SpentAt and Epoch are set together, once a
// host enrolled; WithdrawnAt is set for a withdrawn one; Replaces is
// nil for a new agent.
type EnrollmentSummary struct {
	ID          EnrollmentID
	Placement   Placement
	Agent       grn.GRN
	Supervisor  grn.GRN
	HostName    Name
	Replaces    *Replacement
	ApprovedBy  grn.GRN
	ExpiresAt   time.Time
	CreatedAt   time.Time
	WithdrawnAt *time.Time
	SpentAt     *time.Time
	Epoch       int
}

// State answers where the approval stands at now.
func (e EnrollmentSummary) State(now time.Time) EnrollmentState {
	switch {
	case e.SpentAt != nil:
		return EnrollmentSpent
	case e.WithdrawnAt != nil:
		return EnrollmentWithdrawn
	case !now.Before(e.ExpiresAt):
		return EnrollmentExpired
	}
	return EnrollmentPending
}

// SupervisorSummary is a supervisor registration as an organization
// lists it, current or ended. It has no field for a credential. Epoch
// is the assignment epoch its enrollment gave it; LastActivatedAt is
// the latest activation of its agent at that epoch, nil where it never
// activated, and EndedAt is nil while the registration is current.
type SupervisorSummary struct {
	GRN             grn.GRN
	Agent           grn.GRN
	Enrollment      EnrollmentID
	Placement       Placement
	HostName        Name
	Epoch           int
	RegisteredAt    time.Time
	EndedAt         *time.Time
	LastActivatedAt *time.Time
}
