package audit

import (
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// CredentialKind is which machine credential an event is about
// (ADR-0114).
type CredentialKind string

const (
	// CredentialOperatorCertificate is an operator's client
	// certificate.
	CredentialOperatorCertificate CredentialKind = "operator_certificate"
	// CredentialOperatorCredential is the credential an operator
	// exchanges for tokens (ADR-0100).
	CredentialOperatorCredential CredentialKind = "operator_credential"
	// CredentialOperatorEnrollmentToken is the one-time token an
	// operator enrolls with (ADR-0064).
	CredentialOperatorEnrollmentToken CredentialKind = "operator_enrollment_token"
	// CredentialSupervisorCertificate is a standalone host's
	// supervisor certificate (ADR-0082).
	CredentialSupervisorCertificate CredentialKind = "supervisor_certificate"
	// CredentialSupervisorCredential is the credential a supervisor
	// enrolled under standalone-enrollment.v2 exchanges for tokens.
	CredentialSupervisorCredential CredentialKind = "supervisor_credential"
	// CredentialAgentToken is a token an agent's assignee minted for it.
	CredentialAgentToken CredentialKind = "agent_token"
)

// CredentialAction is what was asked of garam.
type CredentialAction string

const (
	// ActionIssue is a credential issued where none was presented to
	// replace.
	ActionIssue CredentialAction = "issue"
	// ActionRenew is a credential issued to replace the one presented.
	ActionRenew CredentialAction = "renew"
)

// Outcome is what garam decided.
type Outcome string

const (
	// OutcomeIssued is a request garam answered with a credential.
	OutcomeIssued Outcome = "issued"
	// OutcomeRefused is a request garam refused.
	OutcomeRefused Outcome = "refused"
)

// Validity is when an issued credential is accepted. NotBefore is the
// zero time for a credential that names none, a token among them, and
// NotAfter for one that never expires, an operator's credential among
// them.
type Validity struct {
	NotBefore time.Time
	NotAfter  time.Time
}

// Credential is one issuance, renewal or refusal of a machine
// credential as garam decided it (ADR-0114). It never carries the
// credential, a key, a certificate or any part or hash of one. The
// fields are closed: an issued event may name a serial and a validity
// and never a reason, and a refused one names a reason and nothing
// issued.
type Credential struct {
	subject  grn.GRN
	kind     CredentialKind
	action   CredentialAction
	outcome  Outcome
	replay   bool
	reason   string
	serial   string
	validity Validity
}

// CredentialIssued is a credential of kind issued to subject. serial is a
// certificate's decimal serial, empty for a credential that has none.
// replay is an answer that repeated what an identical earlier request
// issued.
func CredentialIssued(subject grn.GRN, kind CredentialKind, action CredentialAction, serial string, v Validity, replay bool) Credential {
	return Credential{subject: subject, kind: kind, action: action, outcome: OutcomeIssued,
		replay: replay, serial: serial, validity: v}
}

// CredentialRefused is a request for a credential of kind for subject that garam
// refused with a failure of reason.
func CredentialRefused(subject grn.GRN, kind CredentialKind, action CredentialAction, reason errorx.Kind) Credential {
	return Credential{subject: subject, kind: kind, action: action, outcome: OutcomeRefused, reason: reason.String()}
}

// Subject is the identity the credential is for.
func (c Credential) Subject() grn.GRN { return c.subject }

// Kind is which credential.
func (c Credential) Kind() CredentialKind { return c.kind }

// Action is what was asked.
func (c Credential) Action() CredentialAction { return c.action }

// Outcome is what garam decided.
func (c Credential) Outcome() Outcome { return c.outcome }

// Replay reports whether an issued answer repeated an earlier one.
func (c Credential) Replay() bool { return c.replay }

// Reason is the name of a refusal's errorx kind, the class of the
// failure and never its text; ok is false for an issued event.
func (c Credential) Reason() (string, bool) {
	return c.reason, c.outcome == OutcomeRefused
}

// Serial is an issued certificate's serial, empty where none.
func (c Credential) Serial() string { return c.serial }

// Validity is when an issued credential is accepted.
func (c Credential) Validity() Validity { return c.validity }

// RecordedCredential is a Credential as stored, with when garam
// recorded it.
type RecordedCredential struct {
	Credential
	RecordedAt time.Time
}

// Reconstruct rebuilds a stored event. It is the read side's
// constructor and checks what the table's constraint already holds.
func Reconstruct(subject grn.GRN, kind CredentialKind, action CredentialAction, outcome Outcome,
	replay bool, reason, serial string, v Validity, recordedAt time.Time) (RecordedCredential, error) {
	var c Credential
	switch outcome {
	case OutcomeIssued:
		c = CredentialIssued(subject, kind, action, serial, v, replay)
	case OutcomeRefused:
		c = Credential{subject: subject, kind: kind, action: action, outcome: OutcomeRefused, reason: reason}
	default:
		return RecordedCredential{}, errorx.New(errorx.KindInternal, "audit: unknown outcome "+string(outcome))
	}
	return RecordedCredential{Credential: c, RecordedAt: recordedAt}, nil
}
