// Package listfilter holds the filter a list call is scoped by. Org,
// agent and user page their lists with it, and a concern three
// boundaries share sits below them rather than inside one
// (docs/architecture/structure.md §When a new boundary is earned).
package listfilter

// Filter scopes a List call. Limit of 0 means "no limit".
type Filter struct {
	Offset int
	Limit  int
}
