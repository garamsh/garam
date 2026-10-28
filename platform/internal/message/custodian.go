package message

import "context"

// Custodian holds the private half of the keys messages are signed
// with (ADR-0104). It never hands the private half back: it makes a
// key and answers where the key is held, and given a key it holds, it
// signs under it.
type Custodian interface {
	// Create makes a P-256 key and answers its public half, as an
	// uncompressed point, and where its private half is held.
	Create(ctx context.Context) (*HeldKey, error)
	// Holds answers whether this custodian signs for a key held there.
	Holds(h Holding) bool
	// Open answers a signer for a key this custodian holds. It reaches
	// nothing remote, so a key it holds opens wherever the custodian
	// can be configured.
	Open(ctx context.Context, key *SigningKey) (KeySigner, error)
}

// HeldKey is a key a Custodian made: its public half and where its
// private half is held.
type HeldKey struct {
	PublicKey []byte
	Held      Holding
}
