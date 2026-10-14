package operator

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// KeyAlgorithm is the one signature algorithm an operator key is
// issued for and an assertion is accepted under (ADR-0102): RS256,
// which RFC 7523 makes mandatory to implement.
const KeyAlgorithm = "RS256"

// IssuedKeyBits is the size of the RSA key garam issues (ADR-0102).
const IssuedKeyBits = 3072

// The RSA sizes a verification key may have (ADR-0102). Every key
// garam issues is IssuedKeyBits; the bounds are what a stored key is
// checked against before it verifies anything.
const (
	MinKeyBits = 2048
	MaxKeyBits = 4096
)

// LiveKeysPerOperator is how many keys an operator may hold at once:
// the key in use and its planned successor (ADR-0102).
const LiveKeysPerOperator = 2

// RolloverWindow is how long a planned rotation keeps the predecessor
// accepted beside its successor (ADR-0102).
const RolloverWindow = 24 * time.Hour

// Sentinels of the key half.
var (
	// ErrKeyNotFound refuses a key the operator was never issued.
	ErrKeyNotFound = errorx.New(errorx.KindNotFound, "operator key not found")
	// ErrKeyLimit refuses a planned rotation where the operator already
	// holds two live keys: one is withdrawn first (ADR-0102).
	ErrKeyLimit = errorx.New(errorx.KindFailedPrecondition,
		"the operator holds two live keys: withdraw one before issuing another")
	// ErrOperatorWasKeyed refuses issuing a first key to an operator
	// that has been issued one, live or withdrawn. Any key change on
	// such an operator, a re-key after a revocation included, is
	// operator:reissue and not the registration's authority (ADR-0102).
	ErrOperatorWasKeyed = errorx.New(errorx.KindPermissionDenied,
		"the operator has been issued a key: issuing another is operator:reissue")
	// ErrKeyInvalid refuses a stored key that cannot verify: not an
	// RSA public key of a supported size, or not the recorded
	// algorithm. The schema and issuance admit no such row, so it is
	// the database's fault.
	ErrKeyInvalid = errorx.New(errorx.KindInternal, "stored operator key cannot verify")
)

// KeyRotation is how a newly issued key stands beside the keys the
// operator already holds (ADR-0102).
type KeyRotation string

const (
	// KeyRotationFirst issues a key only to an operator that has never
	// been issued one. It is the issuance the registration's authority
	// reaches.
	KeyRotationFirst KeyRotation = "first"
	// KeyRotationPlanned adds the key beside the one in use, which is
	// withdrawn by itself RolloverWindow later. An operator already
	// holding two live keys is refused.
	KeyRotationPlanned KeyRotation = "planned"
	// KeyRotationImmediate withdraws every live key and its tokens
	// before the new key is enabled, in one write: the answer to a
	// compromised key or a lost issuance.
	KeyRotationImmediate KeyRotation = "immediate"
)

// KeyState is where a key stands, derived from its row and the
// database's time and recorded nowhere as such.
//
// What the members mean to a browser caller is `api/openapi.yaml`
// §OperatorKeyState, which is authoritative for that (ADR-0059).
type KeyState string

const (
	// KeyStateActive verifies assertions and has no withdrawal set.
	KeyStateActive KeyState = "active"
	// KeyStateRetiring verifies assertions until its planned
	// withdrawal, which a successor's issuance set.
	KeyStateRetiring KeyState = "retiring"
	// KeyStateRevoked verifies nothing: it was withdrawn, or its
	// planned withdrawal has passed.
	KeyStateRevoked KeyState = "revoked"
)

// ParseKeyState reads a state the store derived.
func ParseKeyState(s string) (KeyState, error) {
	switch state := KeyState(s); state {
	case KeyStateActive, KeyStateRetiring, KeyStateRevoked:
		return state, nil
	}
	return "", errorx.New(errorx.KindInternal, "operator key: unknown state "+s)
}

// Key is the public record of one key an operator was issued: what
// verifies its assertions and where the key stands. It holds no
// private material, which garam answers once and keeps nowhere.
type Key struct {
	// ID is the row's identity, which a token exchanged under the key
	// references.
	ID       int64
	Operator grn.GRN
	// KID is the base64url SHA-256 RFC 7638 thumbprint of the public
	// key, which an assertion's protected kid names.
	KID       string
	Algorithm string
	Public    *rsa.PublicKey
	IssuedBy  grn.GRN
	IssuedAt  time.Time
	State     KeyState
	// RetiresAt is when a planned rotation withdraws the key, nil
	// where none is set.
	RetiresAt *time.Time
	// RevokedAt is when the key was withdrawn, nil where it was not.
	RevokedAt *time.Time
	// LastExchangedAt is when an assertion signed by the key last
	// obtained a token, nil where none has.
	LastExchangedAt *time.Time
}

// IssuedKey is a key garam generated for an operator, carrying the
// private PEM it answers once (ADR-0102).
//
// The private key exists in this value alone. What reaches the
// database is the public JWK, so the caller that issued it is the only
// one ever shown the private half.
type IssuedKey struct {
	privatePEM []byte
	public     *rsa.PublicKey
	kid        string
	operator   grn.GRN
	issuedBy   grn.GRN
	issuedAt   time.Time
}

// NewIssuedKey generates a key for the operator: RSA of IssuedKeyBits,
// rendered as a PKCS#8 PRIVATE KEY PEM. issuedBy is the member issuing.
func NewIssuedKey(operatorGRN, issuedBy grn.GRN) (*IssuedKey, error) {
	if operatorGRN.ServiceType != ServiceType {
		return nil, ErrNotAnOperator
	}
	private, err := rsa.GenerateKey(rand.Reader, IssuedKeyBits)
	if err != nil {
		return nil, errorx.Wrap(err, "operator key", errorx.KindInternal)
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		return nil, errorx.Wrap(err, "operator key", errorx.KindInternal)
	}
	kid, err := Thumbprint(&private.PublicKey)
	if err != nil {
		return nil, err
	}
	return &IssuedKey{
		privatePEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}),
		public:     &private.PublicKey,
		kid:        kid,
		operator:   operatorGRN,
		issuedBy:   issuedBy,
		issuedAt:   time.Now().UTC(),
	}, nil
}

// PrivatePEM returns the private key the person installs in the
// operator's cluster. It is answered once, by the call that issued it.
func (k *IssuedKey) PrivatePEM() []byte { return k.privatePEM }

// KID returns the key's thumbprint, which is also its fingerprint.
func (k *IssuedKey) KID() string { return k.kid }

// Operator returns the operator the key proves.
func (k *IssuedKey) Operator() grn.GRN { return k.operator }

// IssuedBy returns the member that issued it.
func (k *IssuedKey) IssuedBy() grn.GRN { return k.issuedBy }

// IssuedAt returns when it was issued.
func (k *IssuedKey) IssuedAt() time.Time { return k.issuedAt }

// PublicJWK returns the persisted form of the key: its public JWK,
// carrying kid, alg and use and no private member.
func (k *IssuedKey) PublicJWK() (string, error) {
	return MarshalPublicJWK(k.public, k.kid)
}

// Thumbprint answers the base64url SHA-256 RFC 7638 thumbprint of an
// RSA public key: the kid an assertion names it by.
func Thumbprint(public *rsa.PublicKey) (string, error) {
	jwk := jose.JSONWebKey{Key: public}
	sum, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return "", errorx.Wrap(err, "operator key thumbprint", errorx.KindInternal)
	}
	return base64.RawURLEncoding.EncodeToString(sum), nil
}

// MarshalPublicJWK renders public as the JWK garam stores and
// publishes.
func MarshalPublicJWK(public *rsa.PublicKey, kid string) (string, error) {
	b, err := json.Marshal(jose.JSONWebKey{Key: public, KeyID: kid, Algorithm: KeyAlgorithm, Use: "sig"})
	if err != nil {
		return "", errorx.Wrap(err, "operator key jwk", errorx.KindInternal)
	}
	return string(b), nil
}

// ParsePublicJWK reads a stored JWK back into the key it verifies with,
// refusing anything but a public RSA key of a supported size whose
// thumbprint is kid and whose algorithm is KeyAlgorithm.
func ParsePublicJWK(stored, kid string) (*rsa.PublicKey, error) {
	var jwk jose.JSONWebKey
	if err := json.Unmarshal([]byte(stored), &jwk); err != nil {
		return nil, errorx.Wrap(ErrKeyInvalid, err.Error())
	}
	public, ok := jwk.Key.(*rsa.PublicKey)
	if !ok || !jwk.IsPublic() || jwk.Algorithm != KeyAlgorithm {
		return nil, ErrKeyInvalid
	}
	if bits := public.N.BitLen(); bits < MinKeyBits || bits > MaxKeyBits {
		return nil, ErrKeyInvalid
	}
	thumbprint, err := Thumbprint(public)
	if err != nil || thumbprint != kid {
		return nil, ErrKeyInvalid
	}
	return public, nil
}
