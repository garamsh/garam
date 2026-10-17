package message

import "context"

// KeySigner signs under one key a Custodian holds.
type KeySigner interface {
	// Sign signs a JWS signing input (RFC 7515) with ES256 and answers
	// the signature as its fixed-length r and s.
	Sign(ctx context.Context, input []byte) ([]byte, error)
}
