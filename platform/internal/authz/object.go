package authz

import "github.com/garamsh/garam/lib/grn"

// Object is anything a subject can act on.
type Object interface {
	GRN() grn.GRN
}
