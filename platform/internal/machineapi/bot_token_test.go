package machineapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/dpop"
	"github.com/garamsh/garam/platform/internal/dpop/dpoptest"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/operator"
)

const actorValue = app.TokenPrefix + "actor"

// botForm is a well-formed bot token exchange (RFC 8693, ADR-0189).
func botForm() url.Values {
	return url.Values{
		"grant_type":         {bot.GrantTypeTokenExchange},
		"subject_token":      {"grn:acme/iam/bot/decl-a"},
		"subject_token_type": {bot.TokenTypeBot},
		"actor_token":        {actorValue},
		"actor_token_type":   {bot.TokenTypeAccessToken},
	}
}

// serveBotExchange serves /oauth/token with bots as the bot
// domain's service, and operators as the operator service, verifying
// DPoP proofs for connectionTokenURL against a store in memory.
func serveBotExchange(t *testing.T, bots *connectionClusters, operators *assertionService) string {
	t.Helper()
	proofs := dpop.NewVerifier(dpoptest.NewStore(nil), nil)
	base, _ := serveTLS(t, machineapi.NewServer(&agentService{}, operators, &messageService{}, unwatched(t),
		nil, nil, nil, nil, nil, nil, stubKeyring{}).WithInstances(bots, bots, proofs, connectionTokenURL, ""), connectedGRN)
	return base
}

// exchangeProved posts body to base's token endpoint with each of
// proofs as a DPoP header.
func exchangeProved(t *testing.T, client *http.Client, base, body string, proofs ...string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/oauth/token", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, proof := range proofs {
		req.Header.Add(dpop.Header, proof)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(got)
}

// nonceFor makes the round trip a connector's first exchange makes: a
// proof by key without a nonce, answered use_dpop_nonce with the nonce
// the next proof carries.
func nonceFor(t *testing.T, client *http.Client, base string, key *dpoptest.Key) string {
	t.Helper()
	resp, body := exchangeProved(t, client, base, botForm().Encode(),
		key.Proof(t, http.MethodPost, connectionTokenURL, "", ""))
	require.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
	require.Equal(t, "use_dpop_nonce", oauthError(t, body))
	nonce := resp.Header.Get(dpop.NonceHeader)
	require.NotEmpty(t, nonce, "the refusal carries the nonce to retry with")
	return nonce
}

// exchangeBound makes the bot exchange body with key's proof, the
// nonce learnt first.
func exchangeBound(t *testing.T, client *http.Client, base, body string, key *dpoptest.Key) (*http.Response, string) {
	t.Helper()
	nonce := nonceFor(t, client, base, key)
	return exchangeProved(t, client, base, body, key.Proof(t, http.MethodPost, connectionTokenURL, nonce, ""))
}

// TestExchangeBotToken_AnswersRFC8693sTokenForm is the success path
// from the wire: the actor, the subject and the scope reach the bot
// service as sent, with the thumbprint of the key the proof was
// signed by, and the answer is RFC 8693's, with issued_token_type,
// token_type DPoP and the hour the token is accepted for, uncached.
func TestExchangeBotToken_AnswersRFC8693sTokenForm(t *testing.T) {
	bots, operators := &connectionClusters{}, &assertionService{}
	base := serveBotExchange(t, bots, operators)
	key := dpoptest.NewKey(t)

	resp, body := exchangeBound(t, anonymous(t), base, botForm().Encode(), key)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	var token map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	assert.Equal(t, map[string]any{
		"access_token": bot.BotTokenPrefix + "answered", "token_type": "DPoP", "expires_in": float64(3600),
		"issued_token_type": bot.TokenTypeAccessToken,
	}, token)
	require.NotNil(t, bots.exchange)
	assert.Equal(t, key.Thumbprint(t), bots.exchange.KeyThumbprint)
	assert.NotNil(t, bots.exchange.SpendProof, "the domain spends the proof")
	bots.exchange.SpendProof = nil
	assert.Equal(t, bot.BotExchange{ActorToken: actorValue, Subject: "grn:acme/iam/bot/decl-a",
		KeyThumbprint: key.Thumbprint(t)}, *bots.exchange)
	assert.Empty(t, operators.calls, "no operator's exchange ran")

	form := botForm()
	form.Set("audience", connectionTokenURL)
	form.Set("requested_token_type", bot.TokenTypeAccessToken)
	resp, body = exchangeBound(t, anonymous(t), base, form.Encode(), key)
	require.Equal(t, http.StatusOK, resp.StatusCode, "an audience and the access token type asked for: %s", body)
	require.NotNil(t, bots.exchange.Audience, "the audience reaches the domain, which judges it")
	assert.Equal(t, connectionTokenURL, *bots.exchange.Audience)
}

// TestExchangeBotToken_RefusesAFormOutsideTheExchange holds each form
// refusal to botForm as its control: nothing refused reaches the
// bot service, and no refusal quotes the actor token.
func TestExchangeBotToken_RefusesAFormOutsideTheExchange(t *testing.T) {
	bots := &connectionClusters{}
	base := serveBotExchange(t, bots, &assertionService{})
	client := anonymous(t)
	resp, body := exchangeBound(t, client, base, botForm().Encode(), dpoptest.NewKey(t))
	require.Equal(t, http.StatusOK, resp.StatusCode, "the control: %s", body)
	bots.exchange = nil

	changed := func(change func(url.Values)) string {
		f := botForm()
		change(f)
		return f.Encode()
	}
	tests := []struct {
		name    string
		body    string
		headers map[string]string
		code    string
	}{
		{"a resource", changed(func(f url.Values) { f.Set("resource", "https://ai.garam.sh") }), nil, "invalid_target"},
		{"another requested token type", changed(func(f url.Values) {
			f.Set("requested_token_type", "urn:ietf:params:oauth:token-type:jwt")
		}), nil, "invalid_request"},
		{"a client_id", changed(func(f url.Values) { f.Set("client_id", connectedGRN.String()) }), nil, "invalid_request"},
		{"a client assertion", changed(func(f url.Values) { f.Set("client_assertion", "a.b.c") }), nil, "invalid_request"},
		{"a client assertion type", changed(func(f url.Values) { f.Set("client_assertion_type", operator.ClientAssertionTypeJWT) }), nil, "invalid_request"},
		{"a client secret", changed(func(f url.Values) { f.Set("client_secret", "s") }), nil, "invalid_request"},
		{"another subject token type", changed(func(f url.Values) {
			f.Set("subject_token_type", bot.TokenTypeAccessToken)
		}), nil, "invalid_request"},
		{"no subject token", changed(func(f url.Values) { f.Del("subject_token") }), nil, "invalid_request"},
		{"another actor token type", changed(func(f url.Values) {
			f.Set("actor_token_type", "urn:ietf:params:oauth:token-type:jwt")
		}), nil, "invalid_request"},
		{"no actor token", changed(func(f url.Values) { f.Del("actor_token") }), nil, "invalid_request"},
		{"a repeated subject", botForm().Encode() + "&subject_token=grn%3Aacme%2Fiam%2Fbot%2Fdecl-b", nil, "invalid_request"},
		{"the actor as a bearer", botForm().Encode(), map[string]string{"Authorization": "Bearer " + actorValue}, "invalid_request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := exchangeForm(t, client, base, tc.body, tc.headers)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
			assert.Equal(t, tc.code, oauthError(t, body))
			assert.NotContains(t, body, actorValue)
		})
	}
	assert.Nil(t, bots.exchange, "no refused form reached the bot service")
}

// TestExchangeBotToken_TranslatesTheDomainsRefusals maps each domain
// answer to its OAuth status and code. RFC 8693 §2.2.2 makes an actor
// or subject token that is invalid or refused by policy
// invalid_request, an actor whose installation hosts no bots among
// them; another audience is invalid_target, a scope
// invalid_scope, and the bound is 429.
func TestExchangeBotToken_TranslatesTheDomainsRefusals(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"an actor that authenticates nothing", bot.ErrActorTokenRefused, http.StatusBadRequest, "invalid_request"},
		{"a bot not placed on the record", bot.ErrBotNotPlaced, http.StatusBadRequest, "invalid_request"},
		{"an actor whose installation hosts no bots", bot.ErrNotHosting, http.StatusBadRequest, "invalid_request"},
		{"another audience", bot.ErrAudienceRefused, http.StatusBadRequest, "invalid_target"},
		{"a scope", bot.ErrScopeUndefined, http.StatusBadRequest, "invalid_scope"},
		{"the bound exceeded", &operator.Exceeded{RetryAfter: time.Second}, http.StatusTooManyRequests, "temporarily_unavailable"},
		{"a store that cannot be read", errorx.New(errorx.KindUnavailable, "postgres down"), http.StatusServiceUnavailable, "temporarily_unavailable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := serveBotExchange(t, &connectionClusters{err: tc.err}, &assertionService{})
			resp, body := exchangeBound(t, anonymous(t), base, botForm().Encode(), dpoptest.NewKey(t))
			assert.Equal(t, tc.status, resp.StatusCode, body)
			assert.Equal(t, tc.code, oauthError(t, body))
			assert.NotContains(t, body, "postgres")
		})
	}
}

// TestExchangeBotToken_IsUnavailableWithoutTheBotService holds that
// a listener serving no bot domain answers the exchange 503.
func TestExchangeBotToken_IsUnavailableWithoutTheBotService(t *testing.T) {
	base, _ := listener(t, &agentService{}, &assertionService{}, &messageService{}, unwatched(t), &decider{}, &roster{}, assertingClient)
	resp, body := exchangeForm(t, anonymous(t), base, botForm().Encode(), nil)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, body)
	assert.Equal(t, "temporarily_unavailable", oauthError(t, body))
}

// TestExchangeBotToken_TheNonceRoundTrip holds RFC 9449 §8 at the
// token endpoint (ADR-0191 decision 5): a proof without a nonce, or
// with one garam did not issue, is answered 400 use_dpop_nonce with a
// nonce, the retry carrying it is accepted, and every answer carries a
// nonce, the same one within a period. Nothing refused reaches the
// bot service.
func TestExchangeBotToken_TheNonceRoundTrip(t *testing.T) {
	bots := &connectionClusters{}
	base := serveBotExchange(t, bots, &assertionService{})
	client, key := anonymous(t), dpoptest.NewKey(t)

	first := nonceFor(t, client, base, key)
	resp, body := exchangeProved(t, client, base, botForm().Encode(),
		key.Proof(t, http.MethodPost, connectionTokenURL, "not-garams", ""))
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
	assert.Equal(t, "use_dpop_nonce", oauthError(t, body), "a nonce garam did not issue")
	assert.Nil(t, bots.exchange, "no proof without garam's nonce reached the bot service")

	resp, body = exchangeProved(t, client, base, botForm().Encode(),
		key.Proof(t, http.MethodPost, connectionTokenURL, first, ""))
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Equal(t, first, resp.Header.Get(dpop.NonceHeader), "the answer carries the period's nonce")
	require.NotNil(t, bots.exchange)
	assert.Equal(t, key.Thumbprint(t), bots.exchange.KeyThumbprint)
}

// TestExchangeBotToken_RefusesAProofThatDoesNotNameTheExchange holds
// ADR-0191 decisions 3 and 4 at the token endpoint, each with
// garam's nonce: no proof, two, one for another method or another
// URL, and one whose header is not a proof's are 400
// invalid_dpop_proof, with a nonce beside, and reach no bot service;
// a replay is 400 invalid_dpop_proof once the domain spends it. A
// fresh proof is the control.
func TestExchangeBotToken_RefusesAProofThatDoesNotNameTheExchange(t *testing.T) {
	bots := &connectionClusters{}
	base := serveBotExchange(t, bots, &assertionService{})
	client, key := anonymous(t), dpoptest.NewKey(t)
	nonce := nonceFor(t, client, base, key)
	spent := key.Proof(t, http.MethodPost, connectionTokenURL, nonce, "")
	resp, body := exchangeProved(t, client, base, botForm().Encode(), spent)
	require.Equal(t, http.StatusOK, resp.StatusCode, "the control: %s", body)
	bots.exchange = nil

	typ := key.Header(t)
	typ["typ"] = "JWT"
	claims := dpoptest.Claims(http.MethodPost, connectionTokenURL, time.Now())
	claims["nonce"] = nonce
	for name, proofs := range map[string][]string{
		"no proof":  nil,
		"two":       {key.Proof(t, http.MethodPost, connectionTokenURL, nonce, ""), key.Proof(t, http.MethodPost, connectionTokenURL, nonce, "")},
		"wrong htm": {key.Proof(t, http.MethodPut, connectionTokenURL, nonce, "")},
		"wrong htu": {key.Proof(t, http.MethodPost, base+"/oauth/token", nonce, "")},
		"not a JWS": {"a.b"},
		"wrong typ": {key.Sign(t, typ, claims)},
	} {
		t.Run(name, func(t *testing.T) {
			resp, body := exchangeProved(t, client, base, botForm().Encode(), proofs...)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
			assert.Equal(t, "invalid_dpop_proof", oauthError(t, body))
			assert.Contains(t, body, `"error_description":"the DPoP proof is invalid"`,
				"the description is fixed, naming no check")
			assert.NotEmpty(t, resp.Header.Get(dpop.NonceHeader), "a refusal carries a nonce too")
		})
	}
	assert.Nil(t, bots.exchange, "no refused proof reached the bot service")

	resp, body = exchangeProved(t, client, base, botForm().Encode(), spent)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
	assert.Equal(t, "invalid_dpop_proof", oauthError(t, body), "a replay, refused when the domain spends it")
}

// refusingActors is a credential store that authenticates no actor.
type refusingActors struct{ app.CredentialStore }

func (refusingActors) ResolveToken(context.Context, string) (app.TokenHolder, error) {
	return app.TokenHolder{}, app.ErrTokenInvalid
}

// TestExchangeBotToken_AnUnauthenticatedFloodWritesNoJTI holds
// ADR-0191 decisions 4 and 5 against a caller garam does not know:
// each of many requests, each with its own key, garam's nonce and a
// proof that verifies but an actor token that authenticates nothing,
// is refused, and garam holds no jti for any of them and the one
// nonce of the period. The real bot service decides the actor.
func TestExchangeBotToken_AnUnauthenticatedFloodWritesNoJTI(t *testing.T) {
	store := dpoptest.NewStore(nil)
	bots := bot.NewService(refusingActors{}, nil, nil, nil, bot.Config{TokenEndpoint: connectionTokenURL})
	base, _ := serveTLS(t, machineapi.NewServer(&agentService{}, &assertionService{}, &messageService{}, unwatched(t),
		nil, nil, nil, nil, nil, nil, stubKeyring{}).
		WithInstances(nil, bots, dpop.NewVerifier(store, nil), connectionTokenURL, ""), connectedGRN)
	client := anonymous(t)
	form := botForm()
	form.Set("actor_token", app.TokenPrefix+strings.Repeat("ab", 32))
	for range 50 {
		resp, body := exchangeBound(t, client, base, form.Encode(), dpoptest.NewKey(t))
		require.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
		require.Equal(t, "invalid_request", oauthError(t, body))
	}
	jtis, nonces := store.Held()
	assert.Zero(t, jtis, "no unauthenticated proof was spent")
	assert.LessOrEqual(t, nonces, 2, "the period's nonce, and at most the previous one")
}
