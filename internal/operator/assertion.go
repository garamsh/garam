package operator

import (
	"bytes"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-jose/go-jose/v4"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// AssertionContractVersion is the contract the private_key_jwt
// exchange serves under (ADR-0102), stamped on its answer.
const AssertionContractVersion = "machine-token.v2"

// The fixed parameter values of the client credentials request
// (ADR-0102, RFC 7523).
const (
	GrantTypeClientCredentials = "client_credentials"
	ClientAssertionTypeJWT     = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"
)

// The time bounds an assertion is held to (ADR-0102): its lifetime,
// exp less iat, and the skew allowed against the database's clock.
const (
	MaxAssertionLifetime = 60 * time.Second
	AssertionClockSkew   = 30 * time.Second
)

// The bounds on an assertion's jti (ADR-0102): at least 128 bits in
// base64url, at most 128 characters.
const (
	minJTILength = 22
	maxJTILength = 128
)

// The bounds on the assertion exchanges one process answers (ADR-0102),
// per ExchangeWindow: in all, counted before anything is read, and per
// client_id, counted once the assertion verified, over at most
// TrackedClients client_ids at once. An operator exchanges about once
// in forty minutes, so ten covers its retries with backoff. They are
// per replica: N replicas answer N times as many.
const (
	ExchangesPerClient  = 10
	ExchangesPerProcess = 600
	ExchangeWindow      = time.Minute
	TrackedClients      = 10000
)

// The bounds on the keys one process issues (ADR-0102): per operator
// and in all, per KeyIssuanceWindow. Generating a 3072-bit RSA key is
// the costliest thing a member can ask of the browser-facing listener.
const (
	KeyIssuancesPerOperator = 3
	KeyIssuancesPerProcess  = 30
	KeyIssuanceWindow       = time.Minute
)

// MaxAssertionLength bounds the compact JWT read before anything is
// parsed. An RS256 assertion over a 4096-bit key with the claims this
// profile reads is well under it.
const MaxAssertionLength = 8 << 10

// ErrClientInvalid refuses an assertion that authenticates no operator:
// malformed, unsigned by a live key of the named registration, naming
// another identity or audience, outside its time bounds, or a replay
// (ADR-0102). They are one refusal, so the answer reveals nothing about
// which registrations or keys exist.
var ErrClientInvalid = errorx.New(errorx.KindUnauthenticated, "client authentication failed")

// ErrAssertionProfileUnavailable refuses the exchange and key issuance
// where no token endpoint URL is configured: no assertion can name an
// audience garam would accept (ADR-0102).
var ErrAssertionProfileUnavailable = errorx.New(errorx.KindUnavailable,
	"the private_key_jwt profile is not configured: GARAM_MACHINE_TOKEN_URL is unset")

// ParseTokenEndpoint reads the configured token endpoint URL: empty,
// which serves no assertion exchange, or an absolute https URL with a
// host and no user information, query or fragment (ADR-0102). It
// answers raw unchanged, because an assertion's aud is compared with
// it byte for byte.
func ParseTokenEndpoint(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", errorx.Wrap(err, "token endpoint", errorx.KindInvalidArgument)
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return "", errorx.New(errorx.KindInvalidArgument,
			"token endpoint: an absolute https URL with no user information, query or fragment")
	}
	return raw, nil
}

var (
	compactJWT = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)
	base64URL  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	numericInt = regexp.MustCompile(`^[0-9]{1,15}$`)
)

// allowedHeader is every protected header member an assertion may
// carry (ADR-0102). Anything else — jwk, jku, x5u, x5c, crit — refuses,
// so no caller-supplied member chooses verification material.
var allowedHeader = map[string]bool{"alg": true, "kid": true, "typ": true}

// ClientAssertion is a compact JWT read far enough to name the key it
// claims to be signed by, and not yet verified: nothing it says is
// trusted until [ClientAssertion.Verify] answers.
type ClientAssertion struct {
	signed  *jose.JSONWebSignature
	kid     string
	payload []byte
}

// ParseClientAssertion reads compact as an assertion of this profile:
// three base64url segments, a protected header holding alg RS256, a
// kid and typ JWT and nothing else, and a JSON object payload, none of
// whose objects repeats a member. Anything else is ErrClientInvalid.
func ParseClientAssertion(compact string) (*ClientAssertion, error) {
	if len(compact) > MaxAssertionLength || !compactJWT.MatchString(compact) {
		return nil, ErrClientInvalid
	}
	segments := strings.Split(compact, ".")
	header, err := strictSegment(segments[0])
	if err != nil {
		return nil, err
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(segments[1])
	if err != nil {
		return nil, ErrClientInvalid
	}
	if _, err := strictObject(payload); err != nil {
		return nil, err
	}
	for name := range header {
		if !allowedHeader[name] {
			return nil, ErrClientInvalid
		}
	}
	if stringMember(header, "alg") != KeyAlgorithm || stringMember(header, "typ") != "JWT" {
		return nil, ErrClientInvalid
	}
	kid := stringMember(header, "kid")
	if kid == "" || !base64URL.MatchString(kid) {
		return nil, ErrClientInvalid
	}
	signed, err := jose.ParseSignedCompact(compact, []jose.SignatureAlgorithm{jose.RS256})
	if err != nil {
		return nil, ErrClientInvalid
	}
	return &ClientAssertion{signed: signed, kid: kid, payload: payload}, nil
}

// KID returns the key the assertion claims to be signed by. It selects
// a key within the client's registration and proves nothing alone.
func (a *ClientAssertion) KID() string { return a.kid }

// VerifiedAssertion is an assertion whose signature and identity held:
// what an exchange admits under its replay and time checks.
type VerifiedAssertion struct {
	// KeyID is the row of the key that verified it.
	KeyID    int64
	Operator grn.GRN
	JTI      string
	// IssuedAt, ExpiresAt and NotBefore are the claims as stated; the
	// time bounds are checked against the database's clock when the
	// exchange is admitted, by [VerifiedAssertion.Timely].
	IssuedAt  time.Time
	ExpiresAt time.Time
	NotBefore *time.Time
}

// Verify checks the signature with key, which the caller selected by
// [ClientAssertion.KID] within client's registration, and the claims
// against client and the token endpoint audience: iss and sub both
// client, aud the one string endpoint, integer iat and exp at most
// MaxAssertionLifetime apart, an nbf no later than exp, and a bounded
// base64url jti. Anything else is ErrClientInvalid.
func (a *ClientAssertion) Verify(key Key, client grn.GRN, endpoint string) (VerifiedAssertion, error) {
	if key.Operator != client || key.KID != a.kid || key.Algorithm != KeyAlgorithm {
		return VerifiedAssertion{}, ErrClientInvalid
	}
	if !supportedKey(key.Public) {
		return VerifiedAssertion{}, ErrClientInvalid
	}
	payload, err := a.signed.Verify(key.Public)
	if err != nil || !bytes.Equal(payload, a.payload) {
		return VerifiedAssertion{}, ErrClientInvalid
	}
	claims, err := strictObject(payload)
	if err != nil {
		return VerifiedAssertion{}, err
	}
	identity := client.String()
	if stringMember(claims, "iss") != identity || stringMember(claims, "sub") != identity {
		return VerifiedAssertion{}, ErrClientInvalid
	}
	if endpoint == "" || stringMember(claims, "aud") != endpoint {
		return VerifiedAssertion{}, ErrClientInvalid
	}
	iat, okIat := dateMember(claims, "iat")
	exp, okExp := dateMember(claims, "exp")
	if !okIat || !okExp {
		return VerifiedAssertion{}, ErrClientInvalid
	}
	if lifetime := exp.Sub(iat); lifetime <= 0 || lifetime > MaxAssertionLifetime {
		return VerifiedAssertion{}, ErrClientInvalid
	}
	verified := VerifiedAssertion{KeyID: key.ID, Operator: client, IssuedAt: iat, ExpiresAt: exp}
	if _, present := claims["nbf"]; present {
		nbf, ok := dateMember(claims, "nbf")
		if !ok || nbf.After(exp) {
			return VerifiedAssertion{}, ErrClientInvalid
		}
		verified.NotBefore = &nbf
	}
	jti := stringMember(claims, "jti")
	if len(jti) < minJTILength || len(jti) > maxJTILength || !base64URL.MatchString(jti) {
		return VerifiedAssertion{}, ErrClientInvalid
	}
	verified.JTI = jti
	return verified, nil
}

// Timely reports whether the assertion's dates hold at now, the
// database's time when the exchange is admitted: iat no later than
// now plus the skew, exp after now less it, and nbf, where given, no
// later than now plus it (ADR-0102).
func (v VerifiedAssertion) Timely(now time.Time) bool {
	if v.IssuedAt.After(now.Add(AssertionClockSkew)) || !v.ExpiresAt.After(now.Add(-AssertionClockSkew)) {
		return false
	}
	return v.NotBefore == nil || !v.NotBefore.After(now.Add(AssertionClockSkew))
}

// ReplayUntil is how long the assertion's jti is kept spent: its exp
// plus the skew, after which no clock garam allows accepts it.
func (v VerifiedAssertion) ReplayUntil() time.Time {
	return v.ExpiresAt.Add(AssertionClockSkew)
}

// supportedKey reports whether public is an RSA key of a size this
// profile verifies with.
func supportedKey(public *rsa.PublicKey) bool {
	if public == nil || public.N == nil {
		return false
	}
	bits := public.N.BitLen()
	return bits >= MinKeyBits && bits <= MaxKeyBits
}

// strictSegment decodes a base64url segment holding a JSON object.
func strictSegment(segment string) (map[string]json.RawMessage, error) {
	raw, err := base64.RawURLEncoding.Strict().DecodeString(segment)
	if err != nil {
		return nil, ErrClientInvalid
	}
	return strictObject(raw)
}

// strictObject reads raw as one JSON object of valid UTF-8, refusing
// a repeated member at any depth and anything after the object. The
// standard decoder keeps the last of two members silently, which is
// the confusion a signed document must not admit.
func strictObject(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) {
		return nil, ErrClientInvalid
	}
	if err := noRepeatedMembers(raw); err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, ErrClientInvalid
	}
	return object, nil
}

// noRepeatedMembers walks raw's tokens and refuses an object that
// names one member twice, or a document that is not a single object.
func noRepeatedMembers(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	type frame struct {
		object  bool
		seen    map[string]bool
		wantKey bool
	}
	var stack []*frame
	first := true
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ErrClientInvalid
		}
		if first {
			if d, ok := tok.(json.Delim); !ok || d != '{' {
				return ErrClientInvalid
			}
		} else if len(stack) == 0 {
			return ErrClientInvalid
		}
		first = false
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		if top != nil && top.object && top.wantKey {
			if d, ok := tok.(json.Delim); ok && d == '}' {
				stack = stack[:len(stack)-1]
				continue
			}
			name, ok := tok.(string)
			if !ok || top.seen[name] {
				return ErrClientInvalid
			}
			top.seen[name] = true
			top.wantKey = false
			continue
		}
		if top != nil && top.object {
			top.wantKey = true
		}
		switch d, ok := tok.(json.Delim); {
		case ok && d == '{':
			stack = append(stack, &frame{object: true, seen: map[string]bool{}, wantKey: true})
		case ok && d == '[':
			stack = append(stack, &frame{})
		case ok && (d == ']' || d == '}'):
			stack = stack[:len(stack)-1]
		}
	}
	if first || len(stack) != 0 {
		return ErrClientInvalid
	}
	return nil
}

// stringMember answers object[name] where it is a JSON string, and ""
// otherwise.
func stringMember(object map[string]json.RawMessage, name string) string {
	raw, ok := object[name]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

// dateMember answers object[name] where it is an integer NumericDate:
// a JSON number of digits alone, with no sign, fraction or exponent.
func dateMember(object map[string]json.RawMessage, name string) (time.Time, bool) {
	raw, ok := object[name]
	if !ok || !numericInt.Match(raw) {
		return time.Time{}, false
	}
	seconds, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || seconds <= 0 {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0).UTC(), true
}
