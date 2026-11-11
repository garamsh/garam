// Package signer holds the key the platform signs JWTs with, and
// signs and publishes under ADR-0138's token profile: RS256, a
// protected header of `alg`, `kid` and `typ` alone, string `aud` and
// integer dates, and a lifetime of at most 300 seconds. Loading the
// key, signing and producing the JWKS are separate calls, so a
// caller takes what it needs.
package signer

import (
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/keyfile"
)

// MaxLifetime is the longest a token may live (ADR-0138 item 2).
const MaxLifetime = 300 * time.Second

// Modulus sizes ADR-0138 item 3 allows a key.
const (
	minModulusBits = 2048
	maxModulusBits = 4096
)

// Key is an RSA signing key with the identifier its tokens name it by.
type Key struct {
	private *rsa.PrivateKey
	kid     string
}

// Claims are the registered claims a token carries. Each is required.
type Claims struct {
	Issuer   string
	Subject  string
	Audience string
	IssuedAt time.Time
	// Lifetime is how long the token lives from IssuedAt: at least a
	// second and at most MaxLifetime.
	Lifetime time.Duration
}

// payload is the token's JSON body. aud is a string, never an array.
type payload struct {
	Issuer   string `json:"iss"`
	Subject  string `json:"sub"`
	Audience string `json:"aud"`
	IssuedAt int64  `json:"iat"`
	Expires  int64  `json:"exp"`
}

// LoadKey reads the PEM key file at path, which only its owner may
// reach (ADR-0015), and parses it as ParseKey does.
func LoadKey(path string) (*Key, error) {
	raw, err := keyfile.Read(path)
	if err != nil {
		return nil, err
	}
	return ParseKey(raw)
}

// ParseKey parses a PEM-encoded PKCS#8 or PKCS#1 RSA private key of
// 2048 to 4096 bits. Its identifier is the key's RFC 7638 thumbprint,
// so the same key names itself the same way in every process.
func ParseKey(pemBytes []byte) (*Key, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errorx.New(errorx.KindInvalidArgument, "signer: key is not PEM")
	}
	var private *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, errorx.Wrap(err, "signer: parse PKCS#1 key", errorx.KindInvalidArgument)
		}
		private = parsed
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, errorx.Wrap(err, "signer: parse PKCS#8 key", errorx.KindInvalidArgument)
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errorx.New(errorx.KindInvalidArgument, "signer: key is not RSA")
		}
		private = rsaKey
	default:
		return nil, errorx.New(errorx.KindInvalidArgument, "signer: PEM block is not a private key")
	}
	if bits := private.N.BitLen(); bits < minModulusBits || bits > maxModulusBits {
		return nil, errorx.New(errorx.KindInvalidArgument, "signer: modulus is outside 2048 to 4096 bits")
	}
	thumbprint, err := (&jose.JSONWebKey{Key: &private.PublicKey}).Thumbprint(crypto.SHA256)
	if err != nil {
		return nil, errorx.Wrap(err, "signer: key thumbprint", errorx.KindInternal)
	}
	return &Key{private: private, kid: base64.RawURLEncoding.EncodeToString(thumbprint)}, nil
}

// ID is the key's `kid`.
func (k *Key) ID() string { return k.kid }

// JWKS is the JSON Web Key Set holding the key's public half, in the
// shape ADR-0138 item 3 has a verifier read: `kty`, `use`, `alg`,
// `kid`, `n` and `e` and nothing else.
func (k *Key) JWKS() ([]byte, error) {
	set := jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       &k.private.PublicKey,
		KeyID:     k.kid,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}}}
	out, err := json.Marshal(set)
	if err != nil {
		return nil, errorx.Wrap(err, "signer: encode JWKS", errorx.KindInternal)
	}
	return out, nil
}

// Sign returns the compact token for c, refusing claims outside the
// profile.
func (k *Key) Sign(c Claims) (string, error) {
	if c.Issuer == "" || c.Subject == "" || c.Audience == "" {
		return "", errorx.New(errorx.KindInvalidArgument, "signer: iss, sub and aud are required")
	}
	if c.Lifetime < time.Second || c.Lifetime > MaxLifetime {
		return "", errorx.New(errorx.KindInvalidArgument, "signer: lifetime is outside 1 to 300 seconds")
	}
	issued := c.IssuedAt.Unix()
	body, err := json.Marshal(payload{
		Issuer:   c.Issuer,
		Subject:  c.Subject,
		Audience: c.Audience,
		IssuedAt: issued,
		Expires:  issued + int64(c.Lifetime/time.Second),
	})
	if err != nil {
		return "", errorx.Wrap(err, "signer: encode claims", errorx.KindInternal)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: k.private, KeyID: k.kid}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return "", errorx.Wrap(err, "signer: new signer", errorx.KindInternal)
	}
	signed, err := signer.Sign(body)
	if err != nil {
		return "", errorx.Wrap(err, "signer: sign", errorx.KindInternal)
	}
	token, err := signed.CompactSerialize()
	if err != nil {
		return "", errorx.Wrap(err, "signer: serialize", errorx.KindInternal)
	}
	return token, nil
}
