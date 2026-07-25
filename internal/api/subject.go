package api

import (
	"github.com/garamsh/gaon/internal/authz"
	"github.com/garamsh/gaon/internal/grn"
	"github.com/garamsh/gaon/internal/user"
)

// subject adapts a user to the authz.Subject seam. Compile-time
// checked below; domain types stay free of authz methods (P1 keeps
// the adapter pattern).
type subject struct {
	u *user.User
}

var _ authz.Subject = subject{}

func (s subject) GRN() grn.GRN       { return s.u.GRN() }
func (s subject) Type() string       { return user.ServiceType }
func (s subject) Roles() []string    { return s.u.Roles() }
func (s subject) Policies() []string { return s.u.Policies() }

// object adapts a GRN to the authz.Object seam.
type object struct {
	g grn.GRN
}

var _ authz.Object = object{}

func (o object) GRN() grn.GRN { return o.g }
func (o object) Type() string { return o.g.ServiceType }
