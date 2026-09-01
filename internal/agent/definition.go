package agent

import (
	"time"
	"unicode/utf8"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Bounds on a definition's values. What the payload means belongs to
// the operator that reads it (ADR-0026 leaves the shape undecided), so
// garam bounds the size it stores and reads nothing else into it.
const (
	// valuesMax is how many entries a definition carries.
	valuesMax = 32
	// valueMax is how long one value may be, in bytes.
	valueMax = 4096
	// keyMax is how long a key may be. It is the GRN segment length,
	// because a key follows the same character rule (grn-model.md
	// §identifier) and nothing here wants a second one.
	keyMax = 64
)

// Values are what an operator constructs an agent from: keys garam
// does not interpret, holding strings garam does not interpret. A
// Values is what [ParseValues] returns, so a value of this type is
// already within the bounds above.
type Values map[string]string

// ParseValues returns the values a definition may carry, or the reason
// they are refused. Keys follow the GRN segment rule, which keeps a
// key printable wherever it is carried; a NUL byte is refused because
// jsonb stores none, and a value carrying one would fail on the way to
// the row rather than here.
func ParseValues(in map[string]string) (Values, error) {
	if len(in) > valuesMax {
		return nil, errorx.New(errorx.KindInvalidArgument, "agent: too many values")
	}
	out := make(Values, len(in))
	for k, v := range in {
		if !isValueKey(k) {
			return nil, errorx.New(errorx.KindInvalidArgument, "agent: invalid value key")
		}
		if len(v) > valueMax {
			return nil, errorx.New(errorx.KindInvalidArgument, "agent: value too long")
		}
		if !utf8.ValidString(v) {
			return nil, errorx.New(errorx.KindInvalidArgument, "agent: value is not valid utf-8")
		}
		for i := 0; i < len(v); i++ {
			if v[i] == 0 {
				return nil, errorx.New(errorx.KindInvalidArgument, "agent: value carries a nul byte")
			}
		}
		out[k] = v
	}
	return out, nil
}

// isValueKey reports whether k is a key ParseValues accepts: one to
// keyMax characters of lowercase letters, digits and '-'. Every
// character in the set is one ASCII byte, so any other byte fails the
// loop and the byte length is the character count.
func isValueKey(k string) bool {
	if k == "" || len(k) > keyMax {
		return false
	}
	for i := 0; i < len(k); i++ {
		switch c := k[i]; {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
		default:
			return false
		}
	}
	return true
}

// Definition is what an organization says an agent should be: which
// operator is to run it, and the values that operator constructs it
// from (ADR-0026).
//
// It is a record and not a command. garam does not learn from it that
// an agent exists — an operator reports that separately, and until it
// does, the agent GRN a definition names answers nothing on the agent
// paths. A definition is written once: garam cannot tell whether an
// operator has already acted on one, and the epoch that makes
// reassignment safe fences a holder that presents it (ADR-0012), which
// nothing reading a definition does yet.
type Definition struct {
	agentGRN    grn.GRN
	orgGRN      grn.GRN
	operatorGRN grn.GRN
	values      Values
	definedBy   grn.GRN
	createdAt   time.Time
}

// NewDefinition mints the identity of the agent orgGRN is defining and
// records what it should be. definedBy is the member that defined it.
//
// It fails unless operatorGRN names an operator of that organization:
// an operator holds one organization's certificate (ADR-0015), so a
// definition naming another organization's operator names one that can
// never read it. Whether the organization registered that operator is
// a read this constructor does not make; the caller answers it against
// internal/operator before the definition is stored (ADR-0045).
func NewDefinition(orgGRN, operatorGRN grn.GRN, values Values, definedBy grn.GRN) (*Definition, error) {
	g, err := NewGRN(orgGRN)
	if err != nil {
		return nil, err
	}
	if err := validateOperator(operatorGRN, orgGRN); err != nil {
		return nil, err
	}
	// A definition naming no value carries an empty payload rather than
	// none: what is stored is an object either way.
	if values == nil {
		values = Values{}
	}
	return &Definition{
		agentGRN:    g,
		orgGRN:      orgGRN,
		operatorGRN: operatorGRN,
		values:      values,
		definedBy:   definedBy,
		createdAt:   time.Now().UTC(),
	}, nil
}

// validateOperator returns nil if operatorGRN names an operator of the
// organization orgGRN identifies.
//
// It validates the GRN itself, not only the two segments below: the
// identifier is answered back in the refusal a definition naming an
// unregistered operator gets (ADR-0045), and a segment that never met
// the character rule is not text to echo.
func validateOperator(operatorGRN, orgGRN grn.GRN) error {
	if err := operatorGRN.Validate(); err != nil {
		return errorx.Wrap(err, "agent: operator", errorx.KindInvalidArgument)
	}
	switch {
	case operatorGRN.ServiceType != ServiceTypeOperator:
		return errorx.New(errorx.KindInvalidArgument, "agent: grn does not name an operator")
	case operatorGRN.Organization != orgGRN.Identifier:
		return errorx.New(errorx.KindInvalidArgument, "agent: operator belongs to another organization")
	}
	return nil
}

// Agent returns the GRN of the agent this definition describes. No
// agent exists under it until the operator claims it.
func (d *Definition) Agent() grn.GRN        { return d.agentGRN }
func (d *Definition) Organization() grn.GRN { return d.orgGRN }
func (d *Definition) Operator() grn.GRN     { return d.operatorGRN }
func (d *Definition) Values() Values        { return d.values }

// DefinedBy returns the member that defined the agent.
func (d *Definition) DefinedBy() grn.GRN   { return d.definedBy }
func (d *Definition) CreatedAt() time.Time { return d.createdAt }

// DefinitionState is a definition together with its claim state: the
// assignment the claim wrote, or none while it is unclaimed
// (ADR-0033). The only claimant a definition admits is the operator it
// names, so a claim here always binds that operator.
type DefinitionState struct {
	definition *Definition
	claim      *Assignment
}

// NewDefinitionState pairs a definition with the assignment claiming
// it, which is nil before any claim.
func NewDefinitionState(d *Definition, claim *Assignment) *DefinitionState {
	return &DefinitionState{definition: d, claim: claim}
}

func (s *DefinitionState) Definition() *Definition { return s.definition }

// Claim returns the assignment the claim wrote, or nil while the
// definition is unclaimed.
func (s *DefinitionState) Claim() *Assignment { return s.claim }
