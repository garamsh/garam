package message

import "context"

// Sealer seals a signing key's private half so that what reaches
// storage is useless without the key-encryption key the process holds
// (ADR-0016, ADR-0101). binding names the row the sealed bytes belong
// to, and the same value opens them.
type Sealer interface {
	Seal(ctx context.Context, binding, plaintext []byte) ([]byte, error)
	Open(ctx context.Context, binding, sealed []byte) ([]byte, error)
}
