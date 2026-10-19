package integration_test

import (
	"context"
	"crypto/rsa"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// keyed registers an operator of acme, issues it its first key, and
// answers its GRN and the private key the issuance showed (ADR-0102).
func keyed(t *testing.T, s *operator.PostgresStore, id string) (grn.GRN, *rsa.PrivateKey) {
	t.Helper()
	g := register(t, s, id)
	private, _ := issueKey(t, s, g, operator.KeyRotationFirst)
	return g, private
}

// exchangeKey exchanges a fresh assertion signed by subject's private
// key on the listener at listener and answers the token: garam's where
// audience is nil, and one shown to audience otherwise.
func exchangeKey(t *testing.T, client *http.Client, listener string, subject grn.GRN, private *rsa.PrivateKey, audience *grn.GRN) string {
	t.Helper()
	form := url.Values{
		"grant_type":            {"client_credentials"},
		"client_id":             {subject.String()},
		"client_assertion_type": {operator.ClientAssertionTypeJWT},
		"client_assertion":      {freshAssertion(t, private, subject)},
	}
	if audience != nil {
		form.Set("audience", audience.String())
	}
	resp, err := client.PostForm(listener+"/oauth/token", form)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(body))
	var token struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(body, &token))
	return token.AccessToken
}

// TestOperatorToken_ExpiredAndSweptAndAudienceKept covers what a row
// says beyond its operator: a token past its expiry resolves to
// nothing, the next exchange sweeps it, and a token minted for a
// hosted operator resolves with that audience.
func TestOperatorToken_ExpiredAndSweptAndAudienceKept(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := keyService(store)
	subject, key := keyed(t, store, "controller")
	hosted := register(t, store, "control")

	expired, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, key, subject), nil)
	require.NoError(t, err)
	_, err = store.ResolveToken(ctx, expired.Hash())
	require.NoError(t, err, "the token resolves while it is live")
	_, err = p.Exec(ctx, `UPDATE operator_tokens SET expires_at = now() - interval '1 second' WHERE token_hash = $1`,
		expired.Hash())
	require.NoError(t, err)
	_, err = store.ResolveToken(ctx, expired.Hash())
	assert.ErrorIs(t, err, operator.ErrTokenInvalid, "an expired token resolves to nothing")

	audienced, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, key, subject), &hosted)
	require.NoError(t, err)
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_tokens WHERE token_hash = $1`, expired.Hash()),
		"the exchange swept the expired token")
	held, err := store.ResolveToken(ctx, audienced.Hash())
	require.NoError(t, err)
	assert.Equal(t, subject, held.Operator)
	require.NotNil(t, held.Audience)
	assert.Equal(t, hosted, *held.Audience)
	assert.WithinDuration(t, time.Now().Add(operator.TokenTTL), held.ExpiresAt, time.Minute)
}

// tokenClient reaches the machine listener presenting no certificate,
// counting the connections it opens, so a test can show its requests
// travelled over one.
func tokenClient(t *testing.T) (*http.Client, *int) {
	t.Helper()
	var connections int
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		VerifyConnection: func(tls.ConnectionState) error {
			connections++
			return nil
		},
	}}}
	t.Cleanup(client.CloseIdleConnections)
	return client, &connections
}

// identifiesWith answers the status of GET /identity presenting token
// over client.
func identifiesWith(t *testing.T, client *http.Client, url, token string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url+"/identity", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Garam-Token "+token)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode
}

// TestOperatorToken_RefusedOnTheNextRequestOverAnOpenConnection is
// ADR-0070's revocation of a token end to end against real Postgres
// and the listener `serve machine` assembles: an operator's token
// authenticates, and the next request on the same connection is
// refused once the organization deletes the operator. Every request
// travels over the connection the first one opened, so what refuses
// the later one is the per-request read and not a new handshake.
// Withdrawing a key is held the same way in operator_key_test.go.
//
// The second operator is the control: it presents a token over the
// same connection throughout and nothing is done to it, so what refused
// the first is the delete and not the listener refusing every token
// after the first.
func TestOperatorToken_RefusedOnTheNextRequestOverAnOpenConnection(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	listener := machineListener(t, p, sealer, engine)
	store := operator.NewPostgresStore(p)

	deleted, deletedKey := keyed(t, store, "deleted")
	kept, keptKey := keyed(t, store, "kept")
	client, connections := tokenClient(t)
	deletedToken := exchangeKey(t, client, listener, deleted, deletedKey, nil)
	keptToken := exchangeKey(t, client, listener, kept, keptKey, nil)
	require.Equal(t, http.StatusOK, identifiesWith(t, client, listener, deletedToken))
	require.Equal(t, http.StatusOK, identifiesWith(t, client, listener, keptToken))

	require.NoError(t, store.Delete(ctx, deleted))
	assert.Equal(t, http.StatusUnauthorized, identifiesWith(t, client, listener, deletedToken),
		"a delete refuses the operator's token on the next request")

	assert.Equal(t, http.StatusOK, identifiesWith(t, client, listener, keptToken),
		"the operator nothing was done to goes on authenticating")
	assert.Equal(t, 1, *connections, "every request travelled over one connection")
}

// requestWith answers the status and body of method on path presenting
// token over client.
func requestWith(t *testing.T, client *http.Client, method, url, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Garam-Token "+token)
	resp, err := client.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, string(body)
}

// TestOperatorToken_ReachesTheOperatorRoutesAloneOnTheShippedListener
// is ConfineTokens as `serve machine` wires it (ADR-0100): a live token
// is refused on a route outside the set that lists it — the signing
// keys, whose handler reads nothing of the caller, so the confinement
// is what refuses it — and on the renewal, which reads a certificate
// the token does not carry. The same token on GET /identity is the
// control.
func TestOperatorToken_ReachesTheOperatorRoutesAloneOnTheShippedListener(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	subject, key := keyed(t, operator.NewPostgresStore(p), "one")
	client, _ := tokenClient(t)
	token := exchangeKey(t, client, url, subject, key, nil)

	status, _ := requestWith(t, client, http.MethodGet, url+"/identity", token)
	require.Equal(t, http.StatusOK, status, "the token authenticates where it is listed")

	status, body := tokenCall(t, client, http.MethodGet, url+"/message-signing-keys", token, "", "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, "this operation does not accept a token")

	status, _ = requestWith(t, client, http.MethodPost, url+"/identity/certificate", token)
	assert.Equal(t, http.StatusUnauthorized, status, "a token renews no certificate")
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operator_certificates`),
		"nothing was issued")
}

// TestOperatorToken_AKeyHoldsItsNewestFiveTokens holds the bound on
// what exchanging writes (ADR-0102): seven exchanges leave five live
// tokens, the two oldest no longer resolve, and the newest five do —
// so what removed the two is the bound and not every token going.
func TestOperatorToken_AKeyHoldsItsNewestFiveTokens(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := keyService(store)
	subject, key := keyed(t, store, "one")

	var tokens []*operator.Token
	for range 7 {
		token, err := svc.ExchangeAssertion(ctx, testSource, subject, freshAssertion(t, key, subject), nil)
		require.NoError(t, err)
		tokens = append(tokens, token)
	}

	assert.Equal(t, operator.LiveTokensPerKey, countRows(t, p,
		`SELECT count(*) FROM operator_tokens WHERE operator_grn = $1`, subject.String()))
	for i, token := range tokens {
		_, err := store.ResolveToken(ctx, token.Hash())
		if i < len(tokens)-operator.LiveTokensPerKey {
			assert.ErrorIs(t, err, operator.ErrTokenInvalid, "token %d is past the bound", i)
		} else {
			assert.NoError(t, err, "token %d is among the newest", i)
		}
	}
}

// TestOperatorTokens_RestOnAKeyAlone holds migration 000056: a token
// row names the key it was exchanged under, since no credential issues
// one any more. The same row naming a key is the control, so what
// refuses the first is the missing key and not the row.
func TestOperatorTokens_RestOnAKeyAlone(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	subject := register(t, store, "one")
	_, key := issueKey(t, store, subject, operator.KeyRotationFirst)

	const insert = `INSERT INTO operator_tokens (token_hash, key_id, operator_grn, issued_at, expires_at)
		VALUES ($1, $2, $3, now(), now() + interval '1 hour')`
	_, err := p.Exec(ctx, insert, "keyless", nil, subject.String())
	var refused *pgconn.PgError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, "23502", refused.Code, "key_id is not null")
	_, err = p.Exec(ctx, insert, "keyed", key.ID, subject.String())
	assert.NoError(t, err, "the same row naming its key is admitted")
}

// TestCredentialEvents_RefuseTheRemovedKinds holds migration 000056's
// check: an operator credential and an operator enrollment token are
// kinds no event names any more. The same event naming an operator
// certificate is the control.
func TestCredentialEvents_RefuseTheRemovedKinds(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	subject := scopedGRN("acme", operator.ServiceType, "one")

	const insert = `INSERT INTO audit_credential_events (subject_grn, org_grn, credential, action, outcome, recorded_at)
		VALUES ($1, $2, $3, 'issue', 'issued', now())`
	for _, kind := range []string{"operator_credential", "operator_enrollment_token"} {
		_, err := p.Exec(ctx, insert, subject.String(), orgRootGRN("acme").String(), kind)
		var refused *pgconn.PgError
		require.ErrorAs(t, err, &refused, kind)
		assert.Equal(t, "audit_credential_events_credential", refused.ConstraintName, kind)
	}
	_, err := p.Exec(ctx, insert, subject.String(), orgRootGRN("acme").String(), "operator_certificate")
	assert.NoError(t, err, "a kind the check admits is recorded")
}
