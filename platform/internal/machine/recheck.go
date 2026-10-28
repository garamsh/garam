package machine

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/errorx"
)

// Recheck reads again, for one decision, what admitted an identity when
// its request was read: its token's dates, and the organization's
// record of it where one admits it (ADR-0070). A lifecycle decision
// asks it so a token that expired or a registration withdrawn since
// the request began is refused there too.
type Recheck struct {
	registrations Registration
	now           func() time.Time
}

// NewRecheck returns a Recheck over registrations. now defaults to
// time.Now.
func NewRecheck(registrations Registration, now func() time.Time) *Recheck {
	if now == nil {
		now = time.Now
	}
	return &Recheck{registrations: registrations, now: now}
}

// Recheck answers nil where identity still holds: ErrOutsideValidity
// outside its token's dates, a KindNotFound error where its record is
// withdrawn, and KindUnavailable where the record could not be read.
func (c *Recheck) Recheck(ctx context.Context, identity Identity) error {
	now := c.now()
	if now.Before(identity.notBefore) || now.After(identity.notAfter) {
		return ErrOutsideValidity
	}
	err := c.registrations.Holds(ctx, identity.grn)
	if err == nil || errorx.IsKind(err, errorx.KindNotFound) {
		return err
	}
	return errorx.Wrap(err, "machine: registration", errorx.KindUnavailable)
}
