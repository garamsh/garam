package org

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Membership ties a user to an Organization with a Role.
type Membership struct {
	orgGRN    grn.GRN
	userGRN   grn.GRN
	role      Role
	createdAt time.Time
}

// NewMembership constructs a Membership.
func NewMembership(orgGRN, userGRN grn.GRN, role Role) *Membership {
	return &Membership{orgGRN: orgGRN, userGRN: userGRN, role: role, createdAt: time.Now().UTC()}
}

func (m *Membership) Organization() grn.GRN { return m.orgGRN }
func (m *Membership) User() grn.GRN         { return m.userGRN }
func (m *Membership) Role() Role            { return m.role }
func (m *Membership) CreatedAt() time.Time  { return m.createdAt }
