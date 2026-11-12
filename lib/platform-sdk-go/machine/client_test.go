package machine_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/platform-sdk-go/machine"
)

const agentGRN = "grn:garam:acme:agent/a1"

// newClient points the generated client at a server answering with
// handler, so each test reads the typed answer the client decodes.
func newClient(t *testing.T, handler http.HandlerFunc) *machine.ClientWithResponses {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := machine.NewClientWithResponses(server.URL)
	require.NoError(t, err)
	return client
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(body))
}

func assertionRequest() machine.ClientAssertionRequest {
	return machine.ClientAssertionRequest{
		ClientAssertion:     "header.payload.signature",
		ClientAssertionType: machine.ClientAssertionRequestClientAssertionTypeUrnIetfParamsOauthClientAssertionTypeJwtBearer,
		ClientId:            "grn:garam:acme:operator/op1",
		GrantType:           machine.ClientCredentials,
	}
}

func TestExchangeClientAssertion_ReadsToken(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/oauth/token", r.URL.Path)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "client_credentials", r.PostForm.Get("grant_type"))
		assert.Equal(t, "header.payload.signature", r.PostForm.Get("client_assertion"))
		assert.Equal(t, "grn:garam:acme:operator/op1", r.PostForm.Get("client_id"))
		writeJSON(t, w, http.StatusOK, map[string]any{
			"access_token": "garam_ot_abc",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	})

	resp, err := client.ExchangeClientAssertionWithFormdataBodyWithResponse(context.Background(), assertionRequest())

	require.NoError(t, err)
	require.NotNil(t, resp.JSON200)
	assert.Equal(t, "garam_ot_abc", resp.JSON200.AccessToken)
	assert.Equal(t, 3600, resp.JSON200.ExpiresIn)
	assert.Equal(t, machine.Bearer, resp.JSON200.TokenType)
	assert.Nil(t, resp.JSON400)
}

func TestExchangeClientAssertion_ReadsUnauthorizedClient(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusBadRequest, map[string]any{
			"error":             "unauthorized_client",
			"error_description": "organization not held",
		})
	})

	resp, err := client.ExchangeClientAssertionWithFormdataBodyWithResponse(context.Background(), assertionRequest())

	require.NoError(t, err)
	require.NotNil(t, resp.JSON400)
	assert.Equal(t, machine.UnauthorizedClient, resp.JSON400.Error)
	require.NotNil(t, resp.JSON400.ErrorDescription)
	assert.Equal(t, "organization not held", *resp.JSON400.ErrorDescription)
	assert.Nil(t, resp.JSON200)
}

func TestMintAgentToken_ReadsAgentToken(t *testing.T) {
	audience := "grn:garam:acme:operator/op2"
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/agents/"+agentGRN+"/tokens", r.URL.Path)
		assert.Equal(t, "machine-token.v1", r.Header.Get("Garam-Contract-Version"))
		assert.Equal(t, "Bearer garam_ot_abc", r.Header.Get("Authorization"))
		var body machine.TokenExchange
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, &audience, body.Audience)
		w.Header().Set("Garam-Contract-Version", "machine-token.v1")
		writeJSON(t, w, http.StatusCreated, map[string]any{
			"audience":      audience,
			"epoch":         3,
			"expiresAt":     "2030-01-02T03:04:05Z",
			"grn":           agentGRN,
			"serverRootPem": "-----BEGIN CERTIFICATE-----",
			"token":         "garam_at_xyz",
		})
	})
	bearer := func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer garam_ot_abc")
		return nil
	}

	resp, err := client.MintAgentTokenWithResponse(
		context.Background(), agentGRN,
		&machine.MintAgentTokenParams{TokenContractVersion: "machine-token.v1"},
		machine.TokenExchange{Audience: &audience}, bearer)

	require.NoError(t, err)
	require.NotNil(t, resp.JSON201)
	assert.Equal(t, "garam_at_xyz", resp.JSON201.Token)
	assert.Equal(t, agentGRN, resp.JSON201.Grn)
	assert.Equal(t, 3, resp.JSON201.Epoch)
	assert.Equal(t, 2030, resp.JSON201.ExpiresAt.Year())
}

func TestMintAgentToken_ReadsRefusal(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusForbidden, map[string]any{
			"kind":    "permission_denied",
			"message": "agent not held",
		})
	})

	resp, err := client.MintAgentTokenWithResponse(
		context.Background(), agentGRN,
		&machine.MintAgentTokenParams{TokenContractVersion: "machine-token.v1"},
		machine.TokenExchange{})

	require.NoError(t, err)
	assert.Equal(t, http.StatusForbidden, resp.StatusCode())
	require.NotNil(t, resp.JSON403)
	assert.Equal(t, "permission_denied", resp.JSON403.Kind)
	assert.Nil(t, resp.JSON201)
}

func TestReportProvisioningState_ReadsNoContent(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/agents/"+agentGRN+"/provisioning-state", r.URL.Path)
		var body machine.ProvisioningStateReport
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, 4, body.Epoch)
		assert.Equal(t, machine.ProvisioningStateReady, body.State)
		w.WriteHeader(http.StatusNoContent)
	})

	resp, err := client.ReportProvisioningStateWithResponse(
		context.Background(), agentGRN,
		machine.ProvisioningStateReport{Epoch: 4, State: machine.ProvisioningStateReady})

	require.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode())
	assert.Nil(t, resp.JSON409)
}

func TestReportProvisioningState_ReadsStaleEpoch(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusConflict, map[string]any{
			"kind":    "failed_precondition",
			"message": "stale epoch",
		})
	})

	resp, err := client.ReportProvisioningStateWithResponse(
		context.Background(), agentGRN,
		machine.ProvisioningStateReport{Epoch: 1, State: machine.ProvisioningStateReady})

	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, resp.StatusCode())
	require.NotNil(t, resp.JSON409)
	assert.Equal(t, "failed_precondition", resp.JSON409.Kind)
	assert.Equal(t, "stale epoch", resp.JSON409.Message)
}
