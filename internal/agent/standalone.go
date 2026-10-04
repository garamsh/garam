package agent

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"regexp"
	"time"

	"github.com/google/uuid"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ServiceTypeSupervisor is the GRN service type of the supervisor a
// standalone host runs its agent under (ADR-0082). garam mints the
// identifier and registers the supervisor for exactly one agent.
const ServiceTypeSupervisor = "supervisor"

// StandaloneContractVersion names the contract the standalone
// enrollment routes serve, carried in the Garam-Contract-Version
// header.
const StandaloneContractVersion = "standalone-enrollment.v1"

// MaxEnrollmentLifetime bounds how long an approval may stay spendable.
const MaxEnrollmentLifetime = 24 * time.Hour

// Standalone enrollment sentinels (ADR-0082).
var (
	// ErrUnsupportedStandaloneContract refuses a request carrying no
	// contract version, or another.
	ErrUnsupportedStandaloneContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: this route serves "+StandaloneContractVersion)
	// ErrBootstrapRefused is the one refusal of a bootstrap secret no
	// approval holds, or one an approval holds that expired or was
	// withdrawn: a caller learns nothing about approvals it cannot
	// spend.
	ErrBootstrapRefused = errorx.New(errorx.KindUnauthenticated, "standalone bootstrap is unknown, expired or withdrawn")
	// ErrKeyNotApproved refuses a certificate request whose key is not
	// the one the administrator approved.
	ErrKeyNotApproved = errorx.New(errorx.KindPermissionDenied, "a certificate request's key is not the approved one")
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
	// ErrRenewalTooEarly refuses renewing a certificate with more than
	// a third of its validity left.
	ErrRenewalTooEarly = errorx.New(errorx.KindFailedPrecondition, "certificate has more than a third of its validity left")
	// ErrRenewalConflict refuses a renewal presented by a certificate
	// that is neither the registration's current one nor its immediate
	// predecessor, and a predecessor's retry with another request.
	ErrRenewalConflict = errorx.New(errorx.KindAlreadyExists, "supervisor renewal conflicts with the recorded one")
)

// KeyFingerprint is the SHA-256 of a public key's SubjectPublicKeyInfo
// DER: what an administrator approves of a host's key.
type KeyFingerprint [32]byte

func (f KeyFingerprint) String() string { return hex.EncodeToString(f[:]) }

var fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ParseKeyFingerprint reads 64 lowercase hexadecimal characters.
func ParseKeyFingerprint(s string) (KeyFingerprint, error) {
	var f KeyFingerprint
	if !fingerprintPattern.MatchString(s) {
		return f, errorx.New(errorx.KindInvalidArgument, "a fingerprint is 64 lowercase hexadecimal characters")
	}
	_, _ = hex.Decode(f[:], []byte(s))
	return f, nil
}

// RequestedKeyFingerprint answers the fingerprint of the key a PEM
// certificate signing request carries, once its own signature proves
// the sender holds that key. A request that proves nothing is
// ErrInvalidCertificateRequest.
func RequestedKeyFingerprint(requestPEM []byte) (KeyFingerprint, error) {
	block, _ := pem.Decode(requestPEM)
	if block == nil {
		return KeyFingerprint{}, ErrInvalidCertificateRequest
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return KeyFingerprint{}, ErrInvalidCertificateRequest
	}
	return sha256.Sum256(csr.RawSubjectPublicKeyInfo), nil
}

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
// value but the actor, the organization and the expiry is one the host
// generated before asking; Replaces is nil for a new agent.
type ApprovalInput struct {
	Actor         grn.GRN
	Org           grn.GRN
	RequestID     RequestID
	HostName      Name
	ExpiresAt     time.Time
	SecretSHA256  [32]byte
	SupervisorKey KeyFingerprint
	AgentKey      KeyFingerprint
	Replaces      *Replacement
}

// Enrollment is an approval as stored: the binding it approved, and
// what spending it answered once it was spent.
type Enrollment struct {
	ID            EnrollmentID
	Org           grn.GRN
	Agent         grn.GRN
	Supervisor    grn.GRN
	HostName      Name
	RequestID     RequestID
	SecretSHA256  [32]byte
	SupervisorKey KeyFingerprint
	AgentKey      KeyFingerprint
	Replaces      *Replacement
	ApprovedBy    grn.GRN
	ExpiresAt     time.Time
	CreatedAt     time.Time
	Withdrawn     bool
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
// on an identical retry. It carries no private key: the host generated
// both before the approval.
type EnrollmentResult struct {
	Agent                    grn.GRN
	Supervisor               grn.GRN
	Epoch                    int
	Lineage                  Lineage
	SupervisorCertificatePEM string
	AgentCertificatePEM      string
	IssuerPEM                string
	ServerRootPEM            string
}

// EnrollmentInput is a host spending its approval, as received. Body
// is the exact bytes the request digest covers.
type EnrollmentInput struct {
	Secret                          string
	RequestID                       RequestID
	Body                            []byte
	SupervisorCertificateRequestPEM []byte
	AgentCertificateRequestPEM      []byte
}

// EnrollmentRequest is a spending the service admitted, as the store
// applies it. Supervisor and AgentCertificate are nil when the
// approval was already spent and is only to be answered again.
type EnrollmentRequest struct {
	Enrollment    EnrollmentID
	RequestSHA256 [32]byte
	Lineage       Lineage
	Supervisor    *SignedCertificate
	Agent         *SignedCertificate
}

// Supervisor is a current supervisor registration: the agent it holds
// and which certificate garam last issued it, until when. Its
// lifecycle authority over the agent holds until that expiry.
type Supervisor struct {
	GRN        grn.GRN
	Agent      grn.GRN
	Enrollment EnrollmentID
	HostName   Name
	Serial     string
	NotAfter   time.Time
}

// SupervisorRenewal is a renewal the service admitted, as the store
// applies it. Signed is nil when only a recorded renewal is to be
// answered again.
type SupervisorRenewal struct {
	Supervisor    grn.GRN
	Presented     Credential
	RequestID     RequestID
	RequestSHA256 [32]byte
	Signed        *SignedCertificate
}

// SupervisorCertificate is the public answer of a supervisor renewal.
type SupervisorCertificate struct {
	CertificatePEM string
	IssuerPEM      string
	ServerRootPEM  string
	NotAfter       time.Time
}
