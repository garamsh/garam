package authz

import "github.com/garamsh/garam/internal/grn"

// Subject is anything that can act: a user, agent, group, role, or
// userset (e.g. group#member).
type Subject interface {
	GRN() grn.GRN
	Type() string
	Roles() []string
	Policies() []string
}
