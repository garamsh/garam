package org

import (
	"time"

	"github.com/garamsh/garam/platform/internal/grn"
)

// Membership ties a user to an Organization with a Role. Every field
// is one the store writes and reads back, and the aggregate holds no
// invariant over any of them, so they are exported and whatever
// restores a row does it from outside this package (ADR-0069).
type Membership struct {
	Organization grn.GRN
	User         grn.GRN
	Role         Role
	CreatedAt    time.Time
}

// NewMembership constructs a Membership, stamping CreatedAt.
func NewMembership(orgGRN, userGRN grn.GRN, role Role) *Membership {
	return &Membership{Organization: orgGRN, User: userGRN, Role: role, CreatedAt: time.Now().UTC()}
}
