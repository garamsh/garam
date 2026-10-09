package message

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/garamsh/garam/internal/errorx"
)

// SignatureType is the `typ` of every signature garam makes over a
// message, so a token of another kind from the same issuer cannot pass
// as one (ADR-0101).
const SignatureType = "agent-message+jwt"

// signatureAlgorithm is the one algorithm a message signature uses, so
// a verifier pins it rather than negotiating one.
const signatureAlgorithm = "ES256"

const (
	// SigningKeyLead is how long a key is published before it signs.
	// It is part of the contract a verifier relies on, the refresh
	// deadline for a key set held as a file (ADR-0101).
	SigningKeyLead = 2 * time.Hour
	// SigningKeyLifetime is how long a key signs before garam announces
	// its replacement, less the lead.
	SigningKeyLifetime = 90 * 24 * time.Hour
	// KeySetMaxAge is how long a verifier may cache the published set.
	KeySetMaxAge = time.Hour
	// SkewAllowance is the clock difference a verifier allows when it
	// judges a signature's times.
	SkewAllowance = 60 * time.Second
)

// ErrNoSuchSigningKey is returned when no key carries the identifier.
var ErrNoSuchSigningKey = errorx.New(errorx.KindNotFound, "no such message signing key")

// Issuer is the deployment's issuer: the machine listener's external
// https base URL, with no path and no trailing slash, compared byte for
// byte (ADR-0101).
type Issuer string

// ParseIssuer reads an issuer, refusing anything that is not exactly an
// https origin: a verifier compares the value byte for byte, so
// `https://x/` and `https://x` would be two issuers.
func ParseIssuer(s string) (Issuer, error) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" ||
		u.Path != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(s, "#") || u.String() != s {
		return "", errorx.New(errorx.KindInvalidArgument,
			"message: issuer must be an https origin with no path, query, fragment or trailing slash")
	}
	return Issuer(s), nil
}

// KeyID names a signing key: its JWK thumbprint (RFC 7638).
type KeyID string

// SigningKey is one key garam signs messages with. It signs from
// SignsFrom until a later key's SignsFrom passes, unless it was
// withdrawn first.
type SigningKey struct {
	ID KeyID
	// PublicKey is the uncompressed P-256 point.
	PublicKey []byte
	// SealedPrivateKey opens only under the key-encryption key and only
	// for this key's row.
	SealedPrivateKey []byte
	SignsFrom        time.Time
	// WithdrawnAt is when a person retired the key at once, and nil for
	// a key nobody retired.
	WithdrawnAt *time.Time
}

// NewSigningKey is a key to store, which signs once Lead has passed on
// the database's clock.
type NewSigningKey struct {
	ID               KeyID
	PublicKey        []byte
	SealedPrivateKey []byte
	Lead             time.Duration
}

// PublishedKey is the public half of a published key, as the two
// fixed-length coordinates a JWK carries.
type PublishedKey struct {
	ID KeyID
	X  []byte
	Y  []byte
}

// Signed is what a hand-out carries beside the claim: the content a
// runtime is handed and garam's signature over it.
type Signed struct {
	Content   string
	Signature string
}

// Keyring holds the keys messages are signed with (ADR-0101). It
// answers the key that signs now, already opened, so a caller obtains
// it before it takes a claim: a key that cannot be opened then refuses
// the claim rather than spending one of the message's attempts.
type Keyring interface {
	// Signer answers the key that signs now, creating it first where
	// one is due.
	Signer(ctx context.Context) (Signer, error)
	// PublishedKeys answers the keys a verifier may need: the one
	// signing, one announced to sign next, and one that stopped while
	// a signature it made can still be valid.
	PublishedKeys(ctx context.Context) ([]PublishedKey, error)
}

// Signer signs hand-outs under one opened key. Signing reads nothing,
// so it fails only where the process cannot encode or draw randomness.
type Signer interface {
	// Sign signs one claim. Every claim signs afresh, and every attempt
	// of one message carries the same content and identifier.
	Sign(c *Claim) (Signed, error)
}

type keyring struct {
	store    SigningKeyStore
	sealer   Sealer
	issuer   Issuer
	validity time.Duration
}

// NewKeyring constructs the Keyring. A signature is valid for the lease
// ceiling cfg sets from when its claim was taken, so it does not
// outlive that claim.
func NewKeyring(store SigningKeyStore, sealer Sealer, issuer Issuer, cfg Config) (Keyring, error) {
	if issuer == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "message: an issuer is required to sign")
	}
	return &keyring{store: store, sealer: sealer, issuer: issuer, validity: withDefaults(cfg).LeaseCeiling}, nil
}

// openKey is one signing key with its private half opened.
type openKey struct {
	id       KeyID
	private  *ecdsa.PrivateKey
	issuer   Issuer
	validity time.Duration
}

// signedClaims is the payload: these members, which a verifier checks
// and garam promises the same across every attempt but the two times.
type signedClaims struct {
	Iss           string `json:"iss"`
	Sub           string `json:"sub"`
	Aud           string `json:"aud"`
	Org           string `json:"org"`
	MessageID     string `json:"message_id"`
	ContentSHA256 string `json:"content_sha256"`
	Iat           int64  `json:"iat"`
	Exp           int64  `json:"exp"`
}

// signatureHeader is the protected header, and nothing else is in it: a
// verifier takes no key, and no place to fetch one, from the message.
type signatureHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

func (r *keyring) Signer(ctx context.Context) (Signer, error) {
	keys, now, err := r.keys(ctx)
	if err != nil {
		return nil, err
	}
	key := signingAt(keys, now)
	if key == nil {
		return nil, errorx.New(errorx.KindInternal, "message: no signing key after rotation")
	}
	private, err := r.open(ctx, key)
	if err != nil {
		return nil, err
	}
	return &openKey{id: key.ID, private: private, issuer: r.issuer, validity: r.validity}, nil
}

func (s *openKey) Sign(c *Claim) (Signed, error) {
	m := c.Message()
	body, err := Content(m)
	if err != nil {
		return Signed{}, err
	}
	sub, err := s.vouchedSender(m)
	if err != nil {
		return Signed{}, err
	}
	digest := sha256.Sum256([]byte(body))
	signature, err := compactJWS(s.private, s.id, signedClaims{
		Iss:           string(s.issuer),
		Sub:           sub,
		Aud:           m.recipient.String(),
		Org:           m.recipient.Organization,
		MessageID:     string(m.id),
		ContentSHA256: encode(digest[:]),
		Iat:           c.claimedAt.Unix(),
		Exp:           c.claimedAt.Add(s.validity).Unix(),
	})
	if err != nil {
		return Signed{}, err
	}
	return Signed{Content: body, Signature: signature}, nil
}

// vouchedSender is the sender garam vouches for: the identity that
// posted the message, or garam itself for a message no caller posted.
func (s *openKey) vouchedSender(m *Message) (string, error) {
	switch m.author {
	case AuthorSender:
		return m.sender.String(), nil
	case AuthorGaram:
		return string(s.issuer), nil
	default:
		return "", errorx.New(errorx.KindInternal, "message: unknown author "+string(m.author))
	}
}

func (r *keyring) PublishedKeys(ctx context.Context) ([]PublishedKey, error) {
	keys, now, err := r.keys(ctx)
	if err != nil {
		return nil, err
	}
	var out []PublishedKey
	for _, k := range published(keys, now, r.validity) {
		x, y, err := coordinates(k.PublicKey)
		if err != nil {
			return nil, err
		}
		out = append(out, PublishedKey{ID: k.ID, X: x, Y: y})
	}
	return out, nil
}

// keys reads the keys, and first creates the one that is due if any is.
// Both paths that read them create it, so a set fetched before anything
// is handed out already holds the key that will sign.
func (r *keyring) keys(ctx context.Context) ([]*SigningKey, time.Time, error) {
	keys, now, err := r.store.SigningKeys(ctx)
	if err != nil {
		return nil, time.Time{}, err
	}
	if _, due := keyDue(keys, now); !due {
		return keys, now, nil
	}
	err = r.store.RotateSigningKeys(ctx, func(keys []*SigningKey, now time.Time) (*NewSigningKey, error) {
		lead, due := keyDue(keys, now)
		if !due {
			return nil, nil
		}
		return r.generate(ctx, lead)
	})
	if err != nil {
		return nil, time.Time{}, err
	}
	return r.store.SigningKeys(ctx)
}

// keyDue answers whether a key is due and the lead it is created with.
// With no key signing — the deployment's first, or one retired at once
// — the replacement signs immediately: there is no earlier set it could
// be missing from, and nothing else can sign meanwhile. Otherwise the
// next key is announced SigningKeyLead before the current one has
// signed for SigningKeyLifetime, and only one is announced at a time.
func keyDue(keys []*SigningKey, now time.Time) (time.Duration, bool) {
	current := signingAt(keys, now)
	if current == nil {
		return 0, true
	}
	for _, k := range keys {
		if k.SignsFrom.After(now) && k.WithdrawnAt == nil {
			return 0, false
		}
	}
	if now.Before(current.SignsFrom.Add(SigningKeyLifetime - SigningKeyLead)) {
		return 0, false
	}
	return SigningKeyLead, true
}

// signingAt is the key that signs at now: the last to have begun
// signing, unless it was withdrawn. An older key never signs again,
// because a later one has begun.
func signingAt(keys []*SigningKey, now time.Time) *SigningKey {
	var current *SigningKey
	for _, k := range keys {
		if !k.SignsFrom.After(now) {
			current = k
		}
	}
	if current == nil || current.WithdrawnAt != nil {
		return nil
	}
	return current
}

// published is the set a verifier may need at now. A key that stopped
// signing stays while a signature it made can still be valid, plus the
// skew allowance and one cache lifetime; a withdrawn key never appears.
func published(keys []*SigningKey, now time.Time, validity time.Duration) []*SigningKey {
	var out []*SigningKey
	for i, k := range keys {
		if k.WithdrawnAt != nil {
			continue
		}
		if k.SignsFrom.After(now) {
			out = append(out, k)
			continue
		}
		stopped, ok := stoppedSigning(keys[i+1:], now)
		if !ok || now.Before(stopped.Add(validity+SkewAllowance+KeySetMaxAge)) {
			out = append(out, k)
		}
	}
	return out
}

// stoppedSigning answers when a key stopped signing: when the first of
// the keys after it began, if one has.
func stoppedSigning(later []*SigningKey, now time.Time) (time.Time, bool) {
	for _, k := range later {
		if !k.SignsFrom.After(now) {
			return k.SignsFrom, true
		}
	}
	return time.Time{}, false
}

// generate makes a P-256 key and seals its private half to its own row.
func (r *keyring) generate(ctx context.Context, lead time.Duration) (*NewSigningKey, error) {
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
	sealed, err := r.sealer.Seal(ctx, keyBinding(id), raw)
	if err != nil {
		return nil, errorx.Wrap(err, "message: seal signing key")
	}
	return &NewSigningKey{ID: id, PublicKey: public, SealedPrivateKey: sealed, Lead: lead}, nil
}

// open recovers a key's private half from its sealed row. A key that
// does not open is garam's failure and not the caller's, whatever the
// sealer called it: under another key-encryption key it would
// otherwise read as a precondition the caller could change. The key is
// named so the log says which one to retire.
func (r *keyring) open(ctx context.Context, k *SigningKey) (*ecdsa.PrivateKey, error) {
	raw, err := r.sealer.Open(ctx, keyBinding(k.ID), k.SealedPrivateKey)
	if err != nil {
		return nil, errorx.Wrap(err, "message: open signing key "+string(k.ID), errorx.KindInternal)
	}
	private, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), raw)
	if err != nil {
		return nil, errorx.Wrap(err, "message: signing key "+string(k.ID), errorx.KindInternal)
	}
	return private, nil
}

// keyBinding ties sealed bytes to one signing key's row, in a namespace
// no certificate authority's binding shares.
func keyBinding(id KeyID) []byte {
	return []byte("message-signing-key:" + string(id))
}

// coordinates splits an uncompressed P-256 point into its two
// fixed-length coordinates.
func coordinates(public []byte) (x, y []byte, err error) {
	if len(public) != 65 || public[0] != 4 {
		return nil, nil, errorx.New(errorx.KindDataLoss, "message: signing key is not an uncompressed P-256 point")
	}
	return public[1:33], public[33:], nil
}

// Thumbprint is a P-256 public key's JWK thumbprint (RFC 7638): the
// digest of its required members in lexicographic order, with no
// whitespace.
func Thumbprint(public []byte) (KeyID, error) {
	x, y, err := coordinates(public)
	if err != nil {
		return "", err
	}
	canonical := `{"crv":"P-256","kty":"EC","x":"` + encode(x) + `","y":"` + encode(y) + `"}`
	digest := sha256.Sum256([]byte(canonical))
	return KeyID(encode(digest[:])), nil
}

// compactJWS signs the claims under key as a JWS in compact
// serialization (RFC 7515), the signature being ES256's fixed-length
// r and s.
func compactJWS(key *ecdsa.PrivateKey, kid KeyID, claims signedClaims) (string, error) {
	header, err := json.Marshal(signatureHeader{Alg: signatureAlgorithm, Kid: string(kid), Typ: SignatureType})
	if err != nil {
		return "", errorx.Wrap(err, "message: signature header", errorx.KindInternal)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", errorx.Wrap(err, "message: signature claims", errorx.KindInternal)
	}
	input := encode(header) + "." + encode(payload)
	digest := sha256.Sum256([]byte(input))
	der, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		return "", errorx.Wrap(err, "message: sign", errorx.KindInternal)
	}
	var rs struct{ R, S *big.Int }
	if _, err := asn1.Unmarshal(der, &rs); err != nil {
		return "", errorx.Wrap(err, "message: signature encoding", errorx.KindInternal)
	}
	raw := make([]byte, 64)
	rs.R.FillBytes(raw[:32])
	rs.S.FillBytes(raw[32:])
	return input + "." + encode(raw), nil
}

// encode is base64url without padding, the encoding every part of a
// JWS and a JWK uses.
func encode(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
