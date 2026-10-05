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
	ErrMemberOfAnotherOrg    = errorx.New(errorx.KindFailedPrecondition, "member is owned by another org")
)

// checkMemberOrg refuses a Group or Project member that another org
// owns. An org-owned GRN names its org in the organization segment
// (ADR-0001), so the two segments must agree; a root-owned GRN, such as
// a user's, exists above every org and is admitted.
func checkMemberOrg(container, member grn.GRN) error {
	if member.Organization != grn.RootOwner && member.Organization != container.Organization {
		return ErrMemberOfAnotherOrg
	}
	return nil
}

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
func (o *Organization) UpdatedAt() time.Time { return o.updatedAt }

// Contents counts what this domain holds for one Organization that
// deleting that Organization removes. It is a count and not a listing
// because the caller is stating a consequence rather than reading the
// rows: a number is what survives a page that a listing does not.
//
// It covers this domain alone. The cascade reaches further, into rows
// the agents, operators and messaging domains own, and each of those
// answers for its own.
type Contents struct {
	Groups      int
	Projects    int
	Members     int
	Invitations int
}
