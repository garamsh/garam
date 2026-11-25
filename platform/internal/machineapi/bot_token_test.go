package machineapi_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/operator"
)

const actorValue = app.TokenPrefix + "actor"

// botForm is a well-formed bot token exchange (RFC 8693, ADR-0189).
func botForm() url.Values {
	return url.Values{
		"grant_type":         {cluster.GrantTypeTokenExchange},
		"subject_token":      {"grn:acme/iam/bot/decl-a"},
		"subject_token_type": {cluster.TokenTypeBot},
		"actor_token":        {actorValue},
		"actor_token_type":   {cluster.TokenTypeAccessToken},
	}
}

// serveBotExchange serves /oauth/token with clusters as the connected
// cluster's service, and operators as the operator service.
func serveBotExchange(t *testing.T, clusters *connectionClusters, operators *assertionService) string {
	t.Helper()
	base, _ := serveTLS(t, machineapi.NewServer(&agentService{}, operators, &messageService{}, unwatched(t),
		nil, nil, nil, nil, nil, nil, stubKeyring{}).WithInstances(clusters, clusters), connectedGRN)
	return base
}

// TestExchangeBotToken_AnswersRFC8693sTokenForm is the success path
// from the wire: the actor, the subject and the scope reach the cluster
// service as sent, and the answer is RFC 8693's, with issued_token_type
// and the hour the token is accepted for, uncached.
func TestExchangeBotToken_AnswersRFC8693sTokenForm(t *testing.T) {
	clusters, operators := &connectionClusters{}, &assertionService{}
	base := serveBotExchange(t, clusters, operators)

	resp, body := exchangeForm(t, anonymous(t), base, botForm().Encode(), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	var token map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	assert.Equal(t, map[string]any{
		"access_token": cluster.BotTokenPrefix + "answered", "token_type": "Bearer", "expires_in": float64(3600),
		"issued_token_type": cluster.TokenTypeAccessToken,
	}, token)
	require.NotNil(t, clusters.exchange)
	assert.Equal(t, cluster.BotExchange{ActorToken: actorValue, Subject: "grn:acme/iam/bot/decl-a"}, *clusters.exchange)
	assert.Empty(t, operators.calls, "no operator's exchange ran")

	form := botForm()
	form.Set("audience", connectionTokenURL)
	form.Set("requested_token_type", cluster.TokenTypeAccessToken)
	resp, body = exchangeForm(t, anonymous(t), base, form.Encode(), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "an audience and the access token type asked for: %s", body)
	require.NotNil(t, clusters.exchange.Audience, "the audience reaches the domain, which judges it")
	assert.Equal(t, connectionTokenURL, *clusters.exchange.Audience)
}

// TestExchangeBotToken_RefusesAFormOutsideTheExchange holds each form
// refusal to botForm as its control: nothing refused reaches the
// cluster service, and no refusal quotes the actor token.
func TestExchangeBotToken_RefusesAFormOutsideTheExchange(t *testing.T) {
	clusters := &connectionClusters{}
	base := serveBotExchange(t, clusters, &assertionService{})
	client := anonymous(t)
	resp, body := exchangeForm(t, client, base, botForm().Encode(), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "the control: %s", body)
	clusters.exchange = nil

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
			f.Set("subject_token_type", cluster.TokenTypeAccessToken)
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
	assert.Nil(t, clusters.exchange, "no refused form reached the cluster service")
}

// TestExchangeBotToken_TranslatesTheDomainsRefusals maps each domain
// answer to its OAuth status and code. RFC 8693 §2.2.2 makes an actor
// or subject token that is invalid or refused by policy
// invalid_request; another audience is invalid_target, a scope
// invalid_scope, and the bound is 429.
func TestExchangeBotToken_TranslatesTheDomainsRefusals(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"an actor that authenticates nothing", cluster.ErrActorTokenRefused, http.StatusBadRequest, "invalid_request"},
		{"a bot not placed on the record", cluster.ErrBotNotPlaced, http.StatusBadRequest, "invalid_request"},
		{"another audience", cluster.ErrAudienceRefused, http.StatusBadRequest, "invalid_target"},
		{"a scope", cluster.ErrScopeUndefined, http.StatusBadRequest, "invalid_scope"},
		{"the bound exceeded", &operator.Exceeded{RetryAfter: time.Second}, http.StatusTooManyRequests, "temporarily_unavailable"},
		{"a store that cannot be read", errorx.New(errorx.KindUnavailable, "postgres down"), http.StatusServiceUnavailable, "temporarily_unavailable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := serveBotExchange(t, &connectionClusters{err: tc.err}, &assertionService{})
			resp, body := exchangeForm(t, anonymous(t), base, botForm().Encode(), nil)
			assert.Equal(t, tc.status, resp.StatusCode, body)
			assert.Equal(t, tc.code, oauthError(t, body))
			assert.NotContains(t, body, "postgres")
		})
	}
}

// TestExchangeBotToken_IsUnavailableWithoutTheClusterService holds that
// a listener serving no connected cluster answers the exchange 503.
func TestExchangeBotToken_IsUnavailableWithoutTheClusterService(t *testing.T) {
	base, _ := listener(t, &agentService{}, &assertionService{}, &messageService{}, unwatched(t), &decider{}, &roster{}, assertingClient)
	resp, body := exchangeForm(t, anonymous(t), base, botForm().Encode(), nil)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode, body)
	assert.Equal(t, "temporarily_unavailable", oauthError(t, body))
}
