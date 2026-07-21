// Package org owns the Organization, Group, and Project aggregates.
//
// All three live in a single package because they share a lifecycle:
// Groups and Projects are children of an Organization, and the Store
// cascade-deletes them when the parent Organization is removed.
package org

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// ServiceType* are the canonical GRN service-type prefixes used by
// this package.
const (
	ServiceTypeOrg     = "org"
	ServiceTypeGroup   = "group"
	ServiceTypeProject = "project"
)

// Organization is the top-level tenancy boundary.
type Organization struct {
	grn       grn.GRN
	name      string
	createdAt time.Time
	updatedAt time.Time
}

// NewOrganization constructs an Organization with the given GRN and name.
func NewOrganization(g grn.GRN, name string) *Organization {
	now := time.Now().UTC()
	return &Organization{grn: g, name: name, createdAt: now, updatedAt: now}
}

func (o *Organization) GRN() grn.GRN { return o.grn }
func (o *Organization) Name() string { return o.name }

// Kind reports the resource kind for authz purposes.
func (o *Organization) Kind() string { return ServiceTypeOrg }
