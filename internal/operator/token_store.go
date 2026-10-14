package operator

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// TokenStore records the tokens an operator exchanges its credential
// for, and answers what a presented one stands for (ADR-0100).
//
// It is separate from the stores beside it because it serves the
// machine listener on every request rather than a person or a
// renewal. Issuing and re-issuing the credential are [Store]'s, on the
// browser-facing listener, because a member does them.
type TokenStore interface {
	// CredentialHolder answers the operator credentialHash belongs to,
	// read beside its registration, or ErrCredentialInvalid where no
	// registered operator holds that credential.
	CredentialHolder(ctx context.Context, credentialHash string) (CredentialHolder, error)
	// RecordToken stores t's hash under the credential issuance holder
	// names, sweeping the operator's expired tokens and keeping that
	// issuance's newest LiveTokensPerCredential. It answers
	// ErrCredentialInvalid where that issuance has been re-issued or
	// withdrawn since it was read.
	RecordToken(ctx context.Context, holder CredentialHolder, t *Token) error
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
	// from that time, sweeps and trims as RecordToken does, and marks
	// the key exchanged. A key withdrawn meanwhile, an untimely
	// assertion and a spent jti each answer ErrClientInvalid and
	// record nothing; an audience nobody registered answers
	// ErrAudienceInvalid.
	RecordAssertionToken(ctx context.Context, a VerifiedAssertion, t *Token) error
}
