package policy

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"time"

	"github.com/garamsh/garam/lib/errorx"
)

const (
	// assertionLifetime is how long an assertion lives: the longest the
	// token endpoint accepts (ADR-0129).
	assertionLifetime = 60 * time.Second

	minModulusBits = 2048
	maxModulusBits = 4096
)

// parseKey parses a PEM PKCS#8 or PKCS#1 RSA key of 2048 to 4096 bits.
func parseKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errorx.New(errorx.KindInvalidArgument, "policy: deployment key is not PEM")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		parsed, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, errorx.Wrap(err, "policy: parse PKCS#1 key", errorx.KindInvalidArgument)
		}
		key = parsed
	case "PRIVATE KEY":
		parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, errorx.Wrap(err, "policy: parse PKCS#8 key", errorx.KindInvalidArgument)
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, errorx.New(errorx.KindInvalidArgument, "policy: deployment key is not RSA")
		}
		key = rsaKey
	default:
		return nil, errorx.New(errorx.KindInvalidArgument, "policy: PEM block is not a private key")
	}
	if bits := key.N.BitLen(); bits < minModulusBits || bits > maxModulusBits {
		return nil, errorx.New(errorx.KindInvalidArgument, "policy: modulus is outside 2048 to 4096 bits")
	}
	return key, nil
}

// thumbprint is the key's RFC 7638 SHA-256 thumbprint, base64url
// without padding: the `kid` the platform knows the key by.
func thumbprint(pub *rsa.PublicKey) string {
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	sum := sha256.Sum256([]byte(`{"e":"` + e + `","kty":"RSA","n":"` + n + `"}`))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// assertion signs the RFC 7523 client assertion cowork presents as
// clientID to the endpoint at tokenURL: RS256, `kid` the key's
// thumbprint, a fresh `jti`.
func assertion(key *rsa.PrivateKey, clientID, tokenURL string, now time.Time) (string, error) {
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", errorx.Wrap(err, "policy: jti", errorx.KindInternal)
	}
	header, err := json.Marshal(map[string]string{"alg": "RS256", "kid": thumbprint(&key.PublicKey), "typ": "JWT"})
	if err != nil {
		return "", errorx.Wrap(err, "policy: assertion header", errorx.KindInternal)
	}
	claims, err := json.Marshal(map[string]any{
		"iss": clientID,
		"sub": clientID,
		"aud": tokenURL,
		"iat": now.Unix(),
		"exp": now.Add(assertionLifetime).Unix(),
		"jti": base64.RawURLEncoding.EncodeToString(jti),
	})
	if err != nil {
		return "", errorx.Wrap(err, "policy: assertion claims", errorx.KindInternal)
	}
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", errorx.Wrap(err, "policy: sign assertion", errorx.KindInternal)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
