package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
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

// agentCall performs method on url as an agent: presenting token and
// the activation's execution token beside it, as its adapter does.
func agentCall(t *testing.T, client *http.Client, method, url, token string, execution agent.Token) int {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Garam-Token "+token)
	req.Header.Set("Garam-Execution", execution.String())
	resp, err := client.Do(req)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode
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
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	operators := operator.NewPostgresStore(p)
	first, firstKey := keyed(t, operators, "first")
	second, secondKey := keyed(t, operators, "second")

	client, connections := tokenClient(t)
	firstToken := exchangeKey(t, client, url, first, firstKey, nil)
	f, a1, execution := lineaged(t, p, first)
	bot := f.agent
	minted := mintedFor(t, client, url, firstToken, bot)
	assert.Equal(t, bot.String(), minted.Grn)
	assert.True(t, agent.WellFormedAccessToken(minted.Token))
	assert.NotEmpty(t, minted.ServerRootPem)

	claims := url + "/agents/" + bot.String() + "/claims"
	status := agentCall(t, client, http.MethodPost, claims, minted.Token, execution)
	require.Equal(t, http.StatusNoContent, status, "the agent claims on its own queue with its token")

	// The second operator holds nothing yet, so it mints nothing.
	secondToken := exchangeKey(t, client, url, second, secondKey, nil)
	status, _ = tokenCall(t, client, http.MethodPost, url+"/agents/"+bot.String()+"/tokens",
		secondToken, operator.TokenContractVersion, `{}`)
	assert.Equal(t, http.StatusForbidden, status, "an operator mints only for an agent it holds")

	_, err := agent.NewPostgresStore(p).Assign(ctx, bot, second)
	require.NoError(t, err)

	status = agentCall(t, client, http.MethodPost, claims, minted.Token, execution)
	assert.Equal(t, http.StatusUnauthorized, status,
		"the reassignment refuses the token on the next request over the open connection")

	// The reassignment ended the activation, so the new assignee
	// activates the agent again before its token is of use.
	f.controller = second
	_, execution = f.activate(t, "after-reassignment", generation(2), &a1.ID, f.valid(t, "1"))
	again := mintedFor(t, client, url, secondToken, bot)
	assert.Equal(t, minted.Epoch+1, again.Epoch, "the new assignee mints at the new epoch")
	status = agentCall(t, client, http.MethodPost, claims, again.Token, execution)
	assert.Equal(t, http.StatusNoContent, status, "the new assignee's token claims")

	status, _ = tokenCall(t, client, http.MethodPost, url+"/agents/"+bot.String()+"/tokens",
		firstToken, operator.TokenContractVersion, `{}`)
	assert.Equal(t, http.StatusForbidden, status, "the operator the reassignment replaced mints nothing")

	// A reassignment to the same operator moves the epoch alone, and
	// that refuses the token minted at the earlier one.
	_, err = agent.NewPostgresStore(p).Assign(ctx, bot, second)
	require.NoError(t, err)
	status = agentCall(t, client, http.MethodPost, claims, again.Token, execution)
	assert.Equal(t, http.StatusUnauthorized, status, "a token minted at an earlier epoch is refused")
	assert.Equal(t, 1, *connections, "every request travelled over one connection")
}

// TestAgentToken_AnAgentHoldsItsNewestFiveTokens holds the bound on
// what minting writes (ADR-0100): seven mints leave five live tokens
// for the audience, the two oldest no longer authenticate, and the
// newest does.
func TestAgentToken_AnAgentHoldsItsNewestFiveTokens(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	one, key := keyed(t, operator.NewPostgresStore(p), "one")
	client, _ := tokenClient(t)
	operatorToken := exchangeKey(t, client, url, one, key, nil)
	f, _, execution := lineaged(t, p, one)
	bot := f.agent

	var tokens []string
	for range 7 {
		tokens = append(tokens, mintedFor(t, client, url, operatorToken, bot).Token)
	}

	assert.Equal(t, agent.LiveAccessTokensPerAudience, countRows(t, p,
		`SELECT count(*) FROM agent_tokens WHERE agent_grn = $1`, bot.String()))
	claims := url + "/agents/" + bot.String() + "/claims"
	status := agentCall(t, client, http.MethodPost, claims, tokens[0], execution)
	assert.Equal(t, http.StatusUnauthorized, status, "the oldest is past the bound")
	status = agentCall(t, client, http.MethodPost, claims, tokens[6], execution)
	assert.Equal(t, http.StatusNoContent, status, "the newest claims")
}

// TestAgentToken_AnAudienceTokenReachesNoGaramRoute is ADR-0100's
// audience rule on the agent's own routes: a token minted for a hosted
// operator the organization registered is refused where the token
// minted for garam claims, and an audience no record names is refused
// at the mint.
func TestAgentToken_AnAudienceTokenReachesNoGaramRoute(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	operators := operator.NewPostgresStore(p)
	controller, key := keyed(t, operators, "controller")
	hosted := register(t, operators, "control")
	client, _ := tokenClient(t)
	operatorToken := exchangeKey(t, client, url, controller, key, nil)
	f, _, execution := lineaged(t, p, controller)
	bot := f.agent
	mint := url + "/agents/" + bot.String() + "/tokens"
	claims := url + "/agents/" + bot.String() + "/claims"

	status, body := tokenCall(t, client, http.MethodPost, mint, operatorToken, operator.TokenContractVersion,
		`{"audience":"`+hosted.String()+`"}`)
	require.Equal(t, http.StatusCreated, status, body)
	var audienced machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(body), &audienced))
	status = agentCall(t, client, http.MethodPost, claims, audienced.Token, execution)
	assert.Equal(t, http.StatusUnauthorized, status, "a token minted for the control service claims nothing at garam")

	garam := mintedFor(t, client, url, operatorToken, bot)
	status = agentCall(t, client, http.MethodPost, claims, garam.Token, execution)
	assert.Equal(t, http.StatusNoContent, status, "control: the token minted for garam claims")

	status, _ = tokenCall(t, client, http.MethodPost, mint, operatorToken, operator.TokenContractVersion,
		`{"audience":"`+scopedGRN("acme", operator.ServiceType, "x1").String()+`"}`)
	assert.Equal(t, http.StatusBadRequest, status, "an audience the organization never registered is refused")
}

// TestAgentToken_AnAgentHoldsTenTokensAcrossItsAudiences holds the
// bound across audiences (ADR-0100): five mints for each of three
// registered audiences leave ten live tokens, so naming more audiences
// does not widen what one agent's tokens occupy.
func TestAgentToken_AnAgentHoldsTenTokensAcrossItsAudiences(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	operators := operator.NewPostgresStore(p)
	controller, key := keyed(t, operators, "controller")
	first := register(t, operators, "first")
	second := register(t, operators, "second")
	client, _ := tokenClient(t)
	operatorToken := exchangeKey(t, client, url, controller, key, nil)
	f, _, _ := lineaged(t, p, controller)
	bot := f.agent
	mint := url + "/agents/" + bot.String() + "/tokens"

	for _, body := range []string{`{}`, `{"audience":"` + first.String() + `"}`, `{"audience":"` + second.String() + `"}`} {
		for range agent.LiveAccessTokensPerAudience {
			status, answered := tokenCall(t, client, http.MethodPost, mint, operatorToken, operator.TokenContractVersion, body)
			require.Equal(t, http.StatusCreated, status, answered)
		}
	}

	assert.Equal(t, agent.LiveAccessTokensPerAgent, countRows(t, p,
		`SELECT count(*) FROM agent_tokens WHERE agent_grn = $1`, bot.String()))
}

// TestAgentToken_TheMintAnswersTheServerRootTheListenerWasGiven pins
// serverRootPem in the agent-token mint answer to the root `serve
// machine` was configured with, which it reads from
// GARAM_MACHINE_SERVER_ROOT_FILE (ADR-0040, ADR-0118): the answer is
// those bytes exactly, for a holder to pin garam by. The root here is
// not the certificate the listener serves, so an answer naming the
// serving certificate, or any root but the configured one, fails; the
// serving certificate differing from it is the control.
func TestAgentToken_TheMintAnswersTheServerRootTheListenerWasGiven(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	presented := loopbackCertificate(t)
	root := certificatePEM(loopbackCertificate(t))
	require.NotEqual(t, certificatePEM(presented), root, "the control: the configured root is not the serving certificate")
	srv, err := cli.NewMachineServer(p, cli.MachineConfig{
		Sealer: sealer, Issuer: testIssuer, Certificate: presented, ServerRoot: root,
		Delivery: briefDelivery(), Engine: engine, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		TokenURL: testTokenURL,
	})
	require.NoError(t, err)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	url := "https://" + lis.Addr().String()

	controller, key := keyed(t, operator.NewPostgresStore(p), "controller")
	client, _ := tokenClient(t)
	token := exchangeKey(t, client, url, controller, key, nil)
	f, _, _ := lineaged(t, p, controller)
	minted := mintedFor(t, client, url, token, f.agent)
	assert.Equal(t, string(root), minted.ServerRootPem, "the mint answers the configured server root, byte for byte")
}
