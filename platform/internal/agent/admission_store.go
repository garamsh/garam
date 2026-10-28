package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// AdmissionStore persists agent-enrollment.v1 approvals and spends
// them (ADR-0102).
type AdmissionStore interface {
	// PrepareApproval records a and answers it with created true.
	// Where an approval already holds a's request identifier in its
	// organization it writes nothing and answers that one with created
	// false, and the caller compares the two; where one holds a's
	// reference under another request identifier it refuses with
	// ErrApprovalBindingConflict.
	PrepareApproval(ctx context.Context, a EnrollmentApproval) (*EnrollmentApproval, bool, error)
	// GetApproval answers the organization's approval id, or
	// ErrApprovalNotFound.
	GetApproval(ctx context.Context, org grn.GRN, id ApprovalID) (*EnrollmentApproval, error)
	// Admit spends approval id on c under the approval's row lock:
	// it writes the agent as IdentityAdmission describes, with c as its
	// managed creation, and answers c as stored with created true. A
	// replacement approval instead moves c.Agent from the assignment it
	// expects onto c.Controller in c.Lineage, holding the agent's
	// execution row first, records no creation and answers c with the
	// epoch it wrote; an agent no longer at that assignment refuses
	// with ErrAdmissionSuperseded. An approval already spent writes
	// nothing and answers what it recorded with created false. A
	// reference or request identifier another creation holds refuses
	// with ErrApprovalSpent.
	Admit(ctx context.Context, id ApprovalID, c ManagedCreation) (*ManagedCreation, bool, error)
}
