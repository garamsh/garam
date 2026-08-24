package api_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
	"github.com/garamsh/garam/internal/user"
)

// The password these tests register with. It is a fixture of this file
// and no account outside it is created with one.
const registeredPassword = "correct-horse-battery-staple"

// inviteTo seeds an organization and a live invitation into it,
// returning the token. Registration is authorized by holding one
// (ADR-0006), so every test here starts from an invitation rather
// than from an account.
func inviteTo(t *testing.T, f *fixture, slug, token string) string {
	t.Helper()
	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, slug)
	require.NoError(t, f.orgs.CreateOrg(testCtx(t), org.NewOrganization(orgGRN, slug)))
	creator := grn.New("root", "default", user.ServiceType, "owner-sub")
	inv := org.NewInvitation(token, orgGRN, org.RoleMember, creator, time.Now().UTC().Add(time.Hour))
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t), inv))
	return token
}

// registerBody renders a registration request.
func registerBody(token, username, password string) string {
	return fmt.Sprintf(`{"invitationToken":%q,"username":%q,"password":%q}`, token, username, password)
}

// loginBody renders a sign-in request.
func loginBody(username, password string) string {
	return fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
}

// sessionCookie returns the session cookie a response set, or nil.
func sessionCookie(rec *http.Response) *http.Cookie {
	for _, c := range rec.Cookies() {
		if c.Name == "garam_session" && c.Value != "" {
			return c
		}
	}
	return nil
}

// TestRegisterPassword_CreatesTheAccountTheInvitationNames walks the
// whole route: an invitation becomes an account, a membership, and a
// session, with no provider anywhere in it.
func TestRegisterPassword_CreatesTheAccountTheInvitationNames(t *testing.T) {
	f := newFixture(t)
	token := inviteTo(t, f, "acme", "invite-tok")

	rec := f.do(t, "POST", "/auth/password/register", registerBody(token, "alice", registeredPassword))
	require.Equal(t, http.StatusCreated, rec.Code)

	m := decodeBody[apigen.Member](t, rec)
	assert.Equal(t, apigen.RoleMember, m.Role)
	// The account sits under the reserved prefix, which is what keeps
	// it and any provider's account from ever being one row.
	assert.Equal(t, "grn:root:default:user:local-alice", m.UserGrn)

	cookie := sessionCookie(rec.Result())
	require.NotNil(t, cookie, "registration signs the new account in")

	rec = f.do(t, "GET", "/me", "", cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "grn:root:default:user:local-alice", decodeBody[apigen.User](t, rec).Grn)
}

// TestLoginPassword_RoundTrip signs out and back in, which is the
// property the whole change exists for: a person signs in with a
// username and a password and gets the session an OAuth callback gives
// (ADR-0005).
func TestLoginPassword_RoundTrip(t *testing.T) {
	f := newFixture(t)
	token := inviteTo(t, f, "acme", "invite-tok")
	rec := f.do(t, "POST", "/auth/password/register", registerBody(token, "alice", registeredPassword))
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, http.StatusNoContent, f.do(t, "POST", "/auth/logout", "", sessionCookie(rec.Result())).Code)

	rec = f.do(t, "POST", "/auth/password/login", loginBody("alice", registeredPassword))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "grn:root:default:user:local-alice", decodeBody[apigen.User](t, rec).Grn)

	cookie := sessionCookie(rec.Result())
	require.NotNil(t, cookie)
	rec = f.do(t, "GET", "/me", "", cookie)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestLoginPassword_UnknownUserAndWrongPasswordAreOneAnswer is the
// defect this endpoint is most likely to introduce: a refusal that
// says which of the two was wrong hands out the account list one
// username at a time. The two responses are compared whole — status
// and body — and neither may set a session.
//
// The accepted case is the control. Without it a handler that refused
// every request identically would satisfy every assertion here.
func TestLoginPassword_UnknownUserAndWrongPasswordAreOneAnswer(t *testing.T) {
	f := newFixture(t)
	token := inviteTo(t, f, "acme", "invite-tok")
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/auth/password/register", registerBody(token, "alice", registeredPassword)).Code)

	wrongPassword := f.do(t, "POST", "/auth/password/login", loginBody("alice", "wrong-password-entirely"))
	unknownUser := f.do(t, "POST", "/auth/password/login", loginBody("mallory", "wrong-password-entirely"))

	assert.Equal(t, http.StatusUnauthorized, wrongPassword.Code)
	assert.Equal(t, unknownUser.Code, wrongPassword.Code, "one status")
	assert.Equal(t, unknownUser.Body.String(), wrongPassword.Body.String(), "one body, byte for byte")
	assert.Nil(t, sessionCookie(wrongPassword.Result()))
	assert.Nil(t, sessionCookie(unknownUser.Result()))

	// Nor does either name the username it was asked about.
	assert.NotContains(t, wrongPassword.Body.String(), "alice")
	assert.NotContains(t, unknownUser.Body.String(), "mallory")

	// The control.
	accepted := f.do(t, "POST", "/auth/password/login", loginBody("alice", registeredPassword))
	require.Equal(t, http.StatusOK, accepted.Code, "the right password is still accepted")
	require.NotNil(t, sessionCookie(accepted.Result()))
}

// TestLoginPassword_RefusalCarriesNothingThatWasSent holds the rule
// that a password is never placed in an error message (ADR-0040).
func TestLoginPassword_RefusalCarriesNothingThatWasSent(t *testing.T) {
	f := newFixture(t)
	token := inviteTo(t, f, "acme", "invite-tok")
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/auth/password/register", registerBody(token, "alice", registeredPassword)).Code)

	const sent = "swordfish-swordfish-swordfish"
	rec := f.do(t, "POST", "/auth/password/login", loginBody("alice", sent))
	require.Equal(t, http.StatusUnauthorized, rec.Code)

	body := rec.Body.String()
	// The control for the search itself: the body is a real refusal
	// with a message in it, so the absence below is the password's and
	// not an empty response's.
	require.Contains(t, body, "message")
	assert.NotContains(t, body, sent, "the password does not reach the error body")
	assert.NotContains(t, body, "swordfish")

	// The same holds for a refusal raised before any account is looked
	// up, where the password is what was wrong.
	rec = f.do(t, "POST", "/auth/password/register", registerBody(token, "bob", "short"))
	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.NotContains(t, rec.Body.String(), "short")
	assert.Contains(t, rec.Body.String(), fmt.Sprint(authn.MinPasswordLength), "it names the bound instead")
}

// TestLoginPassword_ShedsLoadPastTheLiveAttemptBound establishes that
// the bound reaches this path, by driving it rather than by reading
// the code. ADR-0030 bounds the OAuth login endpoint because it
// writes; this endpoint spends an Argon2id computation instead, and
// the bound is what decides whether that happens at all — so a request
// carrying the *right* password is shed too.
//
// Releasing one slot and repeating is the control: it shows the
// refusal tracks the attempts still live rather than the number of
// requests seen, which would refuse the last one as well.
func TestLoginPassword_ShedsLoadPastTheLiveAttemptBound(t *testing.T) {
	f := newFixture(t)
	token := inviteTo(t, f, "acme", "invite-tok")
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/auth/password/register", registerBody(token, "alice", registeredPassword)).Code)

	f.attempts.fill(authn.MaxLivePasswordAttempts)

	rec := f.do(t, "POST", "/auth/password/login", loginBody("alice", registeredPassword))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "shed before anything is hashed")
	assert.Nil(t, sessionCookie(rec.Result()))

	f.attempts.releaseOne()
	rec = f.do(t, "POST", "/auth/password/login", loginBody("alice", registeredPassword))
	assert.Equal(t, http.StatusOK, rec.Code)
}

// TestLoginPassword_ReleasesTheSlotItVerified holds the property that
// keeps the bound off ordinary traffic: only failures accumulate.
func TestLoginPassword_ReleasesTheSlotItVerified(t *testing.T) {
	f := newFixture(t)
	token := inviteTo(t, f, "acme", "invite-tok")
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/auth/password/register", registerBody(token, "alice", registeredPassword)).Code)

	require.Equal(t, http.StatusOK,
		f.do(t, "POST", "/auth/password/login", loginBody("alice", registeredPassword)).Code)
	assert.Empty(t, f.attempts.expiryOf, "a verification that succeeded holds no slot")

	require.Equal(t, http.StatusUnauthorized,
		f.do(t, "POST", "/auth/password/login", loginBody("alice", "wrong-password-entirely")).Code)
	assert.Len(t, f.attempts.expiryOf, 1, "one that failed does")
}

// TestLoginPassword_UpgradesASupersededVerifier holds the upgrade path
// end to end: a credential stored under weaker parameters still signs
// in, and is rewritten at the current policy on the way out, so the
// stored population converges without anyone being asked to reset
// (ADR-0040).
func TestLoginPassword_UpgradesASupersededVerifier(t *testing.T) {
	f := newFixture(t)
	token := inviteTo(t, f, "acme", "invite-tok")
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/auth/password/register", registerBody(token, "alice", registeredPassword)).Code)

	alice := grn.New("root", "default", user.ServiceType, "local-alice")
	superseded := supersededVerifier(t, registeredPassword)
	require.NoError(t, f.credentials.Replace(testCtx(t), alice, superseded))
	require.False(t, f.credentials.held(alice).AtCurrentPolicy())

	require.Equal(t, http.StatusOK,
		f.do(t, "POST", "/auth/password/login", loginBody("alice", registeredPassword)).Code)

	held := f.credentials.held(alice)
	assert.NotEqual(t, superseded, held, "the verifier was rewritten")
	assert.True(t, held.AtCurrentPolicy(), "at the current parameters")
	assert.NoError(t, held.Match(registeredPassword), "and still the same password")
}

func TestRegisterPassword_RefusesWithoutALiveInvitation(t *testing.T) {
	f := newFixture(t)
	// The control: with a live invitation the same request is accepted,
	// so the refusals below are the invitation's state and nothing else.
	live := inviteTo(t, f, "acme", "live-tok")
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/auth/password/register", registerBody(live, "alice", registeredPassword)).Code)

	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, "acme")
	creator := grn.New("root", "default", user.ServiceType, "owner-sub")
	expired := org.NewInvitation("expired-tok", orgGRN, org.RoleMember, creator, time.Now().UTC().Add(-time.Hour))
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t), expired))

	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{"unknown", "no-such-token", http.StatusNotFound},
		{"expired", "expired-tok", http.StatusConflict},
		{"already accepted", "live-tok", http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.do(t, "POST", "/auth/password/register", registerBody(tc.token, "bob", registeredPassword))
			assert.Equal(t, tc.want, rec.Code)
			assert.Nil(t, sessionCookie(rec.Result()))
		})
	}
}

func TestRegisterPassword_RefusesAUsernameThatIsTakenOrUnusable(t *testing.T) {
	f := newFixture(t)
	first := inviteTo(t, f, "acme", "first-tok")
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/auth/password/register", registerBody(first, "alice", registeredPassword)).Code)

	orgGRN := grn.New("root", "default", org.ServiceTypeOrg, "acme")
	creator := grn.New("root", "default", user.ServiceType, "owner-sub")
	second := org.NewInvitation("second-tok", orgGRN, org.RoleMember, creator, time.Now().UTC().Add(time.Hour))
	require.NoError(t, f.orgs.CreateInvitation(testCtx(t), second))

	rec := f.do(t, "POST", "/auth/password/register", registerBody("second-tok", "alice", registeredPassword))
	assert.Equal(t, http.StatusConflict, rec.Code, "the username is taken")

	for _, username := range []string{"", "Alice", "al ice", "alice!", strings.Repeat("a", 64)} {
		rec = f.do(t, "POST", "/auth/password/register", registerBody("second-tok", username, registeredPassword))
		assert.Equal(t, http.StatusBadRequest, rec.Code, "username %q cannot name an account", username)
	}
}

// TestOauthCallback_RefusesAProviderSubjectInTheReservedSpace is the
// other half of the collision guard: a provider that asserts a subject
// in the space local accounts are minted from would otherwise be
// handed that account's row (ADR-0040).
func TestOauthCallback_RefusesAProviderSubjectInTheReservedSpace(t *testing.T) {
	f := newFixture(t)
	token := inviteTo(t, f, "acme", "invite-tok")
	require.Equal(t, http.StatusCreated,
		f.do(t, "POST", "/auth/password/register", registerBody(token, "alice", registeredPassword)).Code)

	// The control: an ordinary subject completes the callback, so the
	// refusal below is the reserved prefix and not the flow.
	f.provider.identity = &authn.Identity{Subject: "ordinary-sub", Nickname: "Ordinary"}
	require.Equal(t, http.StatusFound, callbackFor(t, f).Code)

	f.provider.identity = &authn.Identity{Subject: "local-alice", Nickname: "Not Alice"}
	rec := callbackFor(t, f)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, sessionCookie(rec.Result()))

	// Alice's account is untouched: her password still signs her in.
	require.Equal(t, http.StatusOK,
		f.do(t, "POST", "/auth/password/login", loginBody("alice", registeredPassword)).Code)
}

// callbackFor drives one complete OAuth login and callback, returning
// the callback's response.
func callbackFor(t *testing.T, f *fixture) *httptest.ResponseRecorder {
	t.Helper()
	login := f.do(t, "GET", "/auth/login/google", "")
	require.Equal(t, http.StatusFound, login.Code)
	var state, verifier *http.Cookie
	for _, c := range login.Result().Cookies() {
		switch c.Name {
		case "garam_oauth_state":
			state = c
		case "garam_oauth_verifier":
			verifier = c
		}
	}
	require.NotNil(t, state)
	require.NotNil(t, verifier)
	return f.do(t, "GET", "/auth/callback/google?code=c&state="+state.Value, "", state, verifier)
}

// supersededVerifier builds the verifier a weaker policy would have
// stored, computing the tag here rather than through the code under
// test.
func supersededVerifier(t *testing.T, p string) authn.Verifier {
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
