package authn

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"

	"github.com/garamsh/garam/internal/errorx"
)

// Password is a password as a person typed it. Every way the standard
// library renders a value renders this one as a redaction marker, so a
// password that reaches a log line, a formatted message, or a response
// body carries nothing (ADR-0041). The underlying string is reachable
// only by an explicit conversion, which is what hashing does.
type Password string

// redacted is what a Password renders as, in every form.
const redacted = "[redacted]"

func (Password) String() string               { return redacted }
func (Password) GoString() string             { return redacted }
func (Password) LogValue() slog.Value         { return slog.StringValue(redacted) }
func (Password) MarshalJSON() ([]byte, error) { return []byte(`"` + redacted + `"`), nil }

// Verifier is the stored form of a password: a PHC string carrying the
// Argon2id parameters the tag was computed under, so verification reads
// its cost from the value it is checking rather than from the current
// policy (ADR-0041).
type Verifier string

// argon2idParams are the Argon2id cost parameters a verifier is
// computed under. Memory is in KiB, as argon2.IDKey takes it.
type argon2idParams struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

// currentPolicy is what NewVerifier writes: OWASP's first-choice
// Argon2id setting, read from the Password Storage Cheat Sheet on
// 2026-08-25 — "a minimum configuration of 19 MiB of memory, an
// iteration count of 2, and 1 degree of parallelism". Raising it
// invalidates no stored verifier; each carries the parameters it was
// made with, and a sign-in against any other setting re-hashes
// (ADR-0041).
var currentPolicy = argon2idParams{memory: 19456, iterations: 2, parallelism: 1}

const (
	// saltLength and tagLength are RFC 9106 §4's 128-bit salt and
	// 256-bit tag, in bytes.
	saltLength = 16
	tagLength  = 32

	// MinPasswordLength is the fewest characters a password may carry.
	// OWASP's Authentication Cheat Sheet, read on 2026-08-25, holds
	// that without MFA "passwords shorter than 15 characters are
	// considered to be weak", and garam has no second factor.
	MinPasswordLength = 15
	// MaxPasswordLength bounds what one request can ask to be hashed.
	// The same page asks for at least 64 characters "to allow
	// passphrases"; this is the byte ceiling above it, so a passphrase
	// fits and an unbounded body does not.
	MaxPasswordLength = 128
)

// ErrPasswordRejected is the single answer to a password that does not
// verify and to a username no credential belongs to. Telling the two
// apart would publish the account list, so one error covers both and
// names neither (ADR-0041).
var ErrPasswordRejected = errorx.New(errorx.KindUnauthenticated, "username or password is incorrect")

// ErrPasswordTooShort and ErrPasswordTooLong refuse a password at
// registration. Neither carries what was sent.
var (
	ErrPasswordTooShort = errorx.New(errorx.KindInvalidArgument, fmt.Sprintf("password must be at least %d characters", MinPasswordLength))
	ErrPasswordTooLong  = errorx.New(errorx.KindInvalidArgument, fmt.Sprintf("password must be at most %d bytes", MaxPasswordLength))
)

// NewVerifier computes the verifier for p under the current policy,
// with a fresh salt. It refuses a password outside the accepted
// length; the refusal names the bound, never the password.
func NewVerifier(p Password) (Verifier, error) {
	// Runes for the floor and bytes for the ceiling: the floor is about
	// how much a person chose, the ceiling about how much this process
	// agrees to hash.
	if utf8.RuneCountInString(string(p)) < MinPasswordLength {
		return "", ErrPasswordTooShort
	}
	if len(p) > MaxPasswordLength {
		return "", ErrPasswordTooLong
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", errorx.Wrap(err, "password salt")
	}
	return encodeVerifier(currentPolicy, salt, deriveTag(p, salt, currentPolicy)), nil
}

// Match reports whether p produced v, answering ErrPasswordRejected
// when it did not and when v cannot be read at all — a caller learns
// nothing from the difference.
func (v Verifier) Match(p Password) error {
	params, salt, want, err := decodeVerifier(v)
	if err != nil {
		return err
	}
	// Length taken from the stored tag, so a verifier written under a
	// different tag size still verifies under its own.
	got := argon2.IDKey([]byte(p), salt, params.iterations, params.memory, params.parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrPasswordRejected
	}
	return nil
}

// AtCurrentPolicy reports whether v was computed under the parameters
// NewVerifier writes now. A verifier that is not is re-hashed after the
// sign-in it verified, which is what converges the stored population
// onto one setting without asking anyone to reset (ADR-0041). It reads
// false for a verifier it cannot decode, which Match has already
// refused.
func (v Verifier) AtCurrentPolicy() bool {
	params, _, _, err := decodeVerifier(v)
	return err == nil && params == currentPolicy
}

// absentVerifier is the verifier an unknown username does not have. It
// is derived once from random bytes: no password produces it, nothing
// accepts it, and it is not a credential.
var absentVerifier = sync.OnceValues(func() (Verifier, error) {
	var b [tagLength]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errorx.Wrap(err, "absent verifier")
	}
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", errorx.Wrap(err, "absent verifier salt")
	}
	p := Password(b[:])
	return encodeVerifier(currentPolicy, salt, deriveTag(p, salt, currentPolicy)), nil
})

// RejectAbsent spends the computation a stored credential would have
// spent and then refuses, so a username no credential belongs to costs
// what a wrong password costs and the two are refused alike
// (ADR-0041).
func RejectAbsent(p Password) error {
	v, err := absentVerifier()
	if err != nil {
		return err
	}
	// The outcome is discarded on purpose: the work is the point, and
	// the answer is ErrPasswordRejected either way.
	_ = v.Match(p)
	return ErrPasswordRejected
}

// deriveTag computes the Argon2id tag for p under params and salt.
func deriveTag(p Password, salt []byte, params argon2idParams) []byte {
	return argon2.IDKey([]byte(p), salt, params.iterations, params.memory, params.parallelism, tagLength)
}

// encodeVerifier renders the PHC string for a computed tag.
func encodeVerifier(params argon2idParams, salt, tag []byte) Verifier {
	return Verifier(fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, params.memory, params.iterations, params.parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(tag)))
}

// decodeVerifier reads a PHC string back into the parameters, salt and
// tag it carries. Every malformed shape answers ErrPasswordRejected,
// so a corrupted row refuses a sign-in rather than admitting one.
func decodeVerifier(v Verifier) (argon2idParams, []byte, []byte, error) {
	var params argon2idParams
	fields := strings.Split(string(v), "$")
	if len(fields) != 6 || fields[0] != "" || fields[1] != "argon2id" {
		return params, nil, nil, ErrPasswordRejected
	}
	var version int
	if _, err := fmt.Sscanf(fields[2], "v=%d", &version); err != nil || version != argon2.Version {
		return params, nil, nil, ErrPasswordRejected
	}
	if _, err := fmt.Sscanf(fields[3], "m=%d,t=%d,p=%d", &params.memory, &params.iterations, &params.parallelism); err != nil {
		return params, nil, nil, ErrPasswordRejected
	}
	salt, err := base64.RawStdEncoding.DecodeString(fields[4])
	if err != nil {
		return params, nil, nil, ErrPasswordRejected
	}
	tag, err := base64.RawStdEncoding.DecodeString(fields[5])
	if err != nil || len(tag) == 0 {
		return params, nil, nil, ErrPasswordRejected
	}
	return params, salt, tag, nil
}
