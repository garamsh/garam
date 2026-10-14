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

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// assertionService is an operator.Service answering the client
// assertion exchange with err, or a token where err is nil, and
// recording what the surface passed it.
type assertionService struct {
	operator.Service
	err   error
	calls []assertionCall
}

type assertionCall struct {
	client    grn.GRN
	assertion string
	audience  *grn.GRN
}

func (s *assertionService) ExchangeAssertion(_ context.Context, client grn.GRN, assertion string, audience *grn.GRN) (*operator.Token, error) {
	s.calls = append(s.calls, assertionCall{client: client, assertion: assertion, audience: audience})
	if s.err != nil {
		return nil, s.err
	}
	return &operator.Token{Value: operator.TokenPrefix + "answered", Operator: client, Audience: audience,
		ExpiresAt: time.Now().Add(operator.TokenTTL)}, nil
}

var assertingClient = grn.New("acme", grn.DefaultProject, operator.ServiceType, "controller")

// validForm is a well-formed client credentials request.
func validForm() url.Values {
	return url.Values{
		"grant_type":            {operator.GrantTypeClientCredentials},
		"client_id":             {assertingClient.String()},
		"client_assertion_type": {operator.ClientAssertionTypeJWT},
		"client_assertion":      {"a.compact.jwt"},
	}
}

// exchangeForm posts body to /oauth/token over client with the form
// content type and any extra headers, answering the response with its
// body read.
func exchangeForm(t *testing.T, client *http.Client, base, body string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/oauth/token", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(got)
}

// oauthError reads the error code of an OAuth error body.
func oauthError(t *testing.T, body string) string {
	t.Helper()
	var e struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &e), body)
	return e.Error
}

// TestExchangeClientAssertion_AnswersRFC6749sTokenForm is the success
// path from the wire (ADR-0102): the form's identity, assertion and
// audience reach the domain as sent, and the answer is the standard
// token response, uncached, under machine-token.v2.
func TestExchangeClientAssertion_AnswersRFC6749sTokenForm(t *testing.T) {
	operators := &assertionService{}
	base, _ := listener(t, &agentService{}, operators, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, assertingClient)
	form := validForm()
	form.Set("audience", "grn:acme:default:operator:control")

	resp, body := exchangeForm(t, anonymous(t), base, form.Encode(), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	assert.Equal(t, "no-cache", resp.Header.Get("Pragma"))
	assert.Equal(t, operator.AssertionContractVersion, resp.Header.Get(operator.ContractVersionHeader))
	var token map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	assert.Equal(t, map[string]any{
		"access_token": operator.TokenPrefix + "answered", "token_type": "Bearer", "expires_in": float64(3600),
	}, token)

	require.Len(t, operators.calls, 1)
	assert.Equal(t, assertingClient, operators.calls[0].client)
	assert.Equal(t, "a.compact.jwt", operators.calls[0].assertion)
	require.NotNil(t, operators.calls[0].audience)
	assert.Equal(t, "grn:acme:default:operator:control", operators.calls[0].audience.String())
}

// TestExchangeClientAssertion_RefusesAFormOutsideTheProfile holds each
// form refusal to the valid form as its control: the same request with
// the one change undone reaches the domain. Nothing refused here
// reaches it, and no refusal quotes the body.
func TestExchangeClientAssertion_RefusesAFormOutsideTheProfile(t *testing.T) {
	operators := &assertionService{}
	base, certified := listener(t, &agentService{}, operators, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, assertingClient)
	client := anonymous(t)

	resp, body := exchangeForm(t, client, base, validForm().Encode(), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, "the control: %s", body)
	operators.calls = nil

	changed := func(change func(url.Values)) string {
		f := validForm()
		change(f)
		return f.Encode()
	}
	tests := []struct {
		name    string
		body    string
		headers map[string]string
		code    string
	}{
		{"a repeated parameter", validForm().Encode() + "&client_id=" + url.QueryEscape(assertingClient.String()), nil, "invalid_request"},
		{"a repeated assertion", validForm().Encode() + "&client_assertion=b.c.d", nil, "invalid_request"},
		{"another grant", changed(func(f url.Values) { f.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer") }), nil, "unsupported_grant_type"},
		{"no grant", changed(func(f url.Values) { f.Del("grant_type") }), nil, "invalid_request"},
		{"a scope", changed(func(f url.Values) { f.Set("scope", "admin") }), nil, "invalid_scope"},
		{"a client secret", changed(func(f url.Values) { f.Set("client_secret", "s") }), nil, "invalid_request"},
		{"another assertion type", changed(func(f url.Values) {
			f.Set("client_assertion_type", "urn:ietf:params:oauth:client-assertion-type:saml2-bearer")
		}), nil, "invalid_request"},
		{"no assertion", changed(func(f url.Values) { f.Del("client_assertion") }), nil, "invalid_request"},
		{"no client_id", changed(func(f url.Values) { f.Del("client_id") }), nil, "invalid_request"},
		{"a client_id that is no GRN", changed(func(f url.Values) { f.Set("client_id", "controller") }), nil, "invalid_request"},
		{"an audience that is no GRN", changed(func(f url.Values) { f.Set("audience", "control") }), nil, "invalid_request"},
		{"a JSON body", `{"grant_type":"client_credentials"}`, map[string]string{"Content-Type": "application/json"}, "invalid_request"},
		{"basic authentication", validForm().Encode(), map[string]string{"Authorization": "Basic Y29udHJvbGxlcjpzZWNyZXQ="}, "invalid_request"},
		{"a bearer token beside it", validForm().Encode(), map[string]string{"Authorization": "Bearer garam_ot_x"}, "invalid_request"},
		{"a body past its bound", changed(func(f url.Values) { f.Set("client_assertion", strings.Repeat("a", 13<<10)) }), nil, "invalid_request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := exchangeForm(t, client, base, tc.body, tc.headers)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
			assert.Equal(t, tc.code, oauthError(t, body))
			assert.Equal(t, "no-store", resp.Header.Get("Cache-Control"), "a refusal is not cached either")
			assert.NotContains(t, body, "a.compact.jwt")
		})
	}

	t.Run("a client certificate", func(t *testing.T) {
		resp, body := exchangeForm(t, certified, base, validForm().Encode(), nil)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
		assert.Equal(t, "invalid_request", oauthError(t, body))
		assert.Contains(t, body, "client certificate")
	})
	assert.Empty(t, operators.calls, "no refused form reached the domain")
}

// TestExchangeClientAssertion_TranslatesTheDomainsRefusals maps each
// domain answer to its OAuth status and code (ADR-0102): one
// invalid_client for every authentication failure, 429 with
// Retry-After, 503 where the profile or the store is unavailable.
func TestExchangeClientAssertion_TranslatesTheDomainsRefusals(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"an assertion that authenticates nothing", operator.ErrClientInvalid, http.StatusBadRequest, "invalid_client"},
		{"an audience outside the organization", operator.ErrAudienceInvalid, http.StatusBadRequest, "invalid_request"},
		{"the profile unconfigured", operator.ErrAssertionProfileUnavailable, http.StatusServiceUnavailable, "temporarily_unavailable"},
		{"a store that cannot be read", errorx.New(errorx.KindUnavailable, "postgres down"), http.StatusServiceUnavailable, "temporarily_unavailable"},
		{"a defect", errorx.New(errorx.KindInternal, "bug"), http.StatusInternalServerError, "server_error"},
		{"the bound exceeded", &operator.Exceeded{RetryAfter: 1500 * time.Millisecond}, http.StatusTooManyRequests, "temporarily_unavailable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			operators := &assertionService{err: tc.err}
			base, _ := listener(t, &agentService{}, operators, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, assertingClient)
			resp, body := exchangeForm(t, anonymous(t), base, validForm().Encode(), nil)
			assert.Equal(t, tc.status, resp.StatusCode, body)
			assert.Equal(t, tc.code, oauthError(t, body))
			assert.NotContains(t, body, "postgres", "a cause is never answered")
			if tc.status == http.StatusTooManyRequests {
				assert.Equal(t, "2", resp.Header.Get("Retry-After"), "rounded up to whole seconds")
			}
		})
	}
}
