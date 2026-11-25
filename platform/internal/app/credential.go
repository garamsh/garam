package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// The prefixes an instance's secrets carry, so a scanner recognises
// one: the one-time registration token, and a token a key was exchanged
// for.
const (
	RegistrationTokenPrefix = "garam_crt_"
	TokenPrefix             = "garam_ct_"
)

// RegistrationTokenTTL is how long a registration token is accepted
// (ADR-0184 decision 3, REG-5).
const RegistrationTokenTTL = time.Hour

// TokenTTL is how long a token an instance's key was exchanged for is
// accepted, as an operator's is (ADR-0184 decision 5).
const TokenTTL = operator.TokenTTL

// The bounds on a key an instance registers or rotates to: RSA of at
// least 3072 bits (REG-1), at most what an assertion verifies with.
const (
	MinKeyBits = 3072
	MaxKeyBits = operator.MaxKeyBits
)

// LiveKeysPerInstance is how many keys an instance may hold at once:
// the key in use and its successor during the rollover (ROT-4).
const LiveKeysPerInstance = 2

// RolloverWindow is how long a rotation keeps the predecessor live
// beside its successor (ROT-3).
const RolloverWindow = operator.RolloverWindow

// secretBytes sizes a token: 256 bits, past ADR-0184's 128.
const secretBytes = 32

// Sentinels of the credential half.
var (
	// ErrRegistrationRefused is the one answer to a registration token
	// that is spent, expired or unknown (REG-4).
	ErrRegistrationRefused = errorx.New(errorx.KindUnauthenticated, "the registration token is not valid")
	// ErrKeyNotFound refuses a key the instance never held.
	ErrKeyNotFound = errorx.New(errorx.KindNotFound, "instance key not found")
	// ErrTokenInvalid refuses a token no live row stands for.
	ErrTokenInvalid = errorx.New(errorx.KindUnauthenticated, "instance token is not valid")
	// ErrInvalidProof refuses a rotation whose JWSs do not prove both
	// keys (ROT-2).
	ErrInvalidProof = errorx.New(errorx.KindInvalidArgument, "the rotation's proof does not hold")
	// ErrKeyHeld refuses a rotation to a key the instance has held, the
	// key its registration bound included, other than a live successor
	// a rotation made live, which is a repeat (ROT-4).
	ErrKeyHeld = errorx.New(errorx.KindInvalidArgument, "the instance has held this key")
	// ErrRotationUnavailable answers a rotation on a listener whose
	// token URL names no rotation route, a failure of garam's
	// configuration rather than the instance's proof.
	ErrRotationUnavailable = errorx.New(errorx.KindUnavailable,
		"key rotation is not served: GARAM_MACHINE_TOKEN_URL does not end in /oauth/token")
	// ErrTooManyKeys refuses a rotation while two keys are live (ROT-4).
	ErrTooManyKeys = errorx.New(errorx.KindFailedPrecondition, "the instance holds two live keys")
	// ErrInvalidKey refuses a JWK that is not the public half of an RSA
	// key of a size this profile takes (REG-1, REG-2).
	ErrInvalidKey = errorx.New(errorx.KindInvalidArgument,
		"the key is not the public half of an RSA key of 3072 to 4096 bits")
)

// privateMembers are the JWK members naming a private parameter of an
// RSA key (RFC 7518 §6.3.2). A JWK carrying any is refused, whether or
// not the rest of it parses (REG-2).
var privateMembers = []string{"d", "p", "q", "dp", "dq", "qi", "oth"}

// PublicKey is an instance's public key as garam takes it: an RSA key
// of a supported size, and its RFC 7638 thumbprint.
type PublicKey struct {
	key *rsa.PublicKey
	kid string
}

// ParsePublicJWK reads raw, the JWK an instance sent, as the public
// half of an RSA key of MinKeyBits to MaxKeyBits. A member naming a
// private parameter, an alg other than RS256 or a use other than sig
// refuses it.
func ParsePublicJWK(raw []byte) (PublicKey, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(raw, &members); err != nil {
		return PublicKey{}, ErrInvalidKey
	}
	for _, name := range privateMembers {
		if _, present := members[name]; present {
			return PublicKey{}, ErrInvalidKey
		}
	}
	var jwk jose.JSONWebKey
	if err := json.Unmarshal(raw, &jwk); err != nil {
		return PublicKey{}, ErrInvalidKey
	}
	public, ok := jwk.Key.(*rsa.PublicKey)
	if !ok || !jwk.IsPublic() {
		return PublicKey{}, ErrInvalidKey
	}
	if jwk.Algorithm != "" && jwk.Algorithm != operator.KeyAlgorithm || jwk.Use != "" && jwk.Use != "sig" {
		return PublicKey{}, ErrInvalidKey
	}
	if bits := public.N.BitLen(); bits < MinKeyBits || bits > MaxKeyBits {
		return PublicKey{}, ErrInvalidKey
	}
	kid, err := operator.Thumbprint(public)
	if err != nil {
		return PublicKey{}, err
	}
	return PublicKey{key: public, kid: kid}, nil
}

// KID returns the key's RFC 7638 thumbprint, which a signed request's
// kid names it by.
func (k PublicKey) KID() string { return k.kid }

// Public returns the RSA key.
func (k PublicKey) Public() *rsa.PublicKey { return k.key }

// JWK returns the form garam stores: the public JWK with kid, alg and
// use.
func (k PublicKey) JWK() (string, error) {
	return operator.MarshalPublicJWK(k.key, k.kid)
}

// KeyState is where a key stands at the database's time.
type KeyState string

const (
	KeyStateActive   KeyState = "active"
	KeyStateRetiring KeyState = "retiring"
	KeyStateRevoked  KeyState = "revoked"
)

// Key is the public record of a key an instance generated.
// garam never holds its private half (ADR-0184 decision 4).
type Key struct {
	ID        int64
	Instance  grn.GRN
	KID       string
	Public    *rsa.PublicKey
	CreatedAt time.Time
	State     KeyState
	// RetiresAt is when a rotation withdraws the key, nil where none
	// is set; RevokedAt when it was withdrawn, nil where it was not.
	RetiresAt *time.Time
	RevokedAt *time.Time
}

// Verifier returns the key as the operator package's assertion check
// reads one: the same RS256 profile verifies an instance's assertion.
func (k Key) Verifier() operator.Key {
	return operator.Key{ID: k.ID, Operator: k.Instance, KID: k.KID,
		Algorithm: operator.KeyAlgorithm, Public: k.Public}
}

// Token is a token an instance's key was exchanged for. It exists in
// this value alone, and what is stored is its hash.
type Token struct {
	Value     string
	Instance  grn.GRN
	IssuedAt  time.Time
	ExpiresAt time.Time
}

// TokenHolder is what a live token's row names, read beside the record
// and the key it rests on.
type TokenHolder struct {
	Instance  grn.GRN
	ExpiresAt time.Time
	// Operations are the machine operations the instance's app lists,
	// the routes its token reaches beyond the app system's own.
	Operations []string
}

// Rotation is a rotation's answer: the successor's kid, and when its
// predecessor is withdrawn (ROT-3).
type Rotation struct {
	KID                  string
	PredecessorRetiresAt time.Time
}

// NewSecret draws 256 random bits and renders them behind prefix.
func NewSecret(prefix string) (string, error) {
	var b [secretBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errorx.Wrap(err, "instance secret", errorx.KindInternal)
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

// WellFormed reports whether value is a secret NewSecret could have
// drawn behind prefix. One that is not is refused before anything is
// read for it.
func WellFormed(value, prefix string) bool {
	rest, ok := strings.CutPrefix(value, prefix)
	if !ok || len(rest) != 2*secretBytes {
		return false
	}
	_, err := hex.DecodeString(rest)
	return err == nil && strings.ToLower(rest) == rest
}

// HashSecret derives the persisted form of a secret. Only the hash
// reaches the database (ADR-0184 decision 3).
func HashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
