package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Execution sentinels (ADR-0078). A refusal of a protected write names
// which part of the fence refused it; none says which process holds
// the agent now.
var (
	// ErrExecutionNotCutOver refuses an execution token presented for
	// an agent that has never been activated: its mode is still legacy,
	// and a token cannot select another.
	ErrExecutionNotCutOver = errorx.New(errorx.KindFailedPrecondition, "agent has not cut over to activated execution")
	// ErrExecutionTokenRequired refuses a write without a token for an
	// agent whose execution is fenced.
	ErrExecutionTokenRequired = errorx.New(errorx.KindUnauthenticated, "execution token required")
	// ErrExecutionTokenStale refuses a token that is not the current
	// activation's: an older generation's, a deactivated one, or one a
	// remint replaced.
	ErrExecutionTokenStale = errorx.New(errorx.KindPermissionDenied, "execution token is not current")
	// ErrCredentialFenced refuses a certificate outside its validity at
	// this instant or outside the agent's current lineage.
	ErrCredentialFenced = errorx.New(errorx.KindFailedPrecondition, "credential is not current")
	// ErrActivationStale refuses an activation naming another
	// activation than the agent's most recent one as what it replaces.
	ErrActivationStale = errorx.New(errorx.KindAborted, "activation does not replace the most recent one")
	// ErrActivationConflict refuses a request identifier already used
	// with a different request.
	ErrActivationConflict = errorx.New(errorx.KindAlreadyExists, "activation request identifier reused with a different request")
	// ErrActivationEnded refuses a retry of an activation that has
	// ended: an ended activation is never revived.
	ErrActivationEnded = errorx.New(errorx.KindFailedPrecondition, "activation has ended")
	// ErrGenerationUsed refuses a generation already activated in the
	// agent's current lineage.
	ErrGenerationUsed = errorx.New(errorx.KindAlreadyExists, "generation was already activated in this lineage")
	// ErrActivationNotFound answers an activation identifier the agent
	// never had.
	ErrActivationNotFound = errorx.New(errorx.KindNotFound, "activation not found")
	// ErrAuthorityExpired refuses an act whose grant or handoff expired
	// before the fence was taken, however recently it was checked.
	ErrAuthorityExpired = errorx.New(errorx.KindPermissionDenied, "authority expired")
	// ErrRecoveryConflict refuses a recovery request identifier reused
	// with another body or reference, or a reference that already
	// recovered.
	ErrRecoveryConflict = errorx.New(errorx.KindAlreadyExists, "recovery conflicts with an earlier one")
)

// Mode is whether an agent's writes are fenced by an activation. An
// agent is legacy until its first activation and fenced from then on;
// nothing a caller sends selects it.
type Mode string

const (
	ModeLegacy Mode = "legacy"
	ModeFenced Mode = "fenced"
)

// ParseMode reads a stored mode.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case ModeLegacy, ModeFenced:
		return m, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "unknown execution mode")
}

var generationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Generation names one process start of an agent: 128 random bits in
// lowercase hex, minted by the runtime. It is neither the epoch nor a
// Pod (ADR-0078).
type Generation string

// ParseGeneration accepts exactly the runtime's form.
func ParseGeneration(s string) (Generation, error) {
	if !generationPattern.MatchString(s) {
		return "", errorx.New(errorx.KindInvalidArgument, "generation must be 32 lowercase hex characters")
	}
	return Generation(s), nil
}

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,128}$`)

// RequestID is what makes a lifecycle request retryable: the same
// identifier with the same request answers what the first one did.
type RequestID string

// ParseRequestID accepts 1 to 128 unreserved URL characters.
func ParseRequestID(s string) (RequestID, error) {
	if !requestIDPattern.MatchString(s) {
		return "", errorx.New(errorx.KindInvalidArgument, "request identifier must be 1-128 unreserved characters")
	}
	return RequestID(s), nil
}

// ActivationID names one activation. It never changes, so it is what
// a later activation names as the one it replaces.
type ActivationID uuid.UUID

func (id ActivationID) String() string { return uuid.UUID(id).String() }

// ParseActivationID reads an activation identifier off the wire.
func ParseActivationID(s string) (ActivationID, error) {
	u, err := uuid.Parse(s)
	if err != nil {
		return ActivationID{}, errorx.Wrap(err, "activation id", errorx.KindInvalidArgument)
	}
	return ActivationID(u), nil
}

// Lineage names the credentials a recovery issued from. The legacy
// cohort, every certificate issued before an agent's first recovery,
// has none, and is a nil *Lineage wherever one is optional.
type Lineage uuid.UUID

func (l Lineage) String() string { return uuid.UUID(l).String() }

// NewLineage mints the lineage a recovery moves an agent to.
func NewLineage() Lineage { return Lineage(uuid.New()) }

// Token is the secret an activation hands the runtime. Only its hash
// is stored.
type Token [32]byte

// TokenHash is what a store keeps of a Token and compares against.
type TokenHash [32]byte

// NewToken draws a token from the system's random source.
func NewToken() (Token, error) {
	var t Token
	if _, err := rand.Read(t[:]); err != nil {
		return Token{}, errorx.Wrap(err, "execution token")
	}
	return t, nil
}

// ParseToken reads a token in the unpadded base64url form String
// writes.
func ParseToken(s string) (Token, error) {
	var t Token
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || len(b) != len(t) {
		return Token{}, errorx.New(errorx.KindInvalidArgument, "execution token is malformed")
	}
	copy(t[:], b)
	return t, nil
}

func (t Token) String() string { return base64.RawURLEncoding.EncodeToString(t[:]) }

// Hash returns what a store keeps of the token.
func (t Token) Hash() TokenHash { return sha256.Sum256(t[:]) }

// Credential is a certificate as the fence reads it: which one, and
// when it is valid. It is taken from a certificate whose chain was
// verified, never from a field a caller wrote.
type Credential struct {
	Serial    string
	NotBefore time.Time
	NotAfter  time.Time
}

// Admission is what the fence answered a protected write.
type Admission string

const (
	AdmissionAdmitted         Admission = "admitted"
	AdmissionUnknownAgent     Admission = "unknown_agent"
	AdmissionNotCutOver       Admission = "not_cut_over"
	AdmissionTokenRequired    Admission = "token_required"
	AdmissionTokenStale       Admission = "token_stale"
	AdmissionCredentialFenced Admission = "credential_fenced"
)

// Err returns nil for an admitted write and the refusal otherwise. An
// answer the fence does not give is an internal error rather than an
// admission.
func (a Admission) Err() error {
	switch a {
	case AdmissionAdmitted:
		return nil
	case AdmissionUnknownAgent:
		return ErrAgentNotFound
	case AdmissionNotCutOver:
		return ErrExecutionNotCutOver
	case AdmissionTokenRequired:
		return ErrExecutionTokenRequired
	case AdmissionTokenStale:
		return ErrExecutionTokenStale
	case AdmissionCredentialFenced:
		return ErrCredentialFenced
	}
	return errorx.New(errorx.KindInternal, "unknown admission answer")
}

// EndReason says what ended an activation.
type EndReason string

const (
	EndSuperseded  EndReason = "superseded"
	EndDeactivated EndReason = "deactivated"
	EndRecovered   EndReason = "recovered"
	EndReassigned  EndReason = "reassigned"
)

// Activation is one generation of an agent admitted to act, under the
// lineage it was checked in.
type Activation struct {
	ID           ActivationID
	Agent        grn.GRN
	Generation   Generation
	Epoch        int
	Lineage      *Lineage
	TokenVersion int64
	// Ended is nil while the activation is current.
	Ended *EndReason
}

// Lifecycle is what a lifecycle authority established before the
// fence is taken (ADR-0078): which assignee it covered, and until when
// its grant holds. The store rechecks both under the lock.
type Lifecycle struct {
	Assignee       grn.GRN
	GrantExpiresAt time.Time
}

// ActivationRequest is an activation the authority admitted, as the
// store applies it. Replaces is nil only for an agent never activated.
type ActivationRequest struct {
	Agent        grn.GRN
	RequestID    RequestID
	Generation   Generation
	Epoch        int
	Replaces     *ActivationID
	Audience     grn.GRN
	GrantID      string
	OperationRef *string
	Authority    Lifecycle
	Credential   Credential
	TokenHash    TokenHash
}

// Execution is an agent's execution as it stands: its mode, the
// activation admitting writes now if any, and the most recent one.
type Execution struct {
	Agent   grn.GRN
	Mode    Mode
	Lineage *Lineage
	Current *CurrentActivation
	Latest  *ActivationID
}

// CurrentActivation is the activation whose token admits writes now.
type CurrentActivation struct {
	ID         ActivationID
	Generation Generation
}

// RecoveryRequest is a recovery the authority admitted: the new lineage
// and the certificate it starts with, signed over the requester's key.
type RecoveryRequest struct {
	Agent          grn.GRN
	RequestID      RequestID
	Epoch          int
	Authority      Lifecycle
	TokenExpiresAt time.Time
	OperationRef   string
	BodySHA256     [32]byte
	Lineage        Lineage
	Credential     Credential
	CertificatePEM string
}

// Recovery is what a recovery answers, the first time and on a retry.
type Recovery struct {
	Lineage        Lineage
	CertificatePEM string
}
