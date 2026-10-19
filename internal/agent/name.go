package agent

import (
	"strings"
	"unicode/utf8"

	"github.com/garamsh/garam/internal/errorx"
)

// NameMaxLength bounds what an organization calls an agent. It is the
// operator record's bound, so one organization's words about its
// resources are bounded alike.
const NameMaxLength = 100

// Name is what an organization calls an agent. It is
// separate from the GRN: a name may change and may repeat, and the GRN
// does neither. A Name is what [ParseName] returns.
type Name string

// ParseName trims s and returns it as a Name, refusing one left empty
// or longer than NameMaxLength characters.
func ParseName(s string) (Name, error) {
	n := strings.TrimSpace(s)
	switch {
	case n == "":
		return "", errorx.New(errorx.KindInvalidArgument, "agent: a name must not be empty")
	case utf8.RuneCountInString(n) > NameMaxLength:
		return "", errorx.New(errorx.KindInvalidArgument, "agent: a name is at most 100 characters")
	case !isStorableText(n):
		return "", errorx.New(errorx.KindInvalidArgument, "agent: a name must be utf-8 without nul bytes")
	}
	return Name(n), nil
}

// isStorableText reports whether a Postgres text column accepts s: it
// takes valid utf-8 and refuses a nul byte.
func isStorableText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
