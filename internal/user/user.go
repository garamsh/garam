// Package user owns the User aggregate.
//
// A User is identified by its GRN and may belong to multiple
// Organizations. Roles and Policies are stored directly on the User
// for fast lookup; group-derived access is resolved at check time.
package user

import (
	"time"

	"github.com/garamsh/gaon/internal/grn"
)

// ServiceType is the canonical GRN service-type for users.
const ServiceType = "user"

// User is a human account.
type User struct {
	grn           grn.GRN
	nickname      string
	organizations []grn.GRN
	roles         []string
	policies      []string
	createdAt     time.Time
	updatedAt     time.Time
}

// New constructs a User.
func New(g grn.GRN, nickname string) *User {
	now := time.Now().UTC()
	return &User{grn: g, nickname: nickname, createdAt: now, updatedAt: now}
}

func (u *User) GRN() grn.GRN             { return u.grn }
func (u *User) Nickname() string         { return u.nickname }
func (u *User) Organizations() []grn.GRN { return u.organizations }
func (u *User) Roles() []string          { return u.roles }
func (u *User) Policies() []string       { return u.policies }

// Kind reports the resource kind for authz purposes.
func (u *User) Kind() string { return ServiceType }
