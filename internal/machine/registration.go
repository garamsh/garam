package machine

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// Registration is the organization's record of the identity a
// presented certificate names, as what admits that certificate reads
// it. A certificate proves which identity is calling; this answers
// whether the organization still says that identity is theirs, and
// withdrawing the record is what stops the certificate authenticating
// (ADR-0070).
//
// It is a seam of the same shape as [TrustRoot] and is asked beside
// it, once per connection. What it costs is one indexed read on a path
// that already takes one, which is why the reversal ADR-0014's
// "revocation is by lifetime" needed no list, no responder and no
// stored serial.
type Registration interface {
	// Holds returns nil where the organization still holds a record of
	// subject, and a KindNotFound error where it holds none.
	//
	// An identity garam records no registration for is held rather
	// than refused. This answers for the identities a record admits,
	// and an agent is not one: it is admitted by the claim its
	// operator made (ADR-0033), on a certificate this decision leaves
	// alone.
	Holds(ctx context.Context, subject grn.GRN) error
}
