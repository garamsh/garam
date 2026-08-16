package ca

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/garamsh/garam/internal/errorx"
)

// Sealer seals key material so that what reaches storage is useless
// without the key-encryption key the process holds. Both methods take a
// context so an implementation backed by a remote key service replaces
// this one without touching a caller.
type Sealer interface {
	Seal(ctx context.Context, plaintext []byte) ([]byte, error)
	Open(ctx context.Context, sealed []byte) ([]byte, error)
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
// Sealed bytes are laid out as key identifier, nonce, then ciphertext,
// and the identifier is the AEAD's additional data. That layout is what
// reaches the database, so stored bytes answer which key-encryption key
// opens them without holding any of it.
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
// a file readable only by its owner (ADR-0015), so a file any group or
// other user can read is refused rather than used.
func NewSealerFromKeyFile(path string) (Sealer, error) {
	if path == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "ca: key-encryption key file is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: key-encryption key file", errorx.KindInvalidArgument)
	}
	defer func() { _ = f.Close() }()
	// Stat the open file rather than the path: the mode that matters is
	// the one belonging to the bytes actually read.
	info, err := f.Stat()
	if err != nil {
		return nil, errorx.Wrap(err, "ca: key-encryption key file", errorx.KindInvalidArgument)
	}
	if info.Mode().Perm()&fs.FileMode(0o077) != 0 {
		return nil, errorx.New(errorx.KindInvalidArgument, "ca: key-encryption key file is readable beyond its owner")
	}
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: key-encryption key file", errorx.KindInvalidArgument)
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errorx.Wrap(err, "ca: key-encryption key is not hex", errorx.KindInvalidArgument)
	}
	return NewAESGCMSealer(key)
}

// Seal returns the plaintext encrypted under the key-encryption key,
// prefixed by the identifier of that key and the nonce.
func (s *aesGCMSealer) Seal(_ context.Context, plaintext []byte) ([]byte, error) {
	header := make([]byte, keyIDSize+s.aead.NonceSize())
	copy(header, s.keyID)
	nonce := header[keyIDSize:]
	if _, err := rand.Read(nonce); err != nil {
		return nil, errorx.Wrap(err, "ca: seal nonce", errorx.KindInternal)
	}
	return s.aead.Seal(header, nonce, plaintext, s.keyID), nil
}

// Open reverses Seal. Sealed bytes made under another key-encryption
// key are refused by name, so a process holding the wrong key says
// which one it needed instead of reporting corruption.
func (s *aesGCMSealer) Open(_ context.Context, sealed []byte) ([]byte, error) {
	header := keyIDSize + s.aead.NonceSize()
	if len(sealed) < header {
		return nil, errorx.New(errorx.KindDataLoss, "ca: sealed key is truncated")
	}
	keyID := sealed[:keyIDSize]
	if !bytes.Equal(keyID, s.keyID) {
		return nil, errorx.New(errorx.KindFailedPrecondition,
			"ca: sealed key needs key-encryption key "+hex.EncodeToString(keyID)+
				", process holds "+hex.EncodeToString(s.keyID))
	}
	plaintext, err := s.aead.Open(nil, sealed[keyIDSize:header], sealed[header:], s.keyID)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: sealed key does not open", errorx.KindDataLoss)
	}
	return plaintext, nil
}
