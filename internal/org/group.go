package org

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Group is a member container that belongs to a single Organization.
type Group struct {
	grn       grn.GRN
	name      string
	orgGRN    grn.GRN
	roles     []string
	policies  []string
	createdAt time.Time
	updatedAt time.Time
}

// NewGroup constructs a Group tied to the given Organization GRN. The
// optional roles are the ones the Group grants its members.
func NewGroup(g grn.GRN, name string, orgGRN grn.GRN, roles ...string) *Group {
	now := time.Now().UTC()
	return &Group{grn: g, name: name, orgGRN: orgGRN, roles: roles, createdAt: now, updatedAt: now}
}

func (g *Group) GRN() grn.GRN          { return g.grn }
func (g *Group) Name() string          { return g.name }
func (g *Group) Organization() grn.GRN { return g.orgGRN }
func (g *Group) Roles() []string       { return g.roles }
func (g *Group) Policies() []string    { return g.policies }
func (g *Group) CreatedAt() time.Time  { return g.createdAt }
func (g *Group) UpdatedAt() time.Time  { return g.updatedAt }
