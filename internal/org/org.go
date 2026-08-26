// Package org owns the Organization, Group, and Project aggregates.
//
// All three live in a single package because they share a lifecycle:
// Groups and Projects are children of an Organization, and the Store
// cascade-deletes them when the parent Organization is removed.
package org

import (
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Domain sentinel errors.
var (
	ErrOrgNotFound           = errorx.New(errorx.KindNotFound, "org not found")
	ErrGroupNotFound         = errorx.New(errorx.KindNotFound, "group not found")
	ErrProjectNotFound       = errorx.New(errorx.KindNotFound, "project not found")
	ErrMemberNotFound        = errorx.New(errorx.KindNotFound, "org member not found")
	ErrGroupMemberNotFound   = errorx.New(errorx.KindNotFound, "group member not found")
	ErrProjectMemberNotFound = errorx.New(errorx.KindNotFound, "project member not found")
	ErrInvitationNotFound    = errorx.New(errorx.KindNotFound, "invitation not found")
	ErrInvitationAccepted    = errorx.New(errorx.KindFailedPrecondition, "invitation already accepted")
	ErrInvitationExpired     = errorx.New(errorx.KindFailedPrecondition, "invitation expired")
	ErrMemberAlreadyExists   = errorx.New(errorx.KindAlreadyExists, "org member already exists")
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

func (o *Organization) GRN() grn.GRN         { return o.grn }
func (o *Organization) Name() string         { return o.name }
func (o *Organization) CreatedAt() time.Time { return o.createdAt }
