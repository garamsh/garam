// Package policyfeed switches the platform's feed of the policy
// service on and off (ADR-0140). What the feed holds, and the triggers
// that fill it, are migration 000067's.
package policyfeed

import "context"

// Switch turns the feed on and off.
type Switch interface {
	// Start enables the feed and marks every live entity, in one
	// transaction that waits out the writers of the source tables. It
	// returns how many entities the outbox then holds.
	Start(ctx context.Context) (marked int64, err error)
	// Stop disables the feed and empties the outbox. It returns how
	// many marks it removed.
	Stop(ctx context.Context) (removed int64, err error)
}
