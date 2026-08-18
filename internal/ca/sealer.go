package ca

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/keyfile"
)

// Sealer seals key material so that what reaches storage is useless
// without the key-encryption key the process holds. Both methods take a
// context so an implementation backed by a remote key service replaces
// this one without touching a caller.
//
// binding names where the sealed bytes belong. It is authenticated but
// not encrypted, and the same value must be supplied to open them, so
// bytes moved to another row do not open there. A remote key service
// takes it as its encryption context.
type Sealer interface {
	Seal(ctx context.Context, binding, plaintext []byte) ([]byte, error)
	Open(ctx context.Context, binding, sealed []byte) ([]byte, error)
}

// KeySize is the length in bytes of a key-encryption key.
const KeySize = 32

const (
	// keyIDSize is how much of the derivation below identifies the key.
	// Eight bytes name a key; they carry none of it.
	keyIDSize = 8
	// keyIDLabel separates the identifier derivation from any other use
	// of the key, so the identifier is not a plain digest of it.
	keyIDLabel = "garam ca key-encryption key id"
)

// aesGCMSealer seals with AES-256-GCM under a key-encryption key held
// in memory for the life of the process.
//
// Sealed bytes are laid out as key identifier, nonce, then ciphertext.
// That layout is what reaches the database, so stored bytes answer
// which key-encryption key opens them without holding any of it.
type aesGCMSealer struct {
	aead  cipher.AEAD
	keyID []byte
}

// NewAESGCMSealer constructs the default Sealer from a key-encryption
// key of exactly [KeySize] bytes.
func NewAESGCMSealer(key []byte) (Sealer, error) {
	if len(key) != KeySize {
		return nil, errorx.New(errorx.KindInvalidArgument, "ca: key-encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: key-encryption key", errorx.KindInvalidArgument)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: aes-gcm", errorx.KindInternal)
	}
	digest := sha256.Sum256(append([]byte(keyIDLabel), key...))
	return &aesGCMSealer{aead: aead, keyID: digest[:keyIDSize]}, nil
}

// NewSealerFromKeyFile reads a hex-encoded key-encryption key from path
// and constructs the default Sealer. Key material reaches a process as
// a file readable only by its owner (ADR-0015), so the read goes
// through keyfile and a file any group or other user can reach is
// refused rather than used.
func NewSealerFromKeyFile(path string) (Sealer, error) {
	if path == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "ca: key-encryption key file is required")
	}
	raw, err := keyfile.Read(path)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: key-encryption key file")
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		// The cause names the offending byte, which is a byte of the
		// key file, so it does not travel with the error.
		return nil, errorx.New(errorx.KindInvalidArgument, "ca: key-encryption key is not hex")
	}
	return NewAESGCMSealer(key)
}

// Seal returns the plaintext encrypted under the key-encryption key,
// prefixed by the identifier of that key and the nonce, and tied to
// binding.
func (s *aesGCMSealer) Seal(_ context.Context, binding, plaintext []byte) ([]byte, error) {
	header := make([]byte, keyIDSize+s.aead.NonceSize())
	copy(header, s.keyID)
	nonce := header[keyIDSize:]
	if _, err := rand.Read(nonce); err != nil {
		return nil, errorx.Wrap(err, "ca: seal nonce", errorx.KindInternal)
	}
	return s.aead.Seal(header, nonce, plaintext, s.additionalData(binding)), nil
}

// Open reverses Seal. Sealed bytes made under another key-encryption
// key are refused by the name of the one they need, and bytes carrying
// another binding do not open at all, so moving a row's key onto
// another row yields nothing.
func (s *aesGCMSealer) Open(_ context.Context, binding, sealed []byte) ([]byte, error) {
	header := keyIDSize + s.aead.NonceSize()
	if len(sealed) < header {
		return nil, errorx.New(errorx.KindDataLoss, "ca: sealed key is truncated")
	}
	keyID := sealed[:keyIDSize]
	if !bytes.Equal(keyID, s.keyID) {
		return nil, errorx.New(errorx.KindFailedPrecondition,
			"ca: sealed key needs key-encryption key "+hex.EncodeToString(keyID))
	}
	plaintext, err := s.aead.Open(nil, sealed[keyIDSize:header], sealed[header:], s.additionalData(binding))
	if err != nil {
		return nil, errorx.Wrap(err, "ca: sealed key does not open", errorx.KindDataLoss)
	}
	return plaintext, nil
}

// additionalData is what the AEAD authenticates beside the ciphertext:
// the key identifier, so the prefix cannot be rewritten, and the
// binding, so the bytes open only where they were written.
func (s *aesGCMSealer) additionalData(binding []byte) []byte {
	data := make([]byte, 0, len(s.keyID)+len(binding))
	data = append(data, s.keyID...)
	return append(data, binding...)
}
