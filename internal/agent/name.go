package agent

import (
	"strings"
	"unicode/utf8"

	"github.com/garamsh/garam/internal/errorx"
)

// Bounds on what an organization calls an agent or a template, and on
// what it says a template is for. They are the operator record's
// bounds, so one organization's words about its resources are bounded
// alike.
const (
	NameMaxLength        = 100
	DescriptionMaxLength = 1000
)

// Name is what an organization calls an agent or a template. It is
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

// Description is what an organization says a template is for. A
// Description is what [ParseDescription] returns.
type Description string

// ParseDescription trims s and returns it as a Description, which may
// be empty, refusing one longer than DescriptionMaxLength characters.
func ParseDescription(s string) (Description, error) {
	d := strings.TrimSpace(s)
	switch {
	case utf8.RuneCountInString(d) > DescriptionMaxLength:
		return "", errorx.New(errorx.KindInvalidArgument, "agent: a description is at most 1000 characters")
	case !isStorableText(d):
		return "", errorx.New(errorx.KindInvalidArgument, "agent: a description must be utf-8 without nul bytes")
	}
	return Description(d), nil
}

// isStorableText reports whether a Postgres text column accepts s: it
// takes valid utf-8 and refuses a nul byte.
func isStorableText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
