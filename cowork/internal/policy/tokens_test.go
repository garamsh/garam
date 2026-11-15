package policy_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/policy"
	"github.com/garamsh/garam/cowork/internal/policy/policytest"
	"github.com/garamsh/garam/lib/errorx"
)

// clock is a settable time source.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time { return c.now }

func tokens(t *testing.T, endpoint *policytest.Endpoint, keyFile string, c *clock) *policy.Tokens {
	t.Helper()
	tk, err := policy.NewTokens(policy.Config{TokenURL: endpoint.URL, Issuer: endpoint.Issuer, KeyFile: keyFile}, c.Now)
	require.NoError(t, err)
	return tk
}

func TestTokens_AsksOncePerOrganizationAndAgainAfterExpiry(t *testing.T) {
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)
	endpoint.ExpiresIn = 120
	c := &clock{now: time.Unix(1_800_000_000, 0)}
	tk := tokens(t, endpoint, keyFile, c)
	ctx := context.Background()

	first, err := tk.Token(ctx, "acme")
	require.NoError(t, err)
	again, err := tk.Token(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, first, again)
	assert.Len(t, endpoint.Exchanges(), 1, "a good token is not asked for again")

	other, err := tk.Token(ctx, "globex")
	require.NoError(t, err)
	assert.NotEqual(t, first, other, "each organization has its own token")
	assert.Len(t, endpoint.Exchanges(), 2)

	// Still good a little short of its expiry, less the margin.
	c.now = c.now.Add(80 * time.Second)
	again, err = tk.Token(ctx, "acme")
	require.NoError(t, err)
	assert.Equal(t, first, again)

	// Past expires_in less the margin it is asked for again, and never
	// handed out past its expiry.
	c.now = c.now.Add(11 * time.Second)
	renewed, err := tk.Token(ctx, "acme")
	require.NoError(t, err)
	assert.NotEqual(t, first, renewed)
	assert.Len(t, endpoint.Exchanges(), 3)
}

func TestTokens_AssertionCarriesTheClaimsTheEndpointRequires(t *testing.T) {
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)
	c := &clock{now: time.Unix(1_800_000_000, 0)}
	tk := tokens(t, endpoint, keyFile, c)

	_, err := tk.Token(context.Background(), "acme")
	require.NoError(t, err)
	_, err = tk.Token(context.Background(), "globex")
	require.NoError(t, err)

	ex := endpoint.Exchanges()
	require.Len(t, ex, 2)
	first := ex[0]
	assert.Equal(t, "acme", first.Org)
	assert.Equal(t, "grn:acme/iam/service/cowork", policy.ClientID("acme"))
	assert.Equal(t, map[string]string{
		"grant_type":            "client_credentials",
		"client_id":             "grn:acme/iam/service/cowork",
		"client_assertion_type": "urn:ietf:params:oauth:client-assertion-type:jwt-bearer",
		"client_assertion":      first.Form["client_assertion"],
		"audience":              "policy",
	}, first.Form)
	assert.Equal(t, map[string]any{"alg": "RS256", "kid": policytest.Thumbprint(pub), "typ": "client-authentication+jwt"}, first.Header)
	assert.Equal(t, "grn:acme/iam/service/cowork", first.Claims["iss"])
	assert.Equal(t, "grn:acme/iam/service/cowork", first.Claims["sub"])
	assert.Equal(t, endpoint.Issuer, first.Claims["aud"], "the issuer identifier, as a string and alone")
	assert.NotEqual(t, endpoint.URL, first.Claims["aud"], "control: the token URL is not the audience")
	assert.EqualValues(t, 1_800_000_000, first.Claims["iat"])
	assert.EqualValues(t, 1_800_000_060, first.Claims["exp"])
	assert.NotEqual(t, first.Claims["jti"], ex[1].Claims["jti"], "each assertion has a fresh jti")
}

func TestTokens_ReadsTheKeyPerExchangeAndRefusesOneOthersCanReach(t *testing.T) {
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)
	c := &clock{now: time.Unix(1_800_000_000, 0)}
	tk := tokens(t, endpoint, keyFile, c)
	ctx := context.Background()

	_, err := tk.Token(ctx, "acme")
	require.NoError(t, err, "control: an owner-only key is accepted")

	require.NoError(t, os.Chmod(keyFile, 0o640))
	_, err = tk.Token(ctx, "globex")
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
	assert.Len(t, endpoint.Exchanges(), 1, "nothing is asked with a key others can read")
}

func TestNewTokens_RefusesAConfigNamingNoIssuer(t *testing.T) {
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)

	_, err := policy.NewTokens(policy.Config{TokenURL: endpoint.URL, Issuer: endpoint.Issuer, KeyFile: keyFile}, nil)
	require.NoError(t, err, "control: the same config naming its issuer is accepted")

	_, err = policy.NewTokens(policy.Config{TokenURL: endpoint.URL, KeyFile: keyFile}, nil)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

func TestTokens_AnUnreachableEndpointIsUnavailable(t *testing.T) {
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)
	c := &clock{now: time.Unix(1_800_000_000, 0)}
	tk, err := policy.NewTokens(policy.Config{TokenURL: "http://127.0.0.1:1/oauth/token", Issuer: endpoint.Issuer, KeyFile: keyFile}, c.Now)
	require.NoError(t, err)

	_, err = tk.Token(context.Background(), "acme")
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
	assert.Empty(t, endpoint.Exchanges())
}
