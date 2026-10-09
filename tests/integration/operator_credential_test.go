package integration_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// credentialFor draws the credential a registration of o issues
// (ADR-0100).
func credentialFor(t *testing.T, o *operator.Operator) *operator.IssuedCredential {
	t.Helper()
	c, err := operator.NewIssuedCredential(o.GRN(), scopedGRN(o.GRN().Organization, "user", "ada"))
	require.NoError(t, err)
	return c
}

// registerCredentialed registers an operator of acme and returns its
// GRN and the credential the registration answered.
func registerCredentialed(t *testing.T, s *operator.PostgresStore, id string) (grn.GRN, string) {
	t.Helper()
	registeredBy := scopedGRN("acme", "user", "ada")
	o, err := operator.New(orgRootGRN("acme"), id, registeredBy, operator.DetailsPatch{})
	require.NoError(t, err)
	e, err := operator.NewEnrollment(o.GRN(), registeredBy)
	require.NoError(t, err)
	c := credentialFor(t, o)
	_, err = s.Register(context.Background(), o, e, c)
	require.NoError(t, err)
	return o.GRN(), c.Value()
}

// exchangeService is the operator service over the store alone, which
// is all an exchange reads.
func exchangeService(s *operator.PostgresStore) operator.Service {
	return operator.NewService(s, s, s, nil, operator.Config{})
}

// TestOperatorCredential_ReissueVoidsTheTokensOfThePreviousOne is
// ADR-0100's re-issue against real Postgres: the previous credential
// exchanges nothing afterwards, and a token exchanged for it resolves
// to nothing, by 000038's cascade rather than a statement of its own.
//
// The token exchanged for the new credential is the control: it
// resolves, so what refuses the first is the re-issue and not the
// resolve refusing every token.
func TestOperatorCredential_ReissueVoidsTheTokensOfThePreviousOne(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := exchangeService(store)
	subject, previous := registerCredentialed(t, store, "one")

	before, err := svc.Exchange(ctx, previous, nil)
	require.NoError(t, err)
	held, err := store.ResolveToken(ctx, before.Hash())
	require.NoError(t, err, "the token resolves before the re-issue")
	assert.Equal(t, subject, held.Operator)
	assert.Nil(t, held.Audience)

	next, err := operator.NewIssuedCredential(subject, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	require.NoError(t, store.ReissueCredential(ctx, next))

	_, err = store.ResolveToken(ctx, before.Hash())
	assert.ErrorIs(t, err, operator.ErrTokenInvalid, "the previous credential's token goes with it")
	_, err = svc.Exchange(ctx, previous, nil)
	assert.ErrorIs(t, err, operator.ErrCredentialInvalid, "the previous credential exchanges nothing")

	after, err := svc.Exchange(ctx, next.Value(), nil)
	require.NoError(t, err)
	_, err = store.ResolveToken(ctx, after.Hash())
	assert.NoError(t, err, "the new credential's token resolves")

	assert.Equal(t, 1, countRows(t, p,
		`SELECT count(*) FROM operator_credentials WHERE operator_grn = $1`, subject.String()),
		"an operator holds one credential")
}

// TestOperatorCredential_ReissueIssuesAFirstOrRefusesAnUnknownOperator
// covers the two edges of re-issuing: an operator registered before
// credentials existed holds none and is issued its first, and an
// operator the organization never registered is refused.
func TestOperatorCredential_ReissueIssuesAFirstOrRefusesAnUnknownOperator(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	subject, _ := registerCredentialed(t, store, "one")
	_, err := p.Exec(ctx, `DELETE FROM operator_credentials WHERE operator_grn = $1`, subject.String())
	require.NoError(t, err)
	require.Equal(t, 0, countRows(t, p,
		`SELECT count(*) FROM operator_credentials WHERE operator_grn = $1`, subject.String()),
		"the operator holds no credential, as one registered before 000038 does")

	first, err := operator.NewIssuedCredential(subject, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	require.NoError(t, store.ReissueCredential(ctx, first))
	_, err = exchangeService(store).Exchange(ctx, first.Value(), nil)
	assert.NoError(t, err)

	unknown, err := operator.NewIssuedCredential(scopedGRN("acme", operator.ServiceType, "never"), scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	assert.ErrorIs(t, store.ReissueCredential(ctx, unknown), operator.ErrOperatorNotFound)
}

// TestOperatorCredential_DeleteTakesTheCredentialAndItsTokens holds the
// other half of the cascade: deleting the operator leaves no credential
// to exchange and no token to resolve. The operator nobody deleted is
// the control.
func TestOperatorCredential_DeleteTakesTheCredentialAndItsTokens(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := exchangeService(store)
	deleted, deletedCredential := registerCredentialed(t, store, "one")
	_, keptCredential := registerCredentialed(t, store, "two")
	deletedToken, err := svc.Exchange(ctx, deletedCredential, nil)
	require.NoError(t, err)
	keptToken, err := svc.Exchange(ctx, keptCredential, nil)
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, deleted))

	_, err = store.ResolveToken(ctx, deletedToken.Hash())
	assert.ErrorIs(t, err, operator.ErrTokenInvalid)
	_, err = svc.Exchange(ctx, deletedCredential, nil)
	assert.ErrorIs(t, err, operator.ErrCredentialInvalid)
	_, err = store.ResolveToken(ctx, keptToken.Hash())
	assert.NoError(t, err, "the operator nobody deleted keeps its token")
	assert.Equal(t, 0, countRows(t, p,
		`SELECT count(*) FROM operator_tokens WHERE operator_grn = $1`, deleted.String()))
}

// TestOperatorToken_ExpiredAndSweptAndAudienceKept covers what a row
// says beyond its operator: a token past its expiry resolves to
// nothing, the next exchange sweeps it, and a token minted for a
// hosted operator resolves with that audience.
func TestOperatorToken_ExpiredAndSweptAndAudienceKept(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := exchangeService(store)
	subject, credential := registerCredentialed(t, store, "controller")
	hosted, _ := registerCredentialed(t, store, "control")

	expired, err := svc.Exchange(ctx, credential, nil)
	require.NoError(t, err)
	_, err = store.ResolveToken(ctx, expired.Hash())
	require.NoError(t, err, "the token resolves while it is live")
	_, err = p.Exec(ctx, `UPDATE operator_tokens SET expires_at = now() - interval '1 second' WHERE token_hash = $1`,
		expired.Hash())
	require.NoError(t, err)
	_, err = store.ResolveToken(ctx, expired.Hash())
	assert.ErrorIs(t, err, operator.ErrTokenInvalid, "an expired token resolves to nothing")

	audienced, err := svc.Exchange(ctx, credential, &hosted)
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

// exchangeOver exchanges credential on the listener at url and answers
// the token.
func exchangeOver(t *testing.T, client *http.Client, url, credential string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/tokens", strings.NewReader(`{}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Garam-Credential "+credential)
	req.Header.Set("Garam-Contract-Version", operator.TokenContractVersion)
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode, string(body))
	var token machinegen.MachineToken
	require.NoError(t, json.Unmarshal(body, &token))
	return token.Token
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
// ADR-0100's revocation end to end against real Postgres and the
// listener `serve machine` assembles: an operator's token
// authenticates, and the next request on the same connection is
// refused once the organization re-issues the credential, and once it
// deletes the operator. Every request travels over the connection the
// first one opened, so what refuses the later ones is the per-request
// read and not a new handshake.
//
// The third operator is the control: it presents a token over the same
// connection throughout and nothing is done to it, so what refused the
// other two is what was done to them and not the listener refusing
// every token after the first.
func TestOperatorToken_RefusedOnTheNextRequestOverAnOpenConnection(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	store := operator.NewPostgresStore(p)

	reissued, reissuedCredential := registerCredentialed(t, store, "reissued")
	deleted, deletedCredential := registerCredentialed(t, store, "deleted")
	_, keptCredential := registerCredentialed(t, store, "kept")
	client, connections := tokenClient(t)
	reissuedToken := exchangeOver(t, client, url, reissuedCredential)
	deletedToken := exchangeOver(t, client, url, deletedCredential)
	keptToken := exchangeOver(t, client, url, keptCredential)

	require.Equal(t, http.StatusOK, identifiesWith(t, client, url, reissuedToken))
	require.Equal(t, http.StatusOK, identifiesWith(t, client, url, deletedToken))
	require.Equal(t, http.StatusOK, identifiesWith(t, client, url, keptToken))

	next, err := operator.NewIssuedCredential(reissued, scopedGRN("acme", "user", "ada"))
	require.NoError(t, err)
	require.NoError(t, store.ReissueCredential(ctx, next))
	assert.Equal(t, http.StatusUnauthorized, identifiesWith(t, client, url, reissuedToken),
		"a re-issue refuses the previous credential's token on the next request")
	assert.Equal(t, http.StatusOK, identifiesWith(t, client, url, exchangeOver(t, client, url, next.Value())),
		"the new credential's token authenticates")

	require.NoError(t, store.Delete(ctx, deleted))
	assert.Equal(t, http.StatusUnauthorized, identifiesWith(t, client, url, deletedToken),
		"a delete refuses the operator's token on the next request")

	assert.Equal(t, http.StatusOK, identifiesWith(t, client, url, keptToken),
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
// is what refuses it — and on the renewal, which reads a certificate the token
// does not carry. The same token on GET /identity is the control.
func TestOperatorToken_ReachesTheOperatorRoutesAloneOnTheShippedListener(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	_, credential := registerCredentialed(t, operator.NewPostgresStore(p), "one")
	client, _ := tokenClient(t)
	token := exchangeOver(t, client, url, credential)

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

// TestOperatorToken_ACredentialHoldsItsNewestFiveTokens holds the bound
// on what exchanging writes (ADR-0100): seven exchanges leave five live
// tokens, the two oldest no longer resolve, and the newest five do —
// so what removed the two is the bound and not every token going.
func TestOperatorToken_ACredentialHoldsItsNewestFiveTokens(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	svc := exchangeService(store)
	subject, credential := registerCredentialed(t, store, "one")

	var tokens []*operator.Token
	for range 7 {
		token, err := svc.Exchange(ctx, credential, nil)
		require.NoError(t, err)
		tokens = append(tokens, token)
	}

	assert.Equal(t, operator.LiveTokensPerCredential, countRows(t, p,
		`SELECT count(*) FROM operator_tokens WHERE operator_grn = $1`, subject.String()))
	for i, token := range tokens {
		_, err := store.ResolveToken(ctx, token.Hash())
		if i < len(tokens)-operator.LiveTokensPerCredential {
			assert.ErrorIs(t, err, operator.ErrTokenInvalid, "token %d is past the bound", i)
		} else {
			assert.NoError(t, err, "token %d is among the newest", i)
		}
	}
}
