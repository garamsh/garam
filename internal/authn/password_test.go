package authn_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"

	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/errorx"
)

// A password long enough to satisfy the floor, used wherever the test
// needs a valid one. It is a test fixture and no account anywhere is
// created with it.
const testPassword = authn.Password("correct-horse-battery-staple")

func TestNewVerifier_MatchesItsOwnPasswordAndNoOther(t *testing.T) {
	v, err := authn.NewVerifier(testPassword)
	require.NoError(t, err)

	// The control: without it, a Match that refused everything would
	// pass the refusal below.
	assert.NoError(t, v.Match(testPassword), "the password it was built from verifies")
	assert.ErrorIs(t, v.Match("correct-horse-battery-stapl"), authn.ErrPasswordRejected)
}

func TestNewVerifier_SaltsEachVerifier(t *testing.T) {
	first, err := authn.NewVerifier(testPassword)
	require.NoError(t, err)
	second, err := authn.NewVerifier(testPassword)
	require.NoError(t, err)

	assert.NotEqual(t, first, second, "one password twice is two verifiers")
	assert.NoError(t, second.Match(testPassword))
}

// TestNewVerifier_CarriesTheParametersItUsed holds the property the
// upgrade path rests on: the stored form says what it was computed
// under, so verification reads its cost from the value rather than
// from whatever the policy has since become (ADR-0041).
func TestNewVerifier_CarriesTheParametersItUsed(t *testing.T) {
	v, err := authn.NewVerifier(testPassword)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(string(v), "$argon2id$"), "names the algorithm")
	assert.Contains(t, string(v), fmt.Sprintf("$m=%d,t=%d,p=%d$", 19456, 2, 1),
		"carries OWASP's 19 MiB, two iterations, one lane")
	assert.True(t, v.AtCurrentPolicy())
}

// TestVerifier_MatchUnderSuperseded is the upgrade path's whole claim:
// raising the parameters invalidates no stored verifier. The verifier
// here is built the way a weaker policy would have built it — by this
// test, not by the code under test, so it stands even if the encoder
// changes — and it must still verify while reading as not current.
func TestVerifier_MatchUnderSupersededParameters(t *testing.T) {
	superseded := supersededVerifier(t, testPassword)

	assert.NoError(t, superseded.Match(testPassword), "an older verifier still verifies")
	assert.ErrorIs(t, superseded.Match("correct-horse-battery-stapl"), authn.ErrPasswordRejected)
	assert.False(t, superseded.AtCurrentPolicy(), "and is reported as due a re-hash")
}

func TestVerifier_Match_RefusesAMalformedVerifier(t *testing.T) {
	fresh, err := authn.NewVerifier(testPassword)
	require.NoError(t, err)
	// The control: the same password against a well-formed verifier is
	// accepted, so the refusals below are the malformation and not the
	// password.
	require.NoError(t, fresh.Match(testPassword))

	for _, tc := range []struct {
		name     string
		verifier authn.Verifier
	}{
		{"empty", ""},
		{"not phc", "argon2id-19456-2-1"},
		{"wrong algorithm", "$argon2i$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2E$dGFndGFndGFndGFn"},
		{"unreadable parameters", "$argon2id$v=19$m=x,t=y,p=z$c2FsdHNhbHRzYWx0c2E$dGFndGFndGFndGFn"},
		{"truncated", "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2E"},
		{"empty tag", "$argon2id$v=19$m=19456,t=2,p=1$c2FsdHNhbHRzYWx0c2E$"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.ErrorIs(t, tc.verifier.Match(testPassword), authn.ErrPasswordRejected)
		})
	}
}

func TestNewVerifier_RefusesAPasswordOutsideTheLengthBounds(t *testing.T) {
	// The control: a password inside the bounds is accepted, so the
	// refusals are the bounds and not a verifier that refuses all.
	_, err := authn.NewVerifier(testPassword)
	require.NoError(t, err)

	short := authn.Password(strings.Repeat("a", authn.MinPasswordLength-1))
	_, err = authn.NewVerifier(short)
	assert.ErrorIs(t, err, authn.ErrPasswordTooShort)

	long := authn.Password(strings.Repeat("a", authn.MaxPasswordLength+1))
	_, err = authn.NewVerifier(long)
	assert.ErrorIs(t, err, authn.ErrPasswordTooLong)

	// The floor counts characters rather than bytes, so a passphrase in
	// a multi-byte script is measured as the person wrote it.
	_, err = authn.NewVerifier(authn.Password(strings.Repeat("가", authn.MinPasswordLength)))
	assert.NoError(t, err)
}

// TestNewVerifier_LengthRefusalNamesTheBoundNotThePassword holds the
// rule for the errors raised before any verifier exists: a refusal
// says what was wrong without echoing what was sent (ADR-0041).
func TestNewVerifier_LengthRefusalNamesTheBoundNotThePassword(t *testing.T) {
	secret := authn.Password(strings.Repeat("hunter2-", 20))
	require.Greater(t, len(secret), authn.MaxPasswordLength, "long enough to be refused")

	_, err := authn.NewVerifier(secret)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "hunter2", "the refusal does not echo the password")
	assert.Contains(t, err.Error(), fmt.Sprint(authn.MaxPasswordLength), "it names the bound instead")
}

// TestPassword_RendersRedactedEverywhere is what makes "never logged,
// never returned, never in an error message" a property of the type
// rather than a rule a reviewer has to enforce (ADR-0041).
func TestPassword_RendersRedactedEverywhere(t *testing.T) {
	secret := authn.Password("hunter2-hunter2-hunter2")

	assert.NotContains(t, secret.String(), "hunter2")
	assert.NotContains(t, fmt.Sprintf("%v", secret), "hunter2")
	assert.NotContains(t, fmt.Sprintf("%s", secret), "hunter2")
	assert.NotContains(t, fmt.Sprintf("%q", secret), "hunter2")
	assert.NotContains(t, fmt.Sprintf("%#v", secret), "hunter2")

	encoded, err := json.Marshal(map[string]authn.Password{"password": secret})
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "hunter2")

	// What a logger actually writes, through both handlers the stdlib
	// ships: the rule is that a password does not reach a log line, so
	// the log line is what the assertion reads.
	for name, handler := range map[string]func(*bytes.Buffer) slog.Handler{
		"json": func(b *bytes.Buffer) slog.Handler { return slog.NewJSONHandler(b, nil) },
		"text": func(b *bytes.Buffer) slog.Handler { return slog.NewTextHandler(b, nil) },
	} {
		var buf bytes.Buffer
		slog.New(handler(&buf)).Info("password attempt", "password", secret)
		assert.NotContains(t, buf.String(), "hunter2", "%s handler", name)
		assert.Contains(t, buf.String(), "password attempt", "%s handler wrote the line", name)
	}

	// The control: the value is still reachable where it has to be, so
	// the assertions above measure the rendering and not an empty type.
	assert.Equal(t, "hunter2-hunter2-hunter2", string(secret))
}

// TestRejectAbsent_RefusesAsAStoredVerifierDoes holds that the
// unknown-username path answers with the same error a wrong password
// answers with, which is what keeps the two indistinguishable.
func TestRejectAbsent_RefusesAsAStoredVerifierDoes(t *testing.T) {
	v, err := authn.NewVerifier(testPassword)
	require.NoError(t, err)

	assert.ErrorIs(t, authn.RejectAbsent(testPassword), authn.ErrPasswordRejected)
	assert.ErrorIs(t, v.Match("something-else-entirely"), authn.ErrPasswordRejected)
	assert.True(t, errorx.IsKind(authn.RejectAbsent(testPassword), errorx.KindUnauthenticated))
}

// supersededVerifier builds the verifier a weaker policy would have
// stored, computing the tag here rather than through the code under
// test.
func supersededVerifier(t *testing.T, p authn.Password) authn.Verifier {
	t.Helper()
	const (
		memory      = 7168
		iterations  = 5
		parallelism = 1
	)
	salt := []byte("sixteen-byte-slt")
	tag := argon2.IDKey([]byte(p), salt, iterations, memory, parallelism, 32)
	return authn.Verifier(fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, memory, iterations, parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(tag)))
}
