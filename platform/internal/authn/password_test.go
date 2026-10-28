package authn_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/authn"
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
		// The two shapes that read as parameters and are not: argon2
		// panics on either below one, so an unrefused row answers a
		// sign-in with a 500 rather than with a refusal.
		{"zero iterations", "$argon2id$v=19$m=19456,t=0,p=1$c2FsdHNhbHRzYWx0c2E$dGFndGFndGFndGFn"},
		{"zero lanes", "$argon2id$v=19$m=19456,t=2,p=0$c2FsdHNhbHRzYWx0c2E$dGFndGFndGFndGFn"},
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

	// Both bounds are inclusive, and each is asserted at the bound
	// rather than only past it: a ceiling that refused what it names
	// would refuse a password the API specification accepts, and the
	// case a byte over cannot tell that apart.
	_, err = authn.NewVerifier(authn.Password(strings.Repeat("a", authn.MaxPasswordLength)))
	assert.NoError(t, err)

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

// TestRejectAbsent_DerivesAsAStoredVerifierDoes holds the claim the
// refusal above rests on: the unknown-username path runs the Argon2id
// derivation rather than returning early, so it costs what a wrong
// password costs. Argon2id's cost is the memory it fills, which the
// policy states in KiB and every call allocates, so what the call
// allocated reports whether the derivation was reached (ADR-0041).
func TestRejectAbsent_DerivesAsAStoredVerifierDoes(t *testing.T) {
	// The absent verifier is derived on the first call and kept; the
	// readings below are of a steady-state call, not of that one.
	require.ErrorIs(t, authn.RejectAbsent(testPassword), authn.ErrPasswordRejected)

	stored, err := authn.NewVerifier(testPassword)
	require.NoError(t, err)
	floor := memoryPerDerivation(t, stored) / 2

	// The control: the same refusal with the derivation taken out of
	// it. Without it the floor could be reading allocation the call
	// makes for other reasons rather than the work.
	withoutTheWork := func() error { return authn.ErrPasswordRejected }
	assert.Less(t, bytesAllocated(func() { _ = withoutTheWork() }), floor,
		"a refusal that derives nothing falls below the floor")

	assert.GreaterOrEqual(t, bytesAllocated(func() { _ = authn.RejectAbsent(testPassword) }), floor,
		"the unknown-username path fills Argon2id's memory")
	assert.GreaterOrEqual(t, bytesAllocated(func() { _ = stored.Match("something-else-entirely") }), floor,
		"as does the wrong-password path it has to be indistinguishable from")
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

// memoryPerDerivation reports, in bytes, the memory one derivation
// under v's parameters has to fill. It is read from v rather than
// written here, so raising the policy moves the expectation with it.
func memoryPerDerivation(t *testing.T, v authn.Verifier) uint64 {
	t.Helper()
	fields := strings.Split(string(v), "$")
	require.Len(t, fields, 6, "a verifier carries its parameters")
	var kib uint64
	_, err := fmt.Sscanf(fields[3], "m=%d", &kib)
	require.NoError(t, err)
	require.NotZero(t, kib)
	return kib * 1024
}

// bytesAllocated reports what f allocated. TotalAlloc only rises, so
// anything allocated elsewhere in the process inflates the reading:
// every assertion on it is a floor, and the one ceiling is the control,
// which has the same margin to spare.
func bytesAllocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}
