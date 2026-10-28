// Package page is cowork's page tree: an organization's pages, each
// under at most one parent and ordered among its siblings (ADR-0127).
// A page's content is its Yjs document, which package collab keeps;
// this package keeps what names and places the page.
package page

import (
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// ServiceType is a page's GRN service type, the kind the platform's
// permission checks name it by (ADR-0127).
const ServiceType = "cowork-page"

// Page is one page of an organization's tree. Every field is one the
// store writes and reads back, so they are exported and the store
// restores a row from outside this package.
type Page struct {
	ID string
	// Org is the identifier of the organization that owns the page.
	Org string
	// Parent is the ID of the page this one sits under, empty at the
	// top of the tree.
	Parent string
	Title  string
	// Position orders the page among its siblings, lowest first.
	Position  int
	CreatedBy grn.GRN
	CreatedAt time.Time
	UpdatedAt time.Time
}

// GRN is the page's resource name under its organization (ADR-0001).
func (p Page) GRN() grn.GRN {
	return grn.New(p.Org, grn.DefaultProject, ServiceType, p.ID)
}

// CreateInput is a new page: its title, and the page it goes under,
// empty for the top of the tree. It goes after its last sibling.
type CreateInput struct {
	Title  string
	Parent string
}

// UpdateInput changes what it sets and leaves the rest. Parent set to
// an empty string moves the page to the top of the tree. Position is
// its place among its new siblings, and placing it there moves the
// siblings from that place on down by one.
type UpdateInput struct {
	Title    *string
	Parent   *string
	Position *int
}

// maxTitle is the longest title a page takes, in bytes.
const maxTitle = 500

var (
	ErrPageNotFound = errorx.New(errorx.KindNotFound, "page not found")
	// ErrPageCycle is a move that would put a page under itself or
	// one of its descendants.
	ErrPageCycle        = errorx.New(errorx.KindFailedPrecondition, "a page cannot go under itself or a page beneath it")
	ErrTitleTooLong     = errorx.New(errorx.KindInvalidArgument, "a page title is at most 500 bytes")
	ErrPositionNegative = errorx.New(errorx.KindInvalidArgument, "a position is zero or more")
)
