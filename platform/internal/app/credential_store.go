package app

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// CredentialStore keeps what an instance authenticates with: its
// registration token's hash, its public keys, the tokens they were
// exchanged for and the jtis its signed requests spent. Every check
// that reads the time reads the database's.
type CredentialStore interface {
	// IssueRegistrationToken stores hash as g's one registration token,
	// replacing any it held, live for ttl from the database's time,
	// and answers when it expires. It deletes every expired one, of
	// any instance, in the same write. ErrInstanceNotFound where the
	// organization holds no record under g.
	IssueRegistrationToken(ctx context.Context, g grn.GRN, hash string, ttl time.Duration) (time.Time, error)
	// SpendRegistrationToken deletes the registration token hash names
	// and answers its instance where it was live, or
	// ErrRegistrationRefused where it was spent, expired or unknown
	// (REG-3, REG-4).
	SpendRegistrationToken(ctx context.Context, hash string) (grn.GRN, error)
	// BindKey withdraws every key g held, deletes their tokens and
	// binds key as g's only live key, in one transaction (REG-6).
	// ErrInstanceNotFound where the record went meanwhile.
	BindKey(ctx context.Context, g grn.GRN, key PublicKey) error
	// VerificationKey answers g's live key kid, or
	// operator.ErrClientInvalid where none is.
	VerificationKey(ctx context.Context, g grn.GRN, kid string) (Key, error)
	// RecordAssertionToken admits a verified assertion and stores t's
	// hash under its key in one transaction holding the key's row: it
	// rereads that the key is live, checks the assertion's dates at the
	// database's time, spends its jti, stamps t's dates from that time
	// and sweeps the instance's expired tokens. A key withdrawn
	// meanwhile, an untimely assertion and a spent jti each answer
	// operator.ErrClientInvalid and record nothing.
	RecordAssertionToken(ctx context.Context, a operator.VerifiedAssertion, t *Token) error
	// ResolveToken answers what the live token hash names, read beside
	// the record, its installation's app and the key it rests on, and
	// records the instance's contact, or ErrTokenInvalid.
	ResolveToken(ctx context.Context, hash string) (TokenHolder, error)
	// Rotate adds successor beside g's live keys, authorized by the
	// live key whose row is authorizedBy, checks each of signed's dates
	// at the database's time and spends its jti, in one transaction
	// (ROT-2 to ROT-4). A successor already live answers the rotation
	// that bound it; one the instance held before, an authorizing key no
	// longer live, an untimely JWS or a spent jti is ErrInvalidProof;
	// two live keys is ErrTooManyKeys.
	Rotate(ctx context.Context, g grn.GRN, authorizedBy int64, successor PublicKey, signed []operator.VerifiedAssertion) (Rotation, error)
	// ListKeys answers every key g held, withdrawn ones included,
	// oldest first, or ErrInstanceNotFound.
	ListKeys(ctx context.Context, g grn.GRN) ([]Key, error)
	// RevokeKey withdraws g's key kid and deletes its tokens in one
	// write. A key already withdrawn answers nil; one g never held
	// answers ErrKeyNotFound.
	RevokeKey(ctx context.Context, g grn.GRN, kid string) error
}
