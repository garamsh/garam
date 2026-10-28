package machine

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// TokenScheme is the Authorization scheme a request presents a token
// under (ADR-0100).
const TokenScheme = "Garam-Token"

// BearerScheme is the OAuth scheme a request may present the same
// token under instead (ADR-0102, RFC 6750).
const BearerScheme = "Bearer"

// Tokens answers what a presented token stands for. It is read on
// every request that presents one, not once per connection, because
// what it reads beside the token — the record the token rests on — is
// what deleting an operator or re-issuing its credential withdraws,
// and a connection already open must meet that on its next request
// (ADR-0100).
//
// It is a seam of the shape [Registration] is, satisfied in the
// composition root from the domains that issue tokens, so this package
// still holds no store.
type Tokens interface {
	// Resolve returns what a live token garam accepts stands for. It
	// answers a KindUnauthenticated error where no such token is live —
	// unknown, expired, withdrawn with its record, or minted for
	// another audience — and KindUnavailable where the read could not
	// be made.
	Resolve(ctx context.Context, token string) (Bearer, error)
}

// Bearer is what a live token stands for: the identity it names, and
// the token's own serial and dates. An agent's token carries a serial
// the execution fence reads as it reads a certificate's (ADR-0080,
// ADR-0100); an operator's carries none, so a route reading a credential
// has nothing to read off it.
type Bearer struct {
	Subject   grn.GRN
	Serial    string
	IssuedAt  time.Time
	ExpiresAt time.Time
}
