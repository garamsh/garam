package agent

import (
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/garamsh/garam/internal/errorx"
)

// Bounds on what an operator says about a revision. garam stores the
// words and interprets none of them, so it bounds only their size.
const (
	// ignoredKeysMax is how many ignored keys one acknowledgement
	// names: no definition carries more values than that.
	ignoredKeysMax = valuesMax
	// reasonMax is how long a rejection's reason may be, in
	// characters.
	reasonMax = 500
)

// Revision numbers the values a definition has held: the first set is
// revision 1, and every save raises it by one (ADR-0076). It names a
// saved set and says nothing about whether any operator applied it.
type Revision int

// ParseRevision returns n as a Revision, or refuses one below 1.
func ParseRevision(n int) (Revision, error) {
	if n < 1 {
		return 0, errorx.New(errorx.KindInvalidArgument, "agent: revision must be at least 1")
	}
	return Revision(n), nil
}

// ConfigurationOutcome is what an operator says it did with a
// revision: [ConfigurationApplied] or [ConfigurationRejected], and
// nothing else implements it.
type ConfigurationOutcome interface {
	configurationOutcome()
}

// ConfigurationApplied is an operator saying it applied a revision,
// except for the keys it names as ignored. An empty list is still the
// operator's word and not something garam verified (ADR-0076).
type ConfigurationApplied struct {
	// IgnoredKeys are the revision's keys the operator did not apply,
	// sorted. It is empty rather than nil where the operator named
	// none.
	IgnoredKeys []string
}

// ConfigurationRejected is an operator saying it refused a revision,
// and why.
type ConfigurationRejected struct {
	Reason RejectionReason
}

func (ConfigurationApplied) configurationOutcome()  {}
func (ConfigurationRejected) configurationOutcome() {}

// NewConfigurationApplied returns an application ignoring the keys
// named, or refuses a list garam would not store: more keys than a
// definition holds, a key no definition could carry, or one named
// twice.
func NewConfigurationApplied(ignored []string) (ConfigurationApplied, error) {
	if len(ignored) > ignoredKeysMax {
		return ConfigurationApplied{}, errorx.New(errorx.KindInvalidArgument, "agent: too many ignored keys")
	}
	keys := slices.Clone(ignored)
	if keys == nil {
		keys = []string{}
	}
	slices.Sort(keys)
	for i, k := range keys {
		if !isValueKey(k) {
			return ConfigurationApplied{}, errorx.New(errorx.KindInvalidArgument, "agent: invalid ignored key")
		}
		if i > 0 && keys[i-1] == k {
			return ConfigurationApplied{}, errorx.New(errorx.KindInvalidArgument, "agent: ignored key named twice")
		}
	}
	return ConfigurationApplied{IgnoredKeys: keys}, nil
}

// RejectionReason is why an operator refused a revision, in its own
// words: printable text of 1 to reasonMax characters.
type RejectionReason string

// ParseRejectionReason returns s as a RejectionReason, or refuses one
// that is blank, too long, not UTF-8, or carries a control character.
func ParseRejectionReason(s string) (RejectionReason, error) {
	if strings.TrimSpace(s) == "" {
		return "", errorx.New(errorx.KindInvalidArgument, "agent: rejection reason is empty")
	}
	if !utf8.ValidString(s) {
		return "", errorx.New(errorx.KindInvalidArgument, "agent: rejection reason is not valid utf-8")
	}
	if utf8.RuneCountInString(s) > reasonMax {
		return "", errorx.New(errorx.KindInvalidArgument, "agent: rejection reason too long")
	}
	if strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", errorx.New(errorx.KindInvalidArgument, "agent: rejection reason carries a control character")
	}
	return RejectionReason(s), nil
}

// ConfigurationReport is the last acknowledgement garam accepted from
// the operator holding an agent: the epoch that operator held it at,
// the revision it names and what it did with it.
//
// It describes the current assignment only while Epoch is that
// assignment's epoch; after a reassignment it is the previous
// operator's claim. ReportedAt is when garam received it, not when the
// operator acted, and a later report of the same revision replaces it
// in the order garam receives them (ADR-0076).
type ConfigurationReport struct {
	Epoch      int
	Revision   Revision
	Outcome    ConfigurationOutcome
	ReportedAt time.Time
}
