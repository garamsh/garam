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

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
)

// ServiceType is the canonical GRN service-type for users.
const ServiceType = "user"

// ErrUserNotFound is returned when a user lookup misses.
var ErrUserNotFound = errorx.New(errorx.KindNotFound, "user not found")

// ErrUserReferenced is a delete another row refuses. The user is there
// and stays there, which is why this is not ErrUserNotFound: a caller
// told the row is referenced can remove what references it and ask
// again. It names no table and no constraint, which would put the
// schema in an answer a caller reads (#733).
var ErrUserReferenced = errorx.New(errorx.KindFailedPrecondition, "user is referenced by another record")

// User is a human account. Every field is one the store writes and
// reads back, and the aggregate holds no invariant over any of them,
// so they are exported and whatever restores a row does it from
// outside this package (ADR-0069).
type User struct {
	GRN       grn.GRN
	Nickname  string
	Roles     []string
	Policies  []string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// New constructs a User, stamping CreatedAt and UpdatedAt from one
// instant. The optional roles are the ones granted directly to the
// User.
func New(g grn.GRN, nickname string, roles ...string) *User {
	now := time.Now().UTC()
	return &User{GRN: g, Nickname: nickname, Roles: roles, CreatedAt: now, UpdatedAt: now}
}
