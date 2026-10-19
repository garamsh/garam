package operator

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// TokenStore records the tokens an operator exchanges its key for, and
// answers what a presented one stands for (ADR-0100, ADR-0102).
//
// It is separate from the stores beside it because it serves the
// machine listener on every request rather than a person or a
// renewal. Issuing and withdrawing a key are [Store]'s, on the
// browser-facing listener, because a member does them.
type TokenStore interface {
	// ResolveToken answers what the live token tokenHash names, read
	// joined to the registration it rests on, or ErrTokenInvalid where
	// none is live.
	ResolveToken(ctx context.Context, tokenHash string) (TokenHolder, error)
	// VerificationKey answers the live key kid names within client's
	// registration, or ErrClientInvalid where none is (ADR-0102).
	VerificationKey(ctx context.Context, client grn.GRN, kid string) (Key, error)
	// RecordAssertionToken admits a verified assertion and stores t's
	// hash under its key, in one transaction holding the key's row: it
	// rereads that the key is live, checks the assertion's dates at
	// the database's time, spends its (client, jti), stamps t's dates
	// from that time, keeps the key's newest LiveTokensPerKey,
	// sweeps the operator's expired tokens, and marks the key
	// exchanged. A key withdrawn meanwhile, an untimely
	// assertion and a spent jti each answer ErrClientInvalid and
	// record nothing; an audience nobody registered answers
	// ErrAudienceInvalid.
	RecordAssertionToken(ctx context.Context, a VerifiedAssertion, t *Token) error
}
