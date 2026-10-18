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

// The reasons an invitation refusal carries (api/openapi.yaml
// `Error.reason`): both are failed_precondition, and only the reason
// tells a used invitation from one that lapsed.
const (
	ReasonInvitationAccepted = "invitation_accepted"
	ReasonInvitationExpired  = "invitation_expired"
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
	ErrInvitationAccepted    = errorx.NewWithReason(errorx.KindFailedPrecondition, errorx.Reason{Code: ReasonInvitationAccepted}, "invitation already accepted")
	ErrInvitationExpired     = errorx.NewWithReason(errorx.KindFailedPrecondition, errorx.Reason{Code: ReasonInvitationExpired}, "invitation expired")
	ErrMemberAlreadyExists   = errorx.New(errorx.KindAlreadyExists, "org member already exists")
	ErrMemberOfAnotherOrg    = errorx.New(errorx.KindFailedPrecondition, "member is owned by another org")
)

// CheckMemberOrg refuses a Group or Project member that another org
// owns. An org-owned GRN names its org in the organization segment
// (ADR-0001), so the two segments must agree; a root-owned GRN, such as
// a user's, exists above every org and is admitted.
func CheckMemberOrg(container, member grn.GRN) error {
	if member.Organization != grn.RootOwner && member.Organization != container.Organization {
		return ErrMemberOfAnotherOrg
	}
	return nil
}

// ServiceType* are the canonical GRN service-type prefixes used by
// this package.
const (
	ServiceTypeGroup   = "group"
	ServiceTypeProject = "project"
)

// Organization is the top-level tenancy boundary. Every field is one
// the store writes and reads back, and the aggregate holds no invariant
// over any of them, so they are exported and whatever restores a row
// does it from outside this package (ADR-0069).
type Organization struct {
	GRN       grn.GRN
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewOrganization constructs an Organization with the given GRN and
// name, stamping CreatedAt and UpdatedAt from one instant.
func NewOrganization(g grn.GRN, name string) *Organization {
	now := time.Now().UTC()
	return &Organization{GRN: g, Name: name, CreatedAt: now, UpdatedAt: now}
}

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
