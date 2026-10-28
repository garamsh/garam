package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
)

// testSource is the client address every exchange here arrives from.
const testSource = "192.0.2.10"

// keyService is the operator service over the store alone, configured
// with the token endpoint the listeners here are (ADR-0102).
func keyService(s *operatorpg.Store) operator.Service {
	return operator.NewService(s, operator.Config{TokenEndpoint: testTokenURL})
}

// issueKey issues subject a key under rotation and answers the private
// key the issuance would have shown, parsed as a holder parses the PEM.
func issueKey(t *testing.T, s *operatorpg.Store, subject grn.GRN, rotation operator.KeyRotation) (*rsa.PrivateKey, operator.Key) {
	t.Helper()
	issued, err := operator.NewIssuedKey(subject, scopedGRN(subject.Organization, "user", "ada"))
	require.NoError(t, err)
	key, err := s.IssueKey(context.Background(), issued, rotation)
	require.NoError(t, err)
	block, _ := pem.Decode(issued.PrivatePEM())
	require.NotNil(t, block)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	private, ok := parsed.(*rsa.PrivateKey)
	require.True(t, ok)
	return private, key
}

// freshJTI draws a jti of 128 random bits, as a client does for every
// attempt.
func freshJTI(t *testing.T) string {
	t.Helper()
	var b [16]byte
	_, err := rand.Read(b[:])
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// assertion signs a client assertion of subject with private through
// go-jose, a stock JOSE implementation, for audience aud.
func assertion(t *testing.T, private *rsa.PrivateKey, subject grn.GRN, aud, jti string, iat time.Time) string {
	t.Helper()
	kid, err := operator.Thumbprint(&private.PublicKey)
	require.NoError(t, err)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: private},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", kid))
	require.NoError(t, err)
	claims, err := json.Marshal(map[string]any{
		"iss": subject.String(), "sub": subject.String(), "aud": aud,
		"iat": iat.Unix(), "exp": iat.Add(time.Minute).Unix(), "jti": jti,
	})
	require.NoError(t, err)
	signed, err := signer.Sign(claims)
	require.NoError(t, err)
	compact, err := signed.CompactSerialize()
	require.NoError(t, err)
	return compact
}

// freshAssertion is a valid assertion of subject issued now.
func freshAssertion(t *testing.T, private *rsa.PrivateKey, subject grn.GRN) string {
	t.Helper()
	return assertion(t, private, subject, testTokenURL, freshJTI(t), time.Now())
}

// oauthExchange exchanges a fresh assertion through x/oauth2's client
// credentials flow, a stock OAuth client that knows nothing of garam:
// the assertion travels as RFC 7523's two parameters and nothing else
// is configured. It answers the token or the client's error.
func oauthExchange(t *testing.T, client *http.Client, url string, subject grn.GRN, signed string) (*oauth2.Token, error) {
	t.Helper()
	cfg := clientcredentials.Config{
		ClientID:  subject.String(),
		TokenURL:  url + "/oauth/token",
		AuthStyle: oauth2.AuthStyleInParams,
		EndpointParams: map[string][]string{
			"client_assertion_type": {operator.ClientAssertionTypeJWT},
			"client_assertion":      {signed},
		},
	}
	return cfg.Token(context.WithValue(context.Background(), oauth2.HTTPClient, client))
}

// bearerIdentifies answers the status of GET /identity presenting
// token under the Bearer scheme.
func bearerIdentifies(t *testing.T, client *http.Client, url, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url+"/identity", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	require.NoError(t, err)
	// Drained before closing, so the transport reuses the connection
	// and a test can count the requests that travelled over one.
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode
}

// TestOperatorKey_AStockOAuthClientExchangesTheIssuedPEMOnTheShippedListener
// is ADR-0102's interoperability claim end to end, against real
// Postgres and the listener `serve machine` assembles: the PEM a key
// issuance answered signs, through go-jose, an assertion that
// x/oauth2's client credentials flow exchanges for a token, and the
// token authenticates under Bearer and Garam-Token alike. The store
// holds the public key alone. A copy of the same assertion is refused
// as invalid_client, with the fresh assertion beside it as the
// control.
func TestOperatorKey_AStockOAuthClientExchangesTheIssuedPEMOnTheShippedListener(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	store := operatorpg.New(p)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	private, key := issueKey(t, store, subject, operator.KeyRotationFirst)
	client, connections := tokenClient(t)

	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_keys WHERE public_jwk LIKE '%"d"%' OR public_jwk LIKE '%PRIVATE%'`),
		"no private member is stored")
	require.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM operator_keys WHERE public_jwk LIKE '%"n"%' AND kid = $1`, key.KID),
		"the control: the public modulus is")

	signed := freshAssertion(t, private, subject)
	token, err := oauthExchange(t, client, url, subject, signed)
	require.NoError(t, err)
	assert.Equal(t, "Bearer", token.TokenType)
	assert.WithinDuration(t, time.Now().Add(time.Hour), token.Expiry, time.Minute)
	assert.Equal(t, http.StatusOK, bearerIdentifies(t, client, url, token.AccessToken))
	assert.Equal(t, http.StatusOK, identifiesWith(t, client, url, token.AccessToken), "the same token under Garam-Token")

	_, err = oauthExchange(t, client, url, subject, signed)
	var refused *oauth2.RetrieveError
	require.ErrorAs(t, err, &refused, "a replayed assertion is refused")
	assert.Equal(t, "invalid_client", refused.ErrorCode)
	assert.Equal(t, http.StatusBadRequest, refused.Response.StatusCode)
	assert.Equal(t, 1, *connections, "every request travelled over one connection")

	var exchanged *time.Time
	require.NoError(t, p.QueryRow(ctx, `SELECT last_exchanged_at FROM operator_keys WHERE id = $1`, key.ID).Scan(&exchanged))
	assert.NotNil(t, exchanged, "the key records that it obtained a token, which an inventory reads")
}

// TestOperatorKey_OneAssertionYieldsOneTokenAcrossReplicasAndRestarts
// is the replay rule against real Postgres (ADR-0102): sixteen copies
// of one assertion sent at once through two services — two replicas
// over one database — yield exactly one token, and a third service
// constructed afterwards, a restart, refuses it too. A fresh jti
// through that third service is the control.
func TestOperatorKey_OneAssertionYieldsOneTokenAcrossReplicasAndRestarts(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operatorpg.New(p)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	private, _ := issueKey(t, store, subject, operator.KeyRotationFirst)
	replicas := []operator.Service{keyService(store), keyService(operatorpg.New(p))}
	signed := freshAssertion(t, private, subject)

	const copies = 16
	var wg sync.WaitGroup
	results := make([]error, copies)
	for i := range copies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = replicas[i%2].ExchangeAssertion(ctx, testSource, subject, signed, nil)
		}()
	}
	wg.Wait()
	succeeded := 0
	for _, err := range results {
		if err == nil {
			succeeded++
			continue
		}
		assert.ErrorIs(t, err, operator.ErrClientInvalid)
	}
	assert.Equal(t, 1, succeeded, "one assertion yields one token")
	assert.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE operator_grn = $1`, subject.String()))

	restarted := keyService(operatorpg.New(p))
	_, err := restarted.ExchangeAssertion(ctx, testSource, subject, signed, nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "a restart forgets no spent assertion")
	_, err = restarted.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, private, subject), nil)
	assert.NoError(t, err, "the control: a fresh jti is exchanged")
}

// TestOperatorKey_ARevocationRacingExchangesLeavesNoTokenOfTheRevokedKey
// is ADR-0102's revocation race: exchanges under a key run while the
// key is withdrawn, and once the withdrawal has returned no token
// exchanged under it resolves, however the two interleaved, and no
// exchange after it succeeds. The second key, never withdrawn, is the
// control: its token resolves throughout.
func TestOperatorKey_ARevocationRacingExchangesLeavesNoTokenOfTheRevokedKey(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operatorpg.New(p)
	svc := keyService(store)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	revoked, revokedKey := issueKey(t, store, subject, operator.KeyRotationFirst)
	kept, _ := issueKey(t, store, subject, operator.KeyRotationPlanned)
	keptToken, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, kept, subject), nil)
	require.NoError(t, err)

	// Assertions are signed before the race so that the window holds
	// the exchanges and not the signing.
	const racing = 8
	signed := make([]string, racing)
	for i := range signed {
		signed[i] = freshAssertion(t, revoked, subject)
	}
	tokens := make([]*operator.Token, racing)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range racing {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			token, err := svc.ExchangeAssertion(ctx, testSource, subject, signed[i], nil)
			if err == nil {
				tokens[i] = token
			} else {
				assert.ErrorIs(t, err, operator.ErrClientInvalid)
			}
		}()
	}
	close(start)
	require.NoError(t, store.RevokeKey(ctx, subject, revokedKey.KID))
	wg.Wait()

	for i, token := range tokens {
		if token == nil {
			continue
		}
		_, err := store.ResolveToken(ctx, token.Hash())
		assert.ErrorIs(t, err, operator.ErrTokenInvalid, "token %d of the withdrawn key resolves to nothing", i)
	}
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE key_id = $1`, revokedKey.ID),
		"the withdrawal deleted every token the key had, those the race recorded included")
	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, revoked, subject), nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "the withdrawn key exchanges nothing afterwards")

	_, err = store.ResolveToken(ctx, keptToken.Hash())
	assert.NoError(t, err, "the control: the key nobody withdrew keeps its token")
}

// TestOperatorKey_PlannedRotationOverlapsForItsWindowAndRefusesAThirdKey
// holds planned rotation (ADR-0102): the successor verifies beside its
// predecessor, which reads retiring until the window it was given; a
// third live key is refused; and once that window passes the
// predecessor and its token are refused without a write, while the
// successor goes on.
func TestOperatorKey_PlannedRotationOverlapsForItsWindowAndRefusesAThirdKey(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operatorpg.New(p)
	svc := keyService(store)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	old, oldKey := issueKey(t, store, subject, operator.KeyRotationFirst)
	oldToken, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, old, subject), nil)
	require.NoError(t, err)

	successor, _ := issueKey(t, store, subject, operator.KeyRotationPlanned)
	keys, err := store.ListKeys(ctx, subject)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	assert.Equal(t, operator.KeyStateRetiring, keys[0].State)
	require.NotNil(t, keys[0].RetiresAt)
	assert.WithinDuration(t, time.Now().Add(operator.RolloverWindow), *keys[0].RetiresAt, time.Minute)
	assert.Equal(t, operator.KeyStateActive, keys[1].State)

	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, old, subject), nil)
	assert.NoError(t, err, "the predecessor verifies inside the window")
	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, successor, subject), nil)
	assert.NoError(t, err, "and so does the successor")

	third, err := operator.NewIssuedKey(subject, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	_, err = store.IssueKey(ctx, third, operator.KeyRotationPlanned)
	assert.ErrorIs(t, err, operator.ErrKeyLimit)
	_, err = store.IssueKey(ctx, third, operator.KeyRotationFirst)
	assert.ErrorIs(t, err, operator.ErrOperatorWasKeyed, "a first issuance finds the live keys too")

	_, err = p.Exec(ctx, `UPDATE operator_keys SET retires_at = now() - interval '1 second' WHERE id = $1`, oldKey.ID)
	require.NoError(t, err)
	_, err = store.ResolveToken(ctx, oldToken.Hash())
	assert.ErrorIs(t, err, operator.ErrTokenInvalid, "past the window the predecessor's token is refused")
	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, old, subject), nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "and the predecessor exchanges nothing")
	keys, err = store.ListKeys(ctx, subject)
	require.NoError(t, err)
	assert.Equal(t, operator.KeyStateRevoked, keys[0].State)
	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, successor, subject), nil)
	assert.NoError(t, err, "the control: the successor goes on")
}

// TestOperatorKey_WithdrawalRefusesTheNextRequestOnAnOpenConnection
// holds ADR-0102's revocation on the shipped listener: an immediate
// replacement and a withdrawal each refuse the old key's token on the
// next request over the connection already open; the replacement's
// own token, and an operator nothing was done to, are the controls.
func TestOperatorKey_WithdrawalRefusesTheNextRequestOnAnOpenConnection(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	store := operatorpg.New(p)
	replaced := register(t, store, "replaced")
	withdrawn := register(t, store, "withdrawn")
	kept := register(t, store, "kept")
	client, connections := tokenClient(t)
	exchange := func(private *rsa.PrivateKey, subject grn.GRN) string {
		token, err := oauthExchange(t, client, url, subject, freshAssertion(t, private, subject))
		require.NoError(t, err)
		return token.AccessToken
	}

	replacedKey, _ := issueKey(t, store, replaced, operator.KeyRotationFirst)
	withdrawnKey, withdrawnRecord := issueKey(t, store, withdrawn, operator.KeyRotationFirst)
	keptKey, _ := issueKey(t, store, kept, operator.KeyRotationFirst)
	replacedToken := exchange(replacedKey, replaced)
	withdrawnToken := exchange(withdrawnKey, withdrawn)
	keptToken := exchange(keptKey, kept)
	for _, token := range []string{replacedToken, withdrawnToken, keptToken} {
		require.Equal(t, http.StatusOK, bearerIdentifies(t, client, url, token))
	}

	successor, _ := issueKey(t, store, replaced, operator.KeyRotationImmediate)
	assert.Equal(t, http.StatusUnauthorized, bearerIdentifies(t, client, url, replacedToken),
		"an immediate replacement refuses the old key's token on the next request")
	_, err := oauthExchange(t, client, url, replaced, freshAssertion(t, replacedKey, replaced))
	assert.Error(t, err, "and the old key exchanges nothing")
	assert.Equal(t, http.StatusOK, bearerIdentifies(t, client, url, exchange(successor, replaced)),
		"the replacement's own token authenticates")

	require.NoError(t, store.RevokeKey(ctx, withdrawn, withdrawnRecord.KID))
	assert.Equal(t, http.StatusUnauthorized, bearerIdentifies(t, client, url, withdrawnToken),
		"a withdrawal refuses the key's token on the next request")

	assert.Equal(t, http.StatusOK, bearerIdentifies(t, client, url, keptToken), "the operator nothing was done to")
	assert.Equal(t, 1, *connections, "every request travelled over one connection")
}

// TestOperatorKey_AReusedIdentifierRevivesNothingOfTheDeletedRegistration
// holds the registration incarnation (ADR-0102): deleting an operator
// takes its keys and their tokens, and registering the same identifier
// again neither accepts the old key's fresh assertion nor resolves its
// old token, while a key issued to the new registration exchanges.
func TestOperatorKey_AReusedIdentifierRevivesNothingOfTheDeletedRegistration(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operatorpg.New(p)
	svc := keyService(store)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	earlier, _ := issueKey(t, store, subject, operator.KeyRotationFirst)
	earlierToken, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, earlier, subject), nil)
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, subject))
	registerOperator(t, p, subject)

	_, err = store.ResolveToken(ctx, earlierToken.Hash())
	assert.ErrorIs(t, err, operator.ErrTokenInvalid)
	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, earlier, subject), nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "the earlier incarnation's key proves nothing")

	current, _ := issueKey(t, store, subject, operator.KeyRotationFirst)
	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, current, subject), nil)
	assert.NoError(t, err, "the control: the new registration's key exchanges")
}

// TestOperatorKey_RefusesAnotherTenantsIdentityOrAudience holds the
// identity and audience bounds against real Postgres (ADR-0102). Each
// refusal is the valid exchange with one thing changed: a client_id of
// another organization's operator presenting this key, an assertion
// naming another endpoint, a token audience in another organization.
// The same-organization audience is accepted, and its token is refused
// on garam's own routes, as one minted to be shown elsewhere is.
func TestOperatorKey_RefusesAnotherTenantsIdentityOrAudience(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	for _, name := range []string{"acme", "globex"} {
		require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN(name), name)))
	}
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	store := operatorpg.New(p)
	svc := keyService(store)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	peer := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "control"))
	foreign := registerOperator(t, p, scopedGRN("globex", operator.ServiceType, "controller"))
	private, _ := issueKey(t, store, subject, operator.KeyRotationFirst)
	issueKey(t, store, foreign, operator.KeyRotationFirst)

	_, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, private, subject), nil)
	require.NoError(t, err, "the control: the valid exchange")

	_, err = svc.ExchangeAssertion(ctx, testSource, foreign, freshAssertion(t, private, foreign), nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "a key is looked up within the client's registration alone")
	_, err = svc.ExchangeAssertion(ctx, testSource, subject,
		assertion(t, private, subject, "https://elsewhere.test/oauth/token", freshJTI(t), time.Now()), nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "an assertion for another endpoint")
	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, private, subject), &foreign)
	assert.ErrorIs(t, err, operator.ErrAudienceInvalid, "an audience in another organization")

	audienced, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, private, subject), &peer)
	require.NoError(t, err)
	held, err := store.ResolveToken(ctx, audienced.Hash())
	require.NoError(t, err)
	require.NotNil(t, held.Audience)
	assert.Equal(t, peer, *held.Audience)
	client, _ := tokenClient(t)
	assert.Equal(t, http.StatusUnauthorized, bearerIdentifies(t, client, url, audienced.Value),
		"a token minted to be shown to the peer is accepted on none of garam's routes")
	require.NoError(t, svc.VerifyShown(ctx, subject, peer, audienced.Value),
		"the peer's introspection proves it, as it does a shared-secret one")
}

// TestOperatorKey_AnExpiredTokenRecoversWithAFreshAssertionAndAWithdrawalStays
// holds recovery across an outage that outlasted the token (ADR-0102):
// the expired token is refused, the unchanged key's fresh assertion
// obtains a new one, and a key withdrawn afterwards stays refused
// however fresh its next assertion is.
func TestOperatorKey_AnExpiredTokenRecoversWithAFreshAssertionAndAWithdrawalStays(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operatorpg.New(p)
	svc := keyService(store)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	private, key := issueKey(t, store, subject, operator.KeyRotationFirst)
	before, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, private, subject), nil)
	require.NoError(t, err)

	_, err = p.Exec(ctx, `UPDATE operator_tokens SET expires_at = now() - interval '1 second' WHERE token_hash = $1`, before.Hash())
	require.NoError(t, err)
	_, err = store.ResolveToken(ctx, before.Hash())
	require.ErrorIs(t, err, operator.ErrTokenInvalid, "the token expired across the outage")

	after, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, private, subject), nil)
	require.NoError(t, err, "the unchanged key recovers with a fresh assertion")
	_, err = store.ResolveToken(ctx, after.Hash())
	require.NoError(t, err)

	require.NoError(t, store.RevokeKey(ctx, subject, key.KID))
	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, private, subject), nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "refreshing cannot clear a withdrawn key")
	_, err = store.ResolveToken(ctx, after.Hash())
	assert.ErrorIs(t, err, operator.ErrTokenInvalid)
}

// TestOperatorKey_AnAssertionOutsideItsTimeBoundsSpendsNothing holds
// that the time bounds are measured at the database's time (ADR-0102)
// and that a refused assertion leaves no spent jti: an assertion issued
// past the skew is refused and the same jti in a timely assertion is
// then accepted.
func TestOperatorKey_AnAssertionOutsideItsTimeBoundsSpendsNothing(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operatorpg.New(p)
	svc := keyService(store)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	private, _ := issueKey(t, store, subject, operator.KeyRotationFirst)
	jti := freshJTI(t)

	_, err := svc.ExchangeAssertion(ctx, testSource, subject,
		assertion(t, private, subject, testTokenURL, jti, time.Now().Add(-2*time.Minute)), nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "expired past the skew")
	_, err = svc.ExchangeAssertion(ctx, testSource, subject,
		assertion(t, private, subject, testTokenURL, jti, time.Now().Add(time.Minute)), nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "issued past the skew ahead")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_assertions WHERE jti = $1`, jti))

	_, err = svc.ExchangeAssertion(ctx, testSource, subject, assertion(t, private, subject, testTokenURL, jti, time.Now()), nil)
	require.NoError(t, err, "the control: the same jti, timely, is accepted")
	assert.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM operator_assertions WHERE jti = $1`, jti))
}

// TestOperatorKey_AFirstIssuanceIsRefusedOnceAnyKeyWasIssued is the
// PM's review of #1360, blocking item 1, against real Postgres: a
// withdrawn key still refuses KeyRotationFirst, the issuance the
// registration's authority reaches, so re-keying after a revocation is
// operator:reissue's. An immediate rotation, which that authority
// issues, is the control; an operator never keyed takes a first key.
func TestOperatorKey_AFirstIssuanceIsRefusedOnceAnyKeyWasIssued(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operatorpg.New(p)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	fresh := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "control"))
	_, key := issueKey(t, store, subject, operator.KeyRotationFirst)
	require.NoError(t, store.RevokeKey(ctx, subject, key.KID))
	require.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_keys
		WHERE operator_grn = $1 AND revoked_at IS NULL`, subject.String()), "the operator holds no live key")

	next, err := operator.NewIssuedKey(subject, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	_, err = store.IssueKey(ctx, next, operator.KeyRotationFirst)
	assert.ErrorIs(t, err, operator.ErrOperatorWasKeyed)
	assert.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM operator_keys WHERE operator_grn = $1`, subject.String()),
		"the refused issuance wrote nothing")

	_, err = store.IssueKey(ctx, next, operator.KeyRotationImmediate)
	assert.NoError(t, err, "the control: operator:reissue's rotation re-keys")
	issueKey(t, store, fresh, operator.KeyRotationFirst)
}

// TestOperatorKey_AnImmediateRotationRacingExchangesLeavesNoTokenOfTheOldKey
// is the PM's review of #1360, item 6: exchanges under the old key run
// while an immediate rotation replaces it, and once the rotation has
// returned no token of the old key resolves however the two
// interleaved, the old key exchanges nothing, and the successor does.
// Another operator's key, untouched, is the control on the resolve.
func TestOperatorKey_AnImmediateRotationRacingExchangesLeavesNoTokenOfTheOldKey(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operatorpg.New(p)
	svc := keyService(store)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	bystander := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "control"))
	old, oldKey := issueKey(t, store, subject, operator.KeyRotationFirst)
	kept, _ := issueKey(t, store, bystander, operator.KeyRotationFirst)
	keptToken, err := svc.ExchangeAssertion(ctx, testSource, bystander, freshAssertion(t, kept, bystander), nil)
	require.NoError(t, err)
	successor, err := operator.NewIssuedKey(subject, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)

	const racing = 8
	signed := make([]string, racing)
	for i := range signed {
		signed[i] = freshAssertion(t, old, subject)
	}
	tokens := make([]*operator.Token, racing)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range racing {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			token, err := svc.ExchangeAssertion(ctx, testSource, subject, signed[i], nil)
			if err == nil {
				tokens[i] = token
			} else {
				assert.ErrorIs(t, err, operator.ErrClientInvalid)
			}
		}()
	}
	close(start)
	_, err = store.IssueKey(ctx, successor, operator.KeyRotationImmediate)
	require.NoError(t, err)
	wg.Wait()

	for i, token := range tokens {
		if token == nil {
			continue
		}
		_, err := store.ResolveToken(ctx, token.Hash())
		assert.ErrorIs(t, err, operator.ErrTokenInvalid, "token %d of the replaced key resolves to nothing", i)
	}
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE key_id = $1`, oldKey.ID),
		"the rotation deleted every token of the old key, those the race recorded included")
	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, old, subject), nil)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "the replaced key exchanges nothing afterwards")

	block, _ := pem.Decode(successor.PrivatePEM())
	require.NotNil(t, block)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	next, ok := parsed.(*rsa.PrivateKey)
	require.True(t, ok)
	_, err = svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, next, subject), nil)
	assert.NoError(t, err, "the successor exchanges")
	_, err = store.ResolveToken(ctx, keptToken.Hash())
	assert.NoError(t, err, "the control: another operator's token resolves throughout")
}

// TestOperatorKey_AnImmediateRotationWaitsForAnExchangeAlreadyHoldingTheKey
// is #1399's interleaving made certain rather than raced for: an
// exchange holds the old key when an immediate rotation begins. An
// uncommitted spend of the same jti, another replica's, parks the
// exchange there. The rotation waits for the exchange and deletes the
// token it recorded; holding the two rows in opposite orders, each
// waited on the other and Postgres aborted one with 40P01. Another
// operator's token, untouched, is the control on the resolve.
func TestOperatorKey_AnImmediateRotationWaitsForAnExchangeAlreadyHoldingTheKey(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operatorpg.New(p)
	svc := keyService(store)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	bystander := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "control"))
	old, oldKey := issueKey(t, store, subject, operator.KeyRotationFirst)
	kept, _ := issueKey(t, store, bystander, operator.KeyRotationFirst)
	keptToken, err := svc.ExchangeAssertion(ctx, testSource, bystander, freshAssertion(t, kept, bystander), nil)
	require.NoError(t, err)
	successor, err := operator.NewIssuedKey(subject, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	jti := freshJTI(t)
	signed := assertion(t, old, subject, testTokenURL, jti, time.Now())

	replica, err := p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = replica.Rollback(ctx) }()
	_, err = replica.Exec(ctx, `INSERT INTO operator_assertions (operator_grn, jti, expires_at)
		VALUES ($1, $2, now() + interval '2 minutes')`, subject.String(), jti)
	require.NoError(t, err)

	type exchange struct {
		token *operator.Token
		err   error
	}
	exchanged := make(chan exchange, 1)
	go func() {
		token, err := svc.ExchangeAssertion(ctx, testSource, subject, signed, nil)
		exchanged <- exchange{token, err}
	}()
	awaitLockWaiters(t, p, 1)
	rotated := make(chan error, 1)
	go func() {
		_, err := store.IssueKey(ctx, successor, operator.KeyRotationImmediate)
		rotated <- err
	}()
	awaitLockWaiters(t, p, 2)
	require.NoError(t, replica.Rollback(ctx))

	done := receive(t, exchanged)
	require.NoError(t, done.err, "the exchange holding the key completes")
	require.NoError(t, receive(t, rotated), "the rotation waits for it rather than deadlocking")
	_, err = store.ResolveToken(ctx, done.token.Hash())
	assert.ErrorIs(t, err, operator.ErrTokenInvalid, "the rotation deleted the token the exchange recorded")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE key_id = $1`, oldKey.ID))
	_, err = store.ResolveToken(ctx, keptToken.Hash())
	assert.NoError(t, err, "the control: another operator's token resolves throughout")
}

// receive answers what ch carries, and fails the test rather than
// hanging the run when nothing arrives.
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(30 * time.Second):
		require.FailNow(t, "nothing arrived")
		var zero T
		return zero
	}
}

// TestOperatorKey_AWithdrawalAnswersErrKeyNotFoundForAKeyNeverIssued
// holds RevokeKey's contract across the registration it now holds
// first (#1418): an operator the organization holds no record of was
// never issued the key and answers ErrKeyNotFound, as an unknown kid of
// a registered operator does. The registered operator's own key, which
// withdraws and then withdraws again as a no-op, is the control.
func TestOperatorKey_AWithdrawalAnswersErrKeyNotFoundForAKeyNeverIssued(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operatorpg.New(p)
	subject := registerOperator(t, p, scopedGRN("acme", operator.ServiceType, "controller"))
	_, key := issueKey(t, store, subject, operator.KeyRotationFirst)

	assert.ErrorIs(t, store.RevokeKey(ctx, scopedGRN("acme", operator.ServiceType, "unregistered"), key.KID),
		operator.ErrKeyNotFound, "an operator the organization holds no record of")
	assert.ErrorIs(t, store.RevokeKey(ctx, subject, "kid-never-issued"), operator.ErrKeyNotFound,
		"a kid the registered operator was never issued")
	require.NoError(t, store.RevokeKey(ctx, subject, key.KID), "the control: its own key withdraws")
	assert.NoError(t, store.RevokeKey(ctx, subject, key.KID), "and a second withdrawal is a no-op")
}
