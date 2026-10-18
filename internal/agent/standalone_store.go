package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/listfilter"
)

// StandaloneStore persists administrators' approvals of standalone
// hosts, the supervisors those approvals register, and each
// supervisor's certificate record (ADR-0082). Every write that changes
// an agent's assignment or execution takes the execution row FOR
// UPDATE first, then the approval or registration it changes, in one
// local transaction.
type StandaloneStore interface {
	// Approve records e and answers it with created true. Where the
	// organization already holds an approval under e's request
	// identifier it writes nothing and answers that one with created
	// false; the caller compares the two.
	Approve(ctx context.Context, e Enrollment) (*Enrollment, bool, error)
	// GetEnrollment answers the organization's approval id, or
	// ErrEnrollmentNotFound.
	GetEnrollment(ctx context.Context, org grn.GRN, id EnrollmentID) (*Enrollment, error)
	// ListEnrollments pages the organization's approvals in every
	// state, oldest first.
	ListEnrollments(ctx context.Context, org grn.GRN, f listfilter.Filter) ([]EnrollmentSummary, error)
	// GetEnrollmentBySecret answers the approval holding secretSHA256,
	// or ErrBootstrapRefused.
	GetEnrollmentBySecret(ctx context.Context, secretSHA256 [32]byte) (*Enrollment, error)
	// Withdraw withdraws the organization's unspent approval id under
	// its row lock, so no enrollment read before it commits after it.
	// Withdrawing one already withdrawn changes nothing; a spent one
	// refuses with ErrEnrollmentConflict.
	Withdraw(ctx context.Context, org grn.GRN, id EnrollmentID) error
	// Enroll spends the approval req names and answers its result with
	// created true, or answers the result an identical earlier spending
	// recorded with created false. Under the approval's row
	// lock it refuses a withdrawn or expired approval with
	// ErrBootstrapRefused and another request with
	// ErrEnrollmentConflict. A new agent is written with its execution,
	// first assignment, registration and first credential; a
	// replacement locks the execution first and refuses with
	// ErrEnrollmentSuperseded unless the agent is still assigned to the
	// expected supervisor at the expected epoch. A replay is answered
	// only while its registration is still current, and refuses with
	// ErrEnrollmentSuperseded otherwise.
	Enroll(ctx context.Context, req EnrollmentRequest) (*EnrollmentResult, bool, error)
	// GetSupervisor answers the current registration of g, or
	// ErrSupervisorNotFound for one never registered or ended.
	GetSupervisor(ctx context.Context, g grn.GRN) (*Supervisor, error)
	// ListSupervisors pages the organization's supervisor
	// registrations, current and ended, oldest first.
	ListSupervisors(ctx context.Context, org grn.GRN, f listfilter.Filter) ([]SupervisorSummary, error)
	// Revoke ends the current registration of the organization's
	// supervisor g, the agent's current activation and every unspent
	// approval for that agent, under the execution lock. A supervisor
	// unknown to the organization, or already ended, is
	// ErrSupervisorNotFound.
	Revoke(ctx context.Context, org, g grn.GRN) error
	// Renew records req.Signed as the successor of the presented
	// certificate under the registration's row lock, and answers it
	// with created true. Where an identical renewal of the same
	// certificate already won it answers that one with created false;
	// another request, or a presented certificate that is neither the
	// current one nor its recorded predecessor, refuses with
	// ErrRenewalConflict, and an ended registration with
	// ErrSupervisorNotFound.
	Renew(ctx context.Context, req SupervisorRenewal) (*SupervisorCertificate, bool, error)
}
