package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// CutoverContractVersion names the contract the cutover routes serve,
// carried in the Garam-Contract-Version header (ADR-0086).
const CutoverContractVersion = "agent-cutover.v1"

// TokenCutoverContractVersion is the same contract for a request a
// token authenticated, whose agent:cutover reference travels in a
// Garam-Operation header of its own (ADR-0100).
const TokenCutoverContractVersion = "agent-cutover.v2"

// Cutover sentinels (ADR-0086). The machine listener names each on
// the wire as a reason beside its kind, so the Operator can tell a
// stale replay from a source that moved.
var (
	// ErrCutoverRequired refuses the first activation of a legacy agent
	// whose cutover has not been switched: nothing imported its source,
	// so no generation of it may start under the Operator.
	ErrCutoverRequired = errorx.New(errorx.KindFailedPrecondition,
		"the agent has not been cut over: its source must be imported, frozen and switched before it activates")
	// ErrCutoverDigestMismatch refuses a freeze whose digest is not the
	// source's as it stands under the lock. Nothing is frozen.
	ErrCutoverDigestMismatch = errorx.New(errorx.KindAborted, "the source digest does not match the agent's source")
	// ErrCutoverAttemptOpen refuses a stage naming another import than
	// the agent's open attempt.
	ErrCutoverAttemptOpen = errorx.New(errorx.KindFailedPrecondition, "another cutover attempt is open for the agent")
	// ErrCutoverAttemptEnded refuses a stage of a rolled-back attempt:
	// an ended attempt is never revived.
	ErrCutoverAttemptEnded = errorx.New(errorx.KindFailedPrecondition, "the cutover attempt has been rolled back")
	// ErrCutoverAttemptNotFound answers an import the agent never froze.
	ErrCutoverAttemptNotFound = errorx.New(errorx.KindNotFound, "cutover attempt not found")
	// ErrCutoverNotEligible refuses a freeze of an agent whose
	// execution is already fenced, or whose source garam cannot read.
	ErrCutoverNotEligible = errorx.New(errorx.KindFailedPrecondition, "the agent is not eligible for cutover")
	// ErrReverseMigrationRequired refuses a rollback once the switch is
	// recorded: the Operator's source cannot return to garam, so only a
	// reverse migration restores one.
	ErrReverseMigrationRequired = errorx.New(errorx.KindFailedPrecondition,
		"the cutover is switched: rolling back requires a reverse migration")
	// ErrCutOver refuses the operator's certificate route for an agent
	// whose cutover is switched: its operator no longer owns its source.
	ErrCutOver = errorx.New(errorx.KindFailedPrecondition, "the agent has been cut over to the Operator control service")
	// ErrUnsupportedCutoverContract refuses a request carrying no
	// contract version, or another.
	ErrUnsupportedCutoverContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: this route serves "+CutoverContractVersion)
	// ErrUnsupportedTokenCutoverContract is that refusal for a request
	// a token authenticated (ADR-0100).
	ErrUnsupportedTokenCutoverContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: a request presenting a token is served under "+TokenCutoverContractVersion)
)

// ImportID is the Operator's identifier for one import of an agent's
// source, and so for one cutover attempt.
type ImportID string

// ParseImportID accepts 1 to 128 unreserved URL characters.
func ParseImportID(s string) (ImportID, error) {
	if !requestIDPattern.MatchString(s) {
		return "", errorx.New(errorx.KindInvalidArgument, "import identifier must be 1-128 unreserved characters")
	}
	return ImportID(s), nil
}

// CutoverStage is where an attempt stands.
type CutoverStage string

const (
	CutoverFrozen     CutoverStage = "frozen"
	CutoverSwitched   CutoverStage = "switched"
	CutoverRolledBack CutoverStage = "rolled_back"
)

// ParseCutoverStage reads a stored stage.
func ParseCutoverStage(s string) (CutoverStage, error) {
	switch st := CutoverStage(s); st {
	case CutoverFrozen, CutoverSwitched, CutoverRolledBack:
		return st, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "unknown cutover stage")
}

// Digest is a SHA-256, carried as lowercase hex.
type Digest [32]byte

// ParseDigest reads 64 lowercase hex characters.
func ParseDigest(s string) (Digest, error) {
	var d Digest
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(d) || strings.ToLower(s) != s {
		return Digest{}, errorx.New(errorx.KindInvalidArgument, "digest must be 64 lowercase hex characters")
	}
	copy(d[:], b)
	return d, nil
}

func (d Digest) String() string { return hex.EncodeToString(d[:]) }

// SourceDigest is the SHA-256 of the RFC 8785 JSON of {agentGrn,
// operatorGrn, values}: what the Operator's import and garam's freeze
// each compute and compare. The name is not in it, because the name
// stays garam's and stays renamable.
func SourceDigest(agentGRN, operatorGRN grn.GRN, values Values) Digest {
	var b strings.Builder
	b.WriteString(`{"agentGrn":`)
	writeCanonicalString(&b, agentGRN.String())
	b.WriteString(`,"operatorGrn":`)
	writeCanonicalString(&b, operatorGRN.String())
	b.WriteString(`,"values":{`)
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	// RFC 8785 §3.2.3 orders members by their UTF-16 code units.
	slices.SortFunc(keys, func(a, b string) int {
		return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
	})
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		writeCanonicalString(&b, k)
		b.WriteByte(':')
		writeCanonicalString(&b, values[k])
	}
	b.WriteString("}}")
	return sha256.Sum256([]byte(b.String()))
}

// ValueDigest is the SHA-256 of one value's bytes, which an inventory
// prints in place of the value.
func ValueDigest(v string) Digest { return sha256.Sum256([]byte(v)) }

// writeCanonicalString writes s as RFC 8785 §3.2.2.2 serializes a
// string: only the quote, the backslash and the control characters are
// escaped, the five with a short form in it, the rest as \u00XX in
// lowercase hex. s is valid UTF-8, as every value ParseValues admits
// is.
func writeCanonicalString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				if r < 0x10 {
					b.WriteByte('0')
				}
				b.WriteString(strconv.FormatInt(int64(r), 16))
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}

// CutoverClass is whether an agent's source can be cut over, decided
// from its execution and what garam holds as its source.
type CutoverClass string

const (
	// CutoverEligible is a legacy agent whose source garam can read: a
	// definition, or none at all.
	CutoverEligible CutoverClass = "eligible"
	// CutoverUnparseable is a legacy agent whose stored values no
	// longer parse. They are preserved and block its cutover.
	CutoverUnparseable CutoverClass = "blocked:unparseable"
	// CutoverUnclaimed is a definition no operator claimed: there is no
	// execution row to lock and nothing running to preserve.
	CutoverUnclaimed CutoverClass = "ineligible:unclaimed"
	// CutoverNotApplicable is an agent whose execution is already
	// fenced: a managed creation or a standalone host's agent from
	// birth, or one activated before this contract existed.
	CutoverNotApplicable CutoverClass = "not-applicable"
)

// ClassOf decides the class of an agent in mode whose stored source
// did or did not parse.
func ClassOf(mode Mode, parsed bool) CutoverClass {
	switch {
	case mode == ModeFenced:
		return CutoverNotApplicable
	case !parsed:
		return CutoverUnparseable
	}
	return CutoverEligible
}

// CutoverSource is the definition an agent's cutover imports, as
// garam holds it.
type CutoverSource struct {
	Operator  grn.GRN
	Values    Values
	DefinedBy grn.GRN
	CreatedAt time.Time
}

// CutoverAttempt is one attempt as recorded.
type CutoverAttempt struct {
	ImportID     ImportID
	Stage        CutoverStage
	FrozenDigest Digest
	CreatedAt    time.Time
	// SwitchedAt is set once the attempt is switched.
	SwitchedAt *time.Time
	// EndedAt is set once the attempt is rolled back.
	EndedAt *time.Time
}

// Cutover is what the Operator reads to import an agent: where it
// runs, its execution mode, its source and the digest over it, and the
// agent's most recent attempt.
type Cutover struct {
	Agent    grn.GRN
	Assignee grn.GRN
	Epoch    int
	Mode     Mode
	Class    CutoverClass
	// Source is nil for an agent with no definition, whose values are
	// empty, and for one whose stored values do not parse.
	Source *CutoverSource
	// SourceDigest is nil where the source does not parse.
	SourceDigest *Digest
	// Attempt is nil before the first freeze.
	Attempt *CutoverAttempt
}

// CutoverRequest is a cutover stage the authority admitted, as the
// store applies it.
type CutoverRequest struct {
	Agent        grn.GRN
	ImportID     ImportID
	Audience     grn.GRN
	GrantID      string
	OperationRef string
	Authority    Lifecycle
}

// FreezeRequest is a freeze the authority admitted: the epoch the
// Operator imported at and the digest of what it imported.
type FreezeRequest struct {
	CutoverRequest
	Epoch        int
	SourceDigest Digest
}

// CutoverInput is a cutover stage as received: the agent:cutover
// reference its Authorization header carries, the import it names, and
// the request target it was received at, which the reference must have
// been minted for.
type CutoverInput struct {
	Agent         grn.GRN
	ImportID      ImportID
	OperationRef  string
	RequestTarget string
}

// FreezeInput is a freeze as received.
type FreezeInput struct {
	CutoverInput
	Epoch        int
	SourceDigest Digest
}
