package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// tokenCall performs method on url presenting token over client, with
// the contract value where one is given, and answers the status and the
// body.
func tokenCall(t *testing.T, client *http.Client, method, url, token, contract, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Garam-Token "+token)
	if contract != "" {
		req.Header.Set("Garam-Contract-Version", contract)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	answered, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, string(answered)
}

// createdBy creates an agent on the listener with the operator's token
// and answers its GRN.
func createdBy(t *testing.T, client *http.Client, url, operatorToken string) grn.GRN {
	t.Helper()
	status, body := tokenCall(t, client, http.MethodPost, url+"/agents", operatorToken, "", "")
	require.Equal(t, http.StatusCreated, status, body)
	var created machinegen.AgentAssignment
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	g, err := grn.Parse(created.Grn)
	require.NoError(t, err)
	return g
}

// mintedFor mints the agent a token with the operator's token and
// answers it.
func mintedFor(t *testing.T, client *http.Client, url, operatorToken string, agentGRN grn.GRN) machinegen.AgentToken {
	t.Helper()
	status, body := tokenCall(t, client, http.MethodPost, url+"/agents/"+agentGRN.String()+"/tokens",
		operatorToken, operator.TokenContractVersion, `{}`)
	require.Equal(t, http.StatusCreated, status, body)
	var token machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	return token
}

// TestAgentToken_ReassignmentRefusesTheNextRequestOverAnOpenConnection
// is ADR-0100's agent token end to end against Postgres and the
// listener `serve machine` assembles: an operator mints its agent a
// token, the agent claims on its own queue with it — a protected write
// the execution fence (ADR-0080) admits on the token's serial — and the
// next claim on the same connection is refused once the agent is
// reassigned. The token the new assignee mints is the control: it
// claims, so what refused the first is the reassignment and not the
// route refusing every agent token.
func TestAgentToken_ReassignmentRefusesTheNextRequestOverAnOpenConnection(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	operators := operator.NewPostgresStore(p)
	_, firstCredential := registerCredentialed(t, operators, "first")
	second, secondCredential := registerCredentialed(t, operators, "second")

	client, connections := tokenClient(t)
	firstToken := exchangeOver(t, client, url, firstCredential)
	bot := createdBy(t, client, url, firstToken)
	minted := mintedFor(t, client, url, firstToken, bot)
	assert.Equal(t, bot.String(), minted.Grn)
	assert.True(t, agent.WellFormedAccessToken(minted.Token))
	assert.NotEmpty(t, minted.ServerRootPem)

	claims := url + "/agents/" + bot.String() + "/claims"
	status, body := tokenCall(t, client, http.MethodPost, claims, minted.Token, "", "")
	require.Equal(t, http.StatusNoContent, status, "the agent claims on its own queue with its token: %s", body)

	// The second operator holds nothing yet, so it mints nothing.
	secondToken := exchangeOver(t, client, url, secondCredential)
	status, _ = tokenCall(t, client, http.MethodPost, url+"/agents/"+bot.String()+"/tokens",
		secondToken, operator.TokenContractVersion, `{}`)
	assert.Equal(t, http.StatusForbidden, status, "an operator mints only for an agent it holds")

	_, err := agent.NewPostgresStore(p).Assign(ctx, bot, second)
	require.NoError(t, err)

	status, _ = tokenCall(t, client, http.MethodPost, claims, minted.Token, "", "")
	assert.Equal(t, http.StatusUnauthorized, status,
		"the reassignment refuses the token on the next request over the open connection")

	again := mintedFor(t, client, url, secondToken, bot)
	assert.Equal(t, minted.Epoch+1, again.Epoch, "the new assignee mints at the new epoch")
	status, body = tokenCall(t, client, http.MethodPost, claims, again.Token, "", "")
	assert.Equal(t, http.StatusNoContent, status, "the new assignee's token claims: %s", body)

	status, _ = tokenCall(t, client, http.MethodPost, url+"/agents/"+bot.String()+"/tokens",
		firstToken, operator.TokenContractVersion, `{}`)
	assert.Equal(t, http.StatusForbidden, status, "the operator the reassignment replaced mints nothing")
	assert.Equal(t, 1, *connections, "every request travelled over one connection")
}

// TestAgentToken_AnAgentHoldsItsNewestFiveTokens holds the bound on
// what minting writes (ADR-0100): seven mints leave five live tokens
// for the audience, the two oldest no longer authenticate, and the
// newest does.
func TestAgentToken_AnAgentHoldsItsNewestFiveTokens(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	_, credential := registerCredentialed(t, operator.NewPostgresStore(p), "one")
	client, _ := tokenClient(t)
	operatorToken := exchangeOver(t, client, url, credential)
	bot := createdBy(t, client, url, operatorToken)

	var tokens []string
	for range 7 {
		tokens = append(tokens, mintedFor(t, client, url, operatorToken, bot).Token)
	}

	assert.Equal(t, agent.LiveAccessTokensPerAudience, countRows(t, p,
		`SELECT count(*) FROM agent_tokens WHERE agent_grn = $1`, bot.String()))
	claims := url + "/agents/" + bot.String() + "/claims"
	status, _ := tokenCall(t, client, http.MethodPost, claims, tokens[0], "", "")
	assert.Equal(t, http.StatusUnauthorized, status, "the oldest is past the bound")
	status, _ = tokenCall(t, client, http.MethodPost, claims, tokens[6], "", "")
	assert.Equal(t, http.StatusNoContent, status, "the newest claims")
}
