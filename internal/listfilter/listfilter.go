// Package listfilter holds the filter a list call is scoped by, and
// parses the paging parameters a request carries into one.
package listfilter

import (
	"math"
	"strconv"

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
