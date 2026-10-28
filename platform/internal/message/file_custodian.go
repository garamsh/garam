package message

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"math/big"

	"github.com/garamsh/garam/internal/errorx"
)

// fileCustodian generates each key in process and seals its private
// half under the key-encryption key, bound to the key's own row
// (ADR-0101). It is the custody a deployment that configures no
// provider holds every key in (ADR-0104).
type fileCustodian struct {
	sealer Sealer
}

// NewFileCustodian constructs the Custodian that seals with sealer.
func NewFileCustodian(sealer Sealer) Custodian {
	return &fileCustodian{sealer: sealer}
}

func (c *fileCustodian) Create(ctx context.Context) (*HeldKey, error) {
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, errorx.Wrap(err, "message: generate signing key", errorx.KindInternal)
	}
	public, err := private.PublicKey.Bytes()
	if err != nil {
		return nil, errorx.Wrap(err, "message: signing key public half", errorx.KindInternal)
	}
	raw, err := private.Bytes()
	if err != nil {
		return nil, errorx.Wrap(err, "message: signing key private half", errorx.KindInternal)
	}
	id, err := Thumbprint(public)
	if err != nil {
		return nil, err
	}
	sealed, err := c.sealer.Seal(ctx, keyBinding(id), raw)
	if err != nil {
		return nil, errorx.Wrap(err, "message: seal signing key")
	}
	return &HeldKey{PublicKey: public, Held: SealedKey(sealed)}, nil
}

func (c *fileCustodian) Holds(h Holding) bool {
	_, ok := h.(SealedKey)
	return ok
}

// Open recovers a key's private half from its sealed row. A key that
// does not open is garam's failure and not the caller's, whatever the
// sealer called it: under another key-encryption key it would
// otherwise read as a precondition the caller could change. The key is
// named so the log says which one to retire.
func (c *fileCustodian) Open(ctx context.Context, k *SigningKey) (KeySigner, error) {
	sealed, ok := k.Held.(SealedKey)
	if !ok {
		return nil, errorx.New(errorx.KindInternal, "message: signing key "+string(k.ID)+" is not sealed into its row")
	}
	raw, err := c.sealer.Open(ctx, keyBinding(k.ID), sealed)
	if err != nil {
		return nil, errorx.Wrap(err, "message: open signing key "+string(k.ID), errorx.KindInternal)
	}
	private, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return nil, errorx.Wrap(err, "message: signing key "+string(k.ID), errorx.KindInternal)
	}
	return openedKey{private: private}, nil
}

// openedKey is a private half held in memory, which signs reading
// nothing.
type openedKey struct {
	private *ecdsa.PrivateKey
}

func (k openedKey) Sign(_ context.Context, input []byte) ([]byte, error) {
	digest := sha256.Sum256(input)
	der, err := ecdsa.SignASN1(rand.Reader, k.private, digest[:])
	if err != nil {
		return nil, errorx.Wrap(err, "message: sign", errorx.KindInternal)
	}
	var rs struct{ R, S *big.Int }
	if _, err := asn1.Unmarshal(der, &rs); err != nil {
		return nil, errorx.Wrap(err, "message: signature encoding", errorx.KindInternal)
	}
	raw := make([]byte, 64)
	rs.R.FillBytes(raw[:32])
	rs.S.FillBytes(raw[32:])
	return raw, nil
}

// keyBinding ties sealed bytes to one signing key's row, in a namespace
// no certificate authority's binding shares.
func keyBinding(id KeyID) []byte {
	return []byte("message-signing-key:" + string(id))
}
