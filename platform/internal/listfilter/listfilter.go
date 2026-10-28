// Package listfilter holds the filter a list call is scoped by, and
// parses the paging parameters a request carries into one, and the
// text a search narrows a list by.
package listfilter

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/garamsh/garam/internal/errorx"
)

// Filter scopes a List call. Limit of 0 means "no limit".
type Filter struct {
	Offset int
	Limit  int
}

// Parse builds a Filter from the limit and offset a caller supplied,
// each nil where the caller sent none.
//
// It refuses a value the `integer` paging columns cannot hold, which
// SQL would otherwise truncate rather than reject. This is the boundary
// the values cross (runtime-safety.md §Trust boundaries), so it is the
// one place they are checked: a Filter from here is within range, and
// the stores convert it without checking.
func Parse(limit, offset *int) (Filter, error) {
	l, err := paging("limit", limit)
	if err != nil {
		return Filter{}, err
	}
	o, err := paging("offset", offset)
	if err != nil {
		return Filter{}, err
	}
	return Filter{Limit: l, Offset: o}, nil
}

// paging returns the value a caller supplied, or 0 where it supplied
// none, refusing one outside what the column holds. The refusal names
// the parameter and the range, and never the value it was given: a
// message built from what was submitted repeats it back.
func paging(name string, v *int) (int, error) {
	if v == nil {
		return 0, nil
	}
	if *v < 0 || *v > math.MaxInt32 {
		return 0, errorx.New(errorx.KindOutOfRange,
			name+" must be between 0 and "+strconv.Itoa(math.MaxInt32))
	}
	return *v, nil
}

// NeedleMaxLength bounds the text a search matches, in characters.
const NeedleMaxLength = 100

// Needle is the text a search finds names and identifiers by: trimmed,
// and from 1 to NeedleMaxLength characters.
type Needle struct {
	text string
}

// ParseNeedle builds a Needle from the text a caller typed, refusing
// one that is blank or longer than NeedleMaxLength once trimmed. Like
// paging, the refusal never repeats what it was given.
func ParseNeedle(q string) (Needle, error) {
	text := strings.TrimSpace(q)
	if text == "" || utf8.RuneCountInString(text) > NeedleMaxLength {
		return Needle{}, errorx.New(errorx.KindInvalidArgument,
			"q must hold 1 to "+strconv.Itoa(NeedleMaxLength)+" characters")
	}
	return Needle{text: text}, nil
}

// Text answers the needle as typed, trimmed.
func (n Needle) Text() string {
	return n.text
}
