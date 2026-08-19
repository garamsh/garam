// Package listfilter holds the filter a list call is scoped by. Org
// and agent both page their lists with it, and a domain may not import
// a sibling to borrow the type, so it lives here once.
package listfilter

// Filter scopes a List call. Limit of 0 means "no limit".
type Filter struct {
	Offset int
	Limit  int
}
