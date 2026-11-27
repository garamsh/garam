// Package dpop verifies the DPoP proofs (RFC 9449) that bind a token
// garam issues to a bot's process to that process's key (ADR-0191):
// at the token endpoint, where a proof names the key a token is
// bound to, and at a resource server, where it shows the token's
// presenter holds that key.
package dpop

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/garamsh/garam/lib/errorx"
)

const (
	// Header is the request header a proof is sent in.
	Header = "DPoP"
	// NonceHeader is the response header a fresh nonce is sent in.
	NonceHeader = "DPoP-Nonce"
	// TokenType is the token_type a bound token is answered with, and
	// the Authorization scheme it is presented under.
	TokenType = "DPoP"
	// ProofType is a proof's JOSE typ.
	ProofType = "dpop+jwt"
	// Window is how far a proof's iat may be from garam's clock,
	// either way (ADR-0191 decision 3).
	Window = 60 * time.Second
	// SpentFor is how long a proof's jti stays spent for its key:
	// twice the window, so no proof the window admits is admitted
	// again (ADR-0191 decision 4).
	SpentFor = 2 * Window
	// NonceTTL is how long a nonce garam issued is accepted (ADR-0191
	// decision 5).
	NonceTTL = 5 * time.Minute
	// MaxProofLength bounds the proof a header may carry.
	MaxProofLength = 4 << 10
)

// ErrProofInvalid refuses a proof that does not verify, does not name
// the request, was spent, or is not by the key a token is bound to:
// RFC 9449's invalid_dpop_proof.
var ErrProofInvalid = errorx.New(errorx.KindInvalidArgument, "the DPoP proof is invalid")

// ErrNonceRequired refuses a proof at the token endpoint that carries
// no nonce, or one garam did not issue or that expired: RFC 9449's
// use_dpop_nonce. The client retries with the nonce answered beside
// it.
var ErrNonceRequired = errorx.New(errorx.KindInvalidArgument, "the DPoP proof needs the server's nonce")

// Store keeps what makes a proof single-use and a nonce garam's, in
// one place every replica reads.
type Store interface {
	// SpendJTI records jti as spent for the key jkt names, for ttl
	// from the store's time; false answers one already spent. It
	// sweeps every expired jti.
	SpendJTI(ctx context.Context, jkt, jti string, ttl time.Duration) (bool, error)
	// IssueNonce records nonce as garam's for ttl from the store's
	// time, and sweeps every expired nonce.
	IssueNonce(ctx context.Context, nonce string, ttl time.Duration) error
	// NonceLive answers whether nonce was issued and has not expired.
	NonceLive(ctx context.Context, nonce string) (bool, error)
}

// Request is what a proof has to name: the request's method and its
// URL as garam serves it, and, where the request carries a token, that
// token.
type Request struct {
	Method string
	URL    string
	// Token is the access token the request presents, which the
	// proof's ath has to hash; empty where it presents none.
	Token string
	// Key is the thumbprint of the key Token is bound to, its cnf.jkt,
	// which the proof's key has to have; empty where the request
	// presents no bound token.
	Key string
	// Nonce makes the proof carry a nonce garam issued.
	Nonce bool
}

// Verifier verifies proofs against a Store.
type Verifier struct {
	store Store
	now   func() time.Time
}

// NewVerifier constructs a Verifier over store; a nil now is
// time.Now.
func NewVerifier(store Store, now func() time.Time) *Verifier {
	if now == nil {
		now = time.Now
	}
	return &Verifier{store: store, now: now}
}

// claims are a proof's claims ADR-0191 decision 3 names.
type claims struct {
	HTM   string `json:"htm"`
	HTU   string `json:"htu"`
	IAT   *int64 `json:"iat"`
	JTI   string `json:"jti"`
	Nonce string `json:"nonce"`
	ATH   string `json:"ath"`
}

// Verify checks proof against req and answers the RFC 7638 thumbprint
// of the key that signed it. It spends the proof's jti last, so a
// proof refused for anything else can be corrected and sent again.
func (v *Verifier) Verify(ctx context.Context, proof string, req Request) (string, error) {
	key, c, err := parse(proof)
	if err != nil {
		return "", err
	}
	if err := v.names(c, req); err != nil {
		return "", err
	}
	jkt, err := Thumbprint(key)
	if err != nil {
		return "", err
	}
	if req.Key != "" && jkt != req.Key {
		return "", errorx.Wrap(ErrProofInvalid, "the proof is not by the key the token is bound to")
	}
	if req.Nonce {
		if c.Nonce == "" {
			return "", ErrNonceRequired
		}
		live, err := v.store.NonceLive(ctx, c.Nonce)
		if err != nil {
			return "", err
		}
		if !live {
			return "", ErrNonceRequired
		}
	}
	fresh, err := v.store.SpendJTI(ctx, jkt, c.JTI, SpentFor)
	if err != nil {
		return "", err
	}
	if !fresh {
		return "", errorx.Wrap(ErrProofInvalid, "the proof's jti was spent")
	}
	return jkt, nil
}

// Nonce issues a fresh nonce, which every replica accepts for
// NonceTTL.
func (v *Verifier) Nonce(ctx context.Context) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", errorx.Wrap(err, "dpop nonce", errorx.KindInternal)
	}
	nonce := base64.RawURLEncoding.EncodeToString(raw)
	if err := v.store.IssueNonce(ctx, nonce, NonceTTL); err != nil {
		return "", err
	}
	return nonce, nil
}

// parse verifies proof's signature by the key its header carries and
// answers that key and the proof's claims.
func parse(proof string) (*ecdsa.PublicKey, claims, error) {
	var c claims
	if proof == "" {
		return nil, c, errorx.Wrap(ErrProofInvalid, "the request carries no DPoP proof")
	}
	if len(proof) > MaxProofLength {
		return nil, c, errorx.Wrap(ErrProofInvalid, "the proof is too long")
	}
	signed, err := jose.ParseSignedCompact(proof, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return nil, c, errorx.Wrap(ErrProofInvalid, "the proof is not an ES256 compact JWS")
	}
	if len(signed.Signatures) != 1 {
		return nil, c, errorx.Wrap(ErrProofInvalid, "the proof carries one signature")
	}
	header := signed.Signatures[0].Protected
	if typ, _ := header.ExtraHeaders[jose.HeaderType].(string); typ != ProofType {
		return nil, c, errorx.Wrap(ErrProofInvalid, "the proof's typ is "+ProofType)
	}
	// go-jose refuses, while parsing, a jwk with a private member
	// (RFC 7515 §4.1.3), so the one left to refuse here is none.
	if header.JSONWebKey == nil {
		return nil, c, errorx.Wrap(ErrProofInvalid, "the proof carries no jwk")
	}
	key, ok := header.JSONWebKey.Key.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, c, errorx.Wrap(ErrProofInvalid, "the proof's jwk is a P-256 key")
	}
	payload, err := signed.Verify(key)
	if err != nil {
		return nil, c, errorx.Wrap(ErrProofInvalid, "the proof's signature does not verify by its jwk")
	}
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, c, errorx.Wrap(ErrProofInvalid, "the proof's claims are not a JSON object")
	}
	return key, c, nil
}

// names checks that c names req, at the verifier's time.
func (v *Verifier) names(c claims, req Request) error {
	switch {
	case c.JTI == "":
		return errorx.Wrap(ErrProofInvalid, "the proof carries no jti")
	case c.HTM != req.Method:
		return errorx.Wrap(ErrProofInvalid, "the proof's htm is not the request's method")
	case !sameTarget(c.HTU, req.URL):
		return errorx.Wrap(ErrProofInvalid, "the proof's htu is not the request's URL")
	case c.IAT == nil:
		return errorx.Wrap(ErrProofInvalid, "the proof carries no iat")
	}
	at := time.Unix(*c.IAT, 0)
	if now := v.now(); at.Before(now.Add(-Window)) || at.After(now.Add(Window)) {
		return errorx.Wrap(ErrProofInvalid, "the proof's iat is outside the window")
	}
	switch {
	case req.Token == "" && c.ATH != "":
		return errorx.Wrap(ErrProofInvalid, "the proof's ath names no token the request carries")
	case req.Token != "" && c.ATH != TokenHash(req.Token):
		return errorx.Wrap(ErrProofInvalid, "the proof's ath is not the token's hash")
	}
	return nil
}

// sameTarget answers whether htu names served, ignoring both URLs'
// query and fragment and the case of their scheme and host (RFC 9449
// §4.3, RFC 3986 §6.2.2.1).
func sameTarget(htu, served string) bool {
	a, err := url.Parse(htu)
	if err != nil || !a.IsAbs() {
		return false
	}
	b, err := url.Parse(served)
	if err != nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host) &&
		a.EscapedPath() == b.EscapedPath()
}

// TokenHash is a proof's ath for token: the base64url SHA-256 of its
// ASCII.
func TokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Thumbprint is key's RFC 7638 SHA-256 thumbprint, base64url: the
// cnf.jkt of a token bound to it.
func Thumbprint(key *ecdsa.PublicKey) (string, error) {
	sum, err := (&jose.JSONWebKey{Key: key}).Thumbprint(crypto.SHA256)
	if err != nil {
		return "", errorx.Wrap(ErrProofInvalid, "the proof's jwk has no thumbprint")
	}
	return base64.RawURLEncoding.EncodeToString(sum), nil
}
