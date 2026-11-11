package identity_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/authn"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/signer"
)

const issuerName = "https://garam.test/api"

var noon = time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)

// sessions is the issuer's view of the session table: rows by access
// token and by id, which a test deletes to revoke.
type sessions struct {
	byToken map[string]*authn.Session
}

func (s *sessions) Get(_ context.Context, token string) (*authn.Session, error) {
	if row, ok := s.byToken[token]; ok {
		return row, nil
	}
	return nil, authn.ErrSessionNotFound
}

func (s *sessions) GetByID(_ context.Context, id string) (*authn.Session, error) {
	for _, row := range s.byToken {
		if row.ID == id {
			return row, nil
		}
	}
	return nil, authn.ErrSessionNotFound
}

func newKey(t *testing.T) *signer.Key {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(private)
	require.NoError(t, err)
	key, err := signer.ParseKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)
	return key
}

var ada = grn.New(grn.RootOwner, "user", "ada")

// fixture holds one live CLI session, "cli-access", an hour from
// expiry, and a browser session, "browser-cookie".
func fixture(t *testing.T) (*identity.Issuer, *signer.Key, *sessions) {
	t.Helper()
	store := &sessions{byToken: map[string]*authn.Session{
		"cli-access": {User: ada, ID: "sid-cli", Kind: authn.SessionCLI, ExpiresAt: noon.Add(time.Hour)},
		"browser-cookie": {User: ada, ID: "sid-browser", Kind: authn.SessionBrowser,
			ExpiresAt: noon.Add(time.Hour)},
	}}
	key := newKey(t)
	issuer, err := identity.NewIssuer(issuerName, key, store, func() time.Time { return noon })
	require.NoError(t, err)
	return issuer, key, store
}

func exchange(token, audience string) identity.ExchangeRequest {
	return identity.ExchangeRequest{SubjectToken: token, SubjectTokenType: identity.TokenTypeAccessToken, Audience: audience}
}

func TestExchange_ACLISessionIsAnsweredATokenNamingItsUserAndItsRow(t *testing.T) {
	issuer, key, _ := fixture(t)

	got, err := issuer.Exchange(context.Background(), exchange("cli-access", identity.AudienceCodehub))
	require.NoError(t, err)

	claims, err := key.Verify(got.Token, noon)
	require.NoError(t, err)
	assert.Equal(t, issuerName, claims.Issuer)
	assert.Equal(t, "grn:root/iam/user/ada", claims.Subject)
	assert.Equal(t, "codehub", claims.Audience)
	assert.Equal(t, "sid-cli", claims.SessionID)
	assert.NotEmpty(t, claims.JTI)
	assert.True(t, noon.Equal(claims.IssuedAt), "iat is the issuer's clock")
	assert.Equal(t, 300*time.Second, claims.Lifetime, "an hour of session buys the longest token")
	assert.Equal(t, 300, got.ExpiresIn)

	again, err := issuer.Exchange(context.Background(), exchange("cli-access", identity.AudienceCodehub))
	require.NoError(t, err)
	next, err := key.Verify(again.Token, noon)
	require.NoError(t, err)
	assert.NotEqual(t, claims.JTI, next.JTI, "every token is its own")
}

func TestExchange_ATokenNeverOutlivesItsSession(t *testing.T) {
	issuer, key, store := fixture(t)
	store.byToken["cli-access"].ExpiresAt = noon.Add(90*time.Second + 500*time.Millisecond)

	got, err := issuer.Exchange(context.Background(), exchange("cli-access", identity.AudiencePolicy))
	require.NoError(t, err)

	claims, err := key.Verify(got.Token, noon)
	require.NoError(t, err)
	assert.Equal(t, 90*time.Second, claims.Lifetime)
	assert.Equal(t, 90, got.ExpiresIn)

	store.byToken["cli-access"].ExpiresAt = noon.Add(900 * time.Millisecond)
	_, err = issuer.Exchange(context.Background(), exchange("cli-access", identity.AudiencePolicy))
	assert.ErrorIs(t, err, identity.ErrInvalidGrant, "less than a second left is no session to exchange")
}

func TestExchange_RefusesWhatIsNotALiveCLIAccessToken(t *testing.T) {
	issuer, _, store := fixture(t)

	for name, token := range map[string]string{
		"a refresh token":   "cli-refresh",
		"an ID token":       "eyJhbGciOiJSUzI1NiJ9.eyJzdWIiOiJhZGEifQ.c2ln",
		"a machine token":   "garam_ot_0123456789abcdef",
		"a browser session": "browser-cookie",
	} {
		_, err := issuer.Exchange(context.Background(), exchange(token, identity.AudiencePolicy))
		assert.ErrorIs(t, err, identity.ErrInvalidGrant, name)
	}

	delete(store.byToken, "cli-access")
	_, err := issuer.Exchange(context.Background(), exchange("cli-access", identity.AudiencePolicy))
	assert.ErrorIs(t, err, identity.ErrInvalidGrant, "a revoked or expired session")
}

func TestExchange_RefusesAnAudienceOutsideTheList(t *testing.T) {
	issuer, _, _ := fixture(t)

	for _, audience := range []string{"", "cowork", "Policy", "policy ", "https://policy"} {
		_, err := issuer.Exchange(context.Background(), exchange("cli-access", audience))
		assert.ErrorIs(t, err, identity.ErrInvalidTarget, "%q", audience)
	}
}

func TestExchange_RefusesARequestOutsideTheProfile(t *testing.T) {
	issuer, _, _ := fixture(t)

	for name, req := range map[string]identity.ExchangeRequest{
		"no subject token":      {SubjectTokenType: identity.TokenTypeAccessToken, Audience: "policy"},
		"a refresh token type":  {SubjectToken: "cli-access", SubjectTokenType: "urn:ietf:params:oauth:token-type:refresh_token", Audience: "policy"},
		"no subject token type": {SubjectToken: "cli-access", Audience: "policy"},
		"an ID token requested": {SubjectToken: "cli-access", SubjectTokenType: identity.TokenTypeAccessToken, Audience: "policy", RequestedTokenType: "urn:ietf:params:oauth:token-type:id_token"},
	} {
		_, err := issuer.Exchange(context.Background(), req)
		assert.ErrorIs(t, err, identity.ErrInvalidRequest, name)
	}

	req := exchange("cli-access", "policy")
	req.RequestedTokenType = identity.TokenTypeJWT
	_, err := issuer.Exchange(context.Background(), req)
	assert.NoError(t, err, "the JWT type may be named")
}

func TestIntrospect_AnswersForItsOwnAudienceWhileTheSessionRowLives(t *testing.T) {
	issuer, _, store := fixture(t)
	got, err := issuer.Exchange(context.Background(), exchange("cli-access", identity.AudiencePolicy))
	require.NoError(t, err)

	claims, active, err := issuer.Introspect(context.Background(), identity.AudiencePolicy, got.Token)
	require.NoError(t, err)
	assert.True(t, active)
	assert.Equal(t, "grn:root/iam/user/ada", claims.Subject)

	_, active, err = issuer.Introspect(context.Background(), identity.AudienceCodehub, got.Token)
	require.NoError(t, err)
	assert.False(t, active, "a service reads only the tokens made for it")

	delete(store.byToken, "cli-access")
	_, active, err = issuer.Introspect(context.Background(), identity.AudiencePolicy, got.Token)
	require.NoError(t, err)
	assert.False(t, active, "revoking the session is deleting the row")
}

func TestIntrospect_IsInactiveForWhatGaramDidNotIssue(t *testing.T) {
	issuer, key, store := fixture(t)
	got, err := issuer.Exchange(context.Background(), exchange("cli-access", identity.AudiencePolicy))
	require.NoError(t, err)

	_, active, err := issuer.Introspect(context.Background(), identity.AudiencePolicy, got.Token+"x")
	require.NoError(t, err)
	assert.False(t, active, "a tampered token")

	other, err := identity.NewIssuer("https://other.test/api", key, store, func() time.Time { return noon })
	require.NoError(t, err)
	foreign, err := other.Exchange(context.Background(), exchange("cli-access", identity.AudiencePolicy))
	require.NoError(t, err)
	_, active, err = issuer.Introspect(context.Background(), identity.AudiencePolicy, foreign.Token)
	require.NoError(t, err)
	assert.False(t, active, "another issuer's identity, though the key is the same")

	_, active, err = issuer.Introspect(context.Background(), identity.AudiencePolicy, "cli-access")
	require.NoError(t, err)
	assert.False(t, active, "a session token is not a signed one")

	later, err := identity.NewIssuer(issuerName, key, store, func() time.Time { return noon.Add(301 * time.Second) })
	require.NoError(t, err)
	_, active, err = later.Introspect(context.Background(), identity.AudiencePolicy, got.Token)
	require.NoError(t, err)
	assert.False(t, active, "a token past its exp")

	store.byToken["cli-access"].User = grn.New(grn.RootOwner, "user", "eve")
	_, active, err = issuer.Introspect(context.Background(), identity.AudiencePolicy, got.Token)
	require.NoError(t, err)
	assert.False(t, active, "a row that is another user's")
}

func TestParseIssuer(t *testing.T) {
	for _, ok := range []string{"https://garam.example/api", "https://garam.example:8443/api", "https://10.0.0.1/api"} {
		got, err := identity.ParseIssuer(ok)
		require.NoError(t, err, ok)
		assert.Equal(t, ok, got)
	}
	for _, bad := range []string{
		"", "http://garam.example/api", "https://garam.example", "https://garam.example/",
		"https://garam.example/api/", "https://Garam.example/api", "https://garam.example:/api",
		"https://u@garam.example/api", "https://garam.example/api?x=1", "https://garam.example/api#f",
		"https://garam.example/API", "https://garam.example/v1", "garam.example/api",
	} {
		_, err := identity.ParseIssuer(bad)
		assert.Error(t, err, bad)
	}
}
