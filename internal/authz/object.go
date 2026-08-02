package authz

import "github.com/garamsh/gaon/internal/grn"

// Object is anything a subject can act on.
type Object interface {
	GRN() grn.GRN
	Type() string
}
