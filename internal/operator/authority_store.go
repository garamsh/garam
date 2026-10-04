package operator

import (
	"context"
	"time"
)

// AuthorityStore persists operation authorities: one row per logical
// operation, carrying the hash of its latest short token.
type AuthorityStore interface {
	// MintAuthority records b under its request identifier with a new token, or,
	// where the identifier already holds the same operation, replaces
	// that operation's token and answers its reference. Any other
	// binding under the identifier is ErrAuthorityRequestConflict.
	MintAuthority(ctx context.Context, b Binding, tokenHash string, expiresAt, now time.Time) (OperationRef, error)
	// AuthorityByToken answers the binding a token hash names, or
	// ErrAuthorityNotFound. Expiry is the caller's to judge.
	AuthorityByToken(ctx context.Context, tokenHash string) (Introspected, error)
	// AuthorityByRef answers the binding a reference names, or
	// ErrAuthorityNotFound.
	AuthorityByRef(ctx context.Context, ref OperationRef) (Binding, error)
}
