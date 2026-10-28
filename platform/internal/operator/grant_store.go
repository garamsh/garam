package operator

import (
	"context"
	"time"

	"github.com/garamsh/garam/platform/internal/grn"
)

// GrantStore persists the grants an organization makes to its hosted
// operators. Grants are never updated in place: a write ends the
// current one and adds another, and ended grants remain so their
// request identifiers keep refusing replays.
type GrantStore interface {
	// WriteGrant applies w and answers the grant it leaves current, and
	// whether this call created it. A repeat of an earlier request
	// answers the grant it wrote while that grant is current, refusing
	// with ErrGrantEnded once it is not and ErrGrantRequestConflict
	// where any part of the write differs. A new request is refused with
	// ErrGrantStale unless w.Expected names the current grant, nil for
	// none. id names the grant a new request creates.
	WriteGrant(ctx context.Context, w GrantWrite, id GrantID) (*Grant, bool, error)
	// EndGrant ends the named grant of the hosted operator. A grant
	// already ended answers nil and changes nothing, so a retried
	// withdrawal never ends a later replacement; a grant the operator
	// never held answers ErrGrantNotFound.
	EndGrant(ctx context.Context, org, operator grn.GRN, id GrantID, now time.Time) error
	// CurrentGrant answers the hosted operator's current grant, expired or
	// not, or ErrGrantNotFound.
	CurrentGrant(ctx context.Context, org, operator grn.GRN) (*Grant, error)
	// GetGrant answers one grant by identifier, ended or not, with the
	// controllers still registered, or ErrGrantNotFound.
	GetGrant(ctx context.Context, id GrantID) (*Grant, error)
}
