// Package user owns the User aggregate.
//
// A User is identified by its GRN. Org membership is recorded in the
// org domain (Membership); the User aggregate does not duplicate it.
// Roles are stored directly on the User for fast lookup; the api
// server collects them, along with group- and project-derived access,
// at check time. Policies are stored but not yet evaluated: attaching
// a policy to a single subject is future work (ADR-0007).
package user

import (
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ServiceType is the canonical GRN service-type for users.
const ServiceType = "user"

// ErrUserNotFound is returned when a user lookup misses.
var ErrUserNotFound = errorx.New(errorx.KindNotFound, "user not found")

// ErrUserReferenced is a delete another row refuses. The user is there
// and stays there, which is why this is not ErrUserNotFound: a caller
// told the row is missing has nothing to act on, where one told it is
// referenced can remove what references it and ask again. That is the
// state KindFailedPrecondition names — the message is not wrong, the
// system is not in the state it needs.
//
// It names no table and no constraint. What blocks the delete today is
// an invitation the user created or accepted, and naming that would put
// the schema in an answer a caller reads (#733).
var ErrUserReferenced = errorx.New(errorx.KindFailedPrecondition, "user is referenced by another record")

// User is a human account.
type User struct {
	grn       grn.GRN
	nickname  string
	roles     []string
	policies  []string
	createdAt time.Time
	updatedAt time.Time
}

// New constructs a User. The optional roles are the ones granted
// directly to the User.
func New(g grn.GRN, nickname string, roles ...string) *User {
	now := time.Now().UTC()
	return &User{grn: g, nickname: nickname, roles: roles, createdAt: now, updatedAt: now}
}

func (u *User) GRN() grn.GRN       { return u.grn }
func (u *User) Nickname() string   { return u.nickname }
func (u *User) Roles() []string    { return u.roles }
func (u *User) Policies() []string { return u.policies }
