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

// ParseName trims s and returns it as a Name, refusing text a Postgres
// column cannot hold, then one left empty, then one longer than
// NameMaxLength characters: the order, and the wording, an operator's
// name is refused in (operator.NewDetailsPatch).
func ParseName(s string) (Name, error) {
	n := strings.TrimSpace(s)
	switch {
	case !isStorableText(n):
		return "", errorx.New(errorx.KindInvalidArgument, "an agent's name must be UTF-8 text without NUL characters")
	case n == "":
		return "", errorx.New(errorx.KindInvalidArgument, "an agent's name must not be empty")
	case utf8.RuneCountInString(n) > NameMaxLength:
		return "", errorx.New(errorx.KindInvalidArgument, "an agent's name is at most 100 characters")
	}
	return Name(n), nil
}

// sameName reports whether two optional names are the same request:
// both absent, or both present and equal.
func sameName(a, b *Name) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// isStorableText reports whether a Postgres text column accepts s: it
// takes valid utf-8 and refuses a nul byte.
func isStorableText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
