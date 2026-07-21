package org

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Project is a resource container that belongs to a single Organization.
type Project struct {
	grn       grn.GRN
	name      string
	orgGRN    grn.GRN
	roles     []string
	policies  []string
	members   []grn.GRN
	createdAt time.Time
	updatedAt time.Time
}

// NewProject constructs a Project tied to the given Organization GRN.
func NewProject(g grn.GRN, name string, orgGRN grn.GRN) *Project {
	now := time.Now().UTC()
	return &Project{grn: g, name: name, orgGRN: orgGRN, createdAt: now, updatedAt: now}
}

func (p *Project) GRN() grn.GRN          { return p.grn }
func (p *Project) Name() string          { return p.name }
func (p *Project) Organization() grn.GRN { return p.orgGRN }
func (p *Project) Roles() []string       { return p.roles }
func (p *Project) Policies() []string    { return p.policies }
func (p *Project) Members() []grn.GRN    { return p.members }

// Kind reports the resource kind for authz purposes.
func (p *Project) Kind() string { return ServiceTypeProject }
