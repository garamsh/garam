package message

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// ConsentStore is the persistence of the consent relation. The two
// surfaces that read it never share a process (ADR-0017): the machine
// listener asks whether a post may cross, and the api server grants and
// withdraws an organization's own half.
type ConsentStore interface {
	// Grant stands org's half of its pair with other and answers with
	// the relation as it now stands. Granting a half that already
	// stands only re-stamps it.
	Grant(ctx context.Context, org, other grn.GRN) (*Consent, error)
	// Withdraw clears org's own half and leaves the other's standing:
	// the relation falls, and the half still up is what a later
	// re-grant joins. Withdrawing a half already down changes nothing.
	Withdraw(ctx context.Context, org, other grn.GRN) error
	// Stands reports whether both halves of the pair stand right now.
	// A pair nobody ever granted and a half-withdrawn one are the same
	// answer.
	Stands(ctx context.Context, a, b grn.GRN) (bool, error)
	// List reads every relation org is a side of, standing or
	// half-withdrawn.
	List(ctx context.Context, org grn.GRN) ([]*Consent, error)
}
