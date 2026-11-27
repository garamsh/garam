package machineapi_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/authn"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/signer"
)

const (
	principalIssuer = "https://garam.test/api"
)

var coworkPrincipal = identity.PrincipalGRN("acme", identity.ServiceCowork)

// spendingAdmitter is the store's seam: it records what it admitted.
type spendingAdmitter struct{ admitted []grn.GRN }

func (a *spendingAdmitter) Admit(_ context.Context, principal grn.GRN, _ operator.VerifiedAssertion) error {
	a.admitted = append(a.admitted, principal)
	return nil
}

// AdmitInstallation admits an installation whose app lists the policy
// service alone, as a record listing it would.
func (a *spendingAdmitter) AdmitInstallation(_ context.Context, installation grn.GRN, audience string, _ operator.VerifiedAssertion) error {
	if audience != identity.AudiencePolicy {
		return identity.ErrAudienceNotListed
	}
	a.admitted = append(a.admitted, installation)
	return nil
}

type noSessions struct{}

func (noSessions) Get(context.Context, string) (*authn.Session, error) {
	return nil, authn.ErrSessionNotFound
}
func (noSessions) GetByID(context.Context, string) (*authn.Session, error) {
	return nil, authn.ErrSessionNotFound
}

// principalListener serves the exchange for the principal of cowork,
// whose key private is, with the issuer on or off, and answers the
// store it admits through and the key the issuer signs with.
func principalListener(t *testing.T, private *rsa.PrivateKey, issuerOn bool) (string, *http.Client, *spendingAdmitter, *signer.Key) {
	t.Helper()
	return keyedListener(t, identity.ServiceCowork, private, issuerOn)
}

// keyedListener serves the exchange with private's public half the one
// key of the set named set, as principalListener does.
func keyedListener(t *testing.T, set string, private *rsa.PrivateKey, issuerOn bool) (string, *http.Client, *spendingAdmitter, *signer.Key) {
	t.Helper()
	pkcs8, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(pkcs8)
	require.NoError(t, err)
	key, err := signer.ParseKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)

	store := &spendingAdmitter{}
	principals := identity.NewPrincipals(identity.PrincipalsConfig{
		Keys: identity.FixedKeys{set: {
			{KID: kid, Algorithm: operator.KeyAlgorithm, Public: &private.PublicKey},
		}},
		Issuer:   principalIssuer,
		Admitter: store,
	})
	var issuer *identity.Issuer
	if issuerOn {
		issuer, err = identity.NewIssuer(principalIssuer, key, noSessions{}, nil)
		require.NoError(t, err)
	}
	srv := machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil).WithIdentity(issuer, principals)
	base, _ := serveTLS(t, srv, assertingClient)
	return base, anonymous(t), store, key
}

func principalForm(t *testing.T, private *rsa.PrivateKey, audience string) url.Values {
	t.Helper()
	return clientForm(t, private, coworkPrincipal, audience)
}

// clientForm is the exchange's form for client, its assertion signed
// by private, as principalForm's is for cowork's principal.
func clientForm(t *testing.T, private *rsa.PrivateKey, principal grn.GRN, audience string) url.Values {
	t.Helper()
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	signing, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private},
		(&jose.SignerOptions{}).WithType(operator.TypeClientAuth).WithHeader("kid", kid))
	require.NoError(t, err)
	raw := make([]byte, 32)
	_, err = rand.Read(raw)
	require.NoError(t, err)
	client := principal.String()
	payload, err := json.Marshal(map[string]any{
		"iss": client, "sub": client, "aud": principalIssuer,
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
		"jti": base64.RawURLEncoding.EncodeToString(raw),
	})
	require.NoError(t, err)
	object, err := signing.Sign(payload)
	require.NoError(t, err)
	assertion, err := object.CompactSerialize()
	require.NoError(t, err)
	form := url.Values{
		"grant_type": {operator.GrantTypeClientCredentials}, "client_id": {client},
		"client_assertion_type": {operator.ClientAssertionTypeJWT}, "client_assertion": {assertion},
	}
	if audience != "" {
		form.Set("audience", audience)
	}
	return form
}

// TestExchangeClientAssertion_AnswersAPrincipalTheIssuersTokenForPolicy
// is the principal's path from the wire (ADR-0129): a principal whose
// assertion verifies is answered the issuer's token for the policy
// service in the standard token form, uncached, under machine-token.v2.
func TestExchangeClientAssertion_AnswersAPrincipalTheIssuersTokenForPolicy(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, operator.MinKeyBits)
	require.NoError(t, err)
	base, client, store, key := principalListener(t, private, true)

	resp, body := exchangeForm(t, client, base, principalForm(t, private, identity.AudiencePolicy).Encode(), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	assert.Equal(t, operator.AssertionContractVersion, resp.Header.Get(operator.ContractVersionHeader))
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	assert.Equal(t, "Bearer", token.TokenType)
	assert.Equal(t, 300, token.ExpiresIn)
	claims, err := key.Verify(token.AccessToken, time.Now())
	require.NoError(t, err)
	assert.Equal(t, coworkPrincipal.String(), claims.Subject)
	assert.Equal(t, identity.AudiencePolicy, claims.Audience)
	assert.Equal(t, []grn.GRN{coworkPrincipal}, store.admitted)
}

// TestExchangeClientAssertion_RefusesAPrincipalBeforeSpendingItsAssertion
// holds that a request with an audience other than policy is refused
// before the assertion is read, so it spends no jti, and that a
// process with the issuer off answers 503 and issues nothing.
func TestExchangeClientAssertion_RefusesAPrincipalBeforeSpendingItsAssertion(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, operator.MinKeyBits)
	require.NoError(t, err)
	base, client, store, _ := principalListener(t, private, true)

	for name, audience := range map[string]string{
		"another audience": identity.AudienceCodehub, "an unknown one": "cowork", "none": "", "a GRN": "grn:acme/iam/operator/control",
	} {
		resp, body := exchangeForm(t, client, base, principalForm(t, private, audience).Encode(), nil)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, name+": "+body)
		assert.Equal(t, "invalid_request", oauthError(t, body), name)
	}
	assert.Empty(t, store.admitted, "a refused request spends nothing")

	off, offClient, offStore, _ := principalListener(t, private, false)
	resp, body := exchangeForm(t, offClient, off, principalForm(t, private, identity.AudiencePolicy).Encode(), nil)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, body)
	assert.Equal(t, "temporarily_unavailable", oauthError(t, body))
	assert.Empty(t, offStore.admitted)
}

// TestExchangeClientAssertion_AnswersAnInstallationTheIssuersTokenOfItsAudience
// is the app key's path from the wire (ADR-0192 decisions 5 and 7): an
// installation whose assertion its app's key signed is answered the
// issuer's token, `sub` the installation, of an audience its app lists.
// An audience it does not list is refused once the signature verified;
// one the issuer does not serve, or none, before the assertion is
// read, spending nothing.
func TestExchangeClientAssertion_AnswersAnInstallationTheIssuersTokenOfItsAudience(t *testing.T) {
	private, err := rsa.GenerateKey(rand.Reader, operator.MinKeyBits)
	require.NoError(t, err)
	base, client, store, key := keyedListener(t, "deployer", private, true)
	installation := grn.New("acme", "installation", "deployer")

	resp, body := exchangeForm(t, client, base, clientForm(t, private, installation, identity.AudiencePolicy).Encode(), nil)

	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	var token struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	claims, err := key.Verify(token.AccessToken, time.Now())
	require.NoError(t, err)
	assert.Equal(t, installation.String(), claims.Subject)
	assert.Equal(t, identity.AudiencePolicy, claims.Audience)
	assert.Empty(t, claims.SessionID)
	assert.Equal(t, []grn.GRN{installation}, store.admitted)

	resp, body = exchangeForm(t, client, base, clientForm(t, private, installation, identity.AudienceCodehub).Encode(), nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
	assert.Equal(t, "invalid_request", oauthError(t, body), "an audience its app does not list")
	for name, audience := range map[string]string{"an unknown one": "cowork", "none": ""} {
		resp, body := exchangeForm(t, client, base, clientForm(t, private, installation, audience).Encode(), nil)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, name+": "+body)
		assert.Equal(t, "invalid_request", oauthError(t, body), name)
	}
	assert.Len(t, store.admitted, 1, "no refused request was admitted")

	off, offClient, _, _ := keyedListener(t, "deployer", private, false)
	resp, body = exchangeForm(t, offClient, off, clientForm(t, private, installation, identity.AudiencePolicy).Encode(), nil)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, body)
}
