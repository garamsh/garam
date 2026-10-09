package machineapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
)

// exchange posts body to the exchange route presenting header as the
// Authorization and version as the contract, and answers the status,
// the contract the answer named and the body.
func exchange(t *testing.T, client *http.Client, url, header, version, body string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/tokens", strings.NewReader(body))
	require.NoError(t, err)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	if version != "" {
		req.Header.Set("Garam-Contract-Version", version)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	answered, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get("Garam-Contract-Version"), string(answered)
}

// TestServer_ExchangesACredentialForAToken is the exchange seen from
// the wire: a caller with no certificate presents the credential and
// the contract, and is answered the token the domain minted with the
// contract named on the answer. The refusals beside it each lack one
// thing the accepted call has, so what refuses each is that thing.
func TestServer_ExchangesACredentialForAToken(t *testing.T) {
	subject := operatorGRN("acme")
	operators := &operatorService{certificate: &operator.Certificate{Subject: subject}, credential: "garam_oc_held"}
	url, _ := listener(t, &agentService{}, operators, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, subject)
	client := anonymous(t)

	status, version, body := exchange(t, client, url, "Garam-Credential garam_oc_held", operator.TokenContractVersion, `{}`)
	require.Equal(t, http.StatusCreated, status, body)
	assert.Equal(t, operator.TokenContractVersion, version)
	var token machinegen.MachineToken
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	assert.Equal(t, operator.TokenPrefix+"answered", token.Token)
	assert.Equal(t, subject.String(), token.Grn)
	assert.Nil(t, token.Audience)

	hosted := grn.New("acme", grn.DefaultProject, operator.ServiceType, "control")
	status, _, body = exchange(t, client, url, "Garam-Credential garam_oc_held", operator.TokenContractVersion,
		`{"audience":"`+hosted.String()+`"}`)
	require.Equal(t, http.StatusCreated, status, body)
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	require.NotNil(t, token.Audience)
	assert.Equal(t, hosted.String(), *token.Audience)
	require.Len(t, operators.audiences, 2)
	assert.Equal(t, &hosted, operators.audiences[1], "the audience reaches the domain parsed")

	status, version, _ = exchange(t, client, url, "Garam-Credential garam_oc_held", "", `{}`)
	assert.Equal(t, http.StatusBadRequest, status, "a missing contract value is refused")
	assert.Equal(t, operator.TokenContractVersion, version, "and the refusal names the one this route serves")

	status, version, _ = exchange(t, client, url, "Garam-Credential garam_oc_held", "machine-token.v0", `{}`)
	assert.Equal(t, http.StatusBadRequest, status, "another contract value is refused")
	assert.Equal(t, operator.TokenContractVersion, version, "and the refusal names the one served")

	status, _, _ = exchange(t, client, url, "Garam-Credential garam_oc_other", operator.TokenContractVersion, `{}`)
	assert.Equal(t, http.StatusUnauthorized, status, "a credential no operator holds is refused")

	status, _, body = exchange(t, client, url, "Garam-Token garam_ot_x", operator.TokenContractVersion, `{}`)
	assert.Equal(t, http.StatusUnauthorized, status, "a token mints no token")
	assert.Contains(t, body, "Garam-Credential")

	status, _, _ = exchange(t, client, url, "Garam-Credential garam_oc_held", operator.TokenContractVersion,
		`{"audience":"not a grn"}`)
	assert.Equal(t, http.StatusBadRequest, status, "an audience that is no GRN is refused")
}

// TestConfineTokens_ATokenReachesTheRoutesThatListIt holds that a
// token reaches the operations api/machine.yaml lists machineToken on
// and no other. The certificate is the control: the same route refuses
// the token and admits the certificate, so what refuses the token is
// the confinement and not the route.
func TestConfineTokens_ATokenReachesTheRoutesThatListIt(t *testing.T) {
	subject := operatorGRN("acme")
	operators := &operatorService{certificate: &operator.Certificate{Subject: subject}}
	srv := machineapi.NewServer(&agentService{}, operators, &messageService{}, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, stubKeyring{})
	url, certified := serveTLSTokens(t, srv, subject, presentedTokens{"garam_ot_live": subject},
		machineapi.ConfineTokens)
	client := anonymous(t)

	request := func(c *http.Client, method, path, token string) int {
		req, err := http.NewRequest(method, url+path, nil)
		require.NoError(t, err)
		if token != "" {
			req.Header.Set("Authorization", "Garam-Token "+token)
		}
		resp, err := c.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	assert.Equal(t, http.StatusOK, request(client, http.MethodGet, "/identity", "garam_ot_live"),
		"a route listing machineToken admits the token")
	assert.Equal(t, http.StatusUnauthorized, request(client, http.MethodGet, "/identity", "garam_ot_dead"),
		"a token no row stands for is refused")
	assert.Equal(t, http.StatusUnauthorized, request(client, http.MethodPost, "/identity/certificate", "garam_ot_live"),
		"a route a certificate alone reaches refuses the token")
	assert.NotEqual(t, http.StatusUnauthorized, request(certified, http.MethodPost, "/identity/certificate", ""),
		"the same route admits the certificate")
}

// TestServer_ARouteReadingTheCertificateRefusesAToken holds the refusal
// that does not rest on ConfineTokens: with no confinement in front of
// it, a token's identity still reaches no route that reads the
// certificate a connection presented (ADR-0100). The renewal is that
// route, and the certificate reaching it is the control: the same
// route renews for a certificate, so what refuses the token is the
// missing certificate and not the route refusing everyone.
func TestServer_ARouteReadingTheCertificateRefusesAToken(t *testing.T) {
	subject := operatorGRN("acme")
	operators := &operatorService{certificate: &operator.Certificate{Subject: subject}}
	srv := machineapi.NewServer(&agentService{}, operators, &messageService{}, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, stubKeyring{})
	url, certified := serveTLSTokens(t, srv, subject, presentedTokens{"garam_ot_live": subject})

	req, err := http.NewRequest(http.MethodPost, url+"/identity/certificate", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Garam-Token garam_ot_live")
	resp, err := anonymous(t).Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Contains(t, string(body), "a token carries none")
	assert.Empty(t, operators.renewed, "nothing reached the renewal")

	status, _ := do(t, certified, http.MethodPost, url+"/identity/certificate")
	assert.Equal(t, http.StatusCreated, status, "the certificate renews")
	assert.Len(t, operators.renewed, 1)
}

// TestServer_InitialIssuanceRefusesATokenItself holds that initial
// issuance, which goes with the certificate path (ADR-0100), refuses a
// token without ConfineTokens in front of it, now that the lifecycle
// routes beside it admit one. The certificate is the control: the same
// request with it is answered past that refusal, by the contract check
// naming the certificate's value.
func TestServer_InitialIssuanceRefusesATokenItself(t *testing.T) {
	subject := operatorGRN("acme")
	srv := machineapi.NewServer(&agentService{}, &operatorService{}, &messageService{}, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, stubKeyring{})
	url, certified := serveTLSTokens(t, srv, subject, presentedTokens{"garam_ot_live": subject})
	path := url + "/agents/" + agentGRN("acme", "bot").String() + "/initial-certificate"

	send := func(client *http.Client, token string) (int, string) {
		req, err := http.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		require.NoError(t, err)
		req.Header.Set("Garam-Contract-Version", agent.TokenEnrollmentContractVersion)
		if token != "" {
			req.Header.Set("Authorization", "Garam-Token "+token)
		}
		resp, err := client.Do(req)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		return resp.StatusCode, string(body)
	}
	status, body := send(anonymous(t), "garam_ot_live")
	assert.Equal(t, http.StatusUnauthorized, status, body)
	assert.Contains(t, body, "initial issuance takes no token")

	status, body = send(certified, "")
	assert.Equal(t, http.StatusBadRequest, status, body)
	assert.Contains(t, body, agent.EnrollmentContractVersion, "control: a certificate reaches the contract check")
}

// TestTokenRoutes_AreTheOperationsTheSpecificationListsMachineTokenOn
// holds the hand-kept route set against the file that states it: every
// operation api/machine.yaml lists machineToken on is in the set, and
// nothing else is. The count of operations read is asserted too, so a
// file that failed to parse into paths could not pass as one listing
// none.
func TestTokenRoutes_AreTheOperationsTheSpecificationListsMachineTokenOn(t *testing.T) {
	raw, err := os.ReadFile("../../api/machine.yaml")
	require.NoError(t, err)
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string                `yaml:"operationId"`
			Security    []map[string][]string `yaml:"security"`
		} `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &spec))

	listed := map[string]bool{}
	operations := 0
	for path, methods := range spec.Paths {
		for method, operation := range methods {
			if operation.OperationID == "" {
				continue
			}
			operations++
			for _, requirement := range operation.Security {
				if _, ok := requirement["machineToken"]; ok {
					listed[strings.ToUpper(method)+" "+path] = true
				}
			}
		}
	}
	require.Equal(t, reflect.TypeOf((*machinegen.ServerInterface)(nil)).Elem().NumMethod(), operations,
		"every operation the generated interface declares was read")
	assert.Equal(t, listed, machineapi.TokenRoutes)
}

// tokenRequest sends method to path presenting token, with headers
// beside it, and answers the status, the contract the answer named and
// the body.
func tokenRequest(t *testing.T, client *http.Client, method, url, token string, headers map[string]string, body string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Garam-Token "+token)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	answered, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get("Garam-Contract-Version"), string(answered)
}

// TestServer_MintsAnAgentTokenForTheCallingOperator is the mint seen
// from the wire: the operator's token reaches the route, what it passes
// on is the caller, the agent and the audience, and the answer carries
// the token, its epoch and the server root. A request missing its
// contract value is refused naming machine-token.v1, and one naming
// another is refused too, so what admitted the first is the value.
func TestServer_MintsAnAgentTokenForTheCallingOperator(t *testing.T) {
	controller := operatorGRN("acme")
	bot := agentGRN("acme", "0a1b2c3d4e5f6071")
	agents := &agentService{}
	srv := machineapi.NewServer(agents, &operatorService{}, &messageService{}, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, stubKeyring{})
	url, _ := serveTLSTokens(t, srv, controller, presentedTokens{"garam_ot_live": controller},
		machineapi.ConfineTokens)
	client := anonymous(t)
	path := url + "/agents/" + bot.String() + "/tokens"
	contract := map[string]string{"Garam-Contract-Version": operator.TokenContractVersion}

	hosted := grn.New("acme", grn.DefaultProject, operator.ServiceType, "control")
	status, version, body := tokenRequest(t, client, http.MethodPost, path, "garam_ot_live", contract,
		`{"audience":"`+hosted.String()+`"}`)
	require.Equal(t, http.StatusCreated, status, body)
	assert.Equal(t, operator.TokenContractVersion, version)
	var token machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	assert.True(t, strings.HasPrefix(token.Token, agent.AccessTokenPrefix))
	assert.Equal(t, bot.String(), token.Grn)
	assert.Equal(t, 3, token.Epoch)
	assert.Equal(t, "server root", token.ServerRootPem)
	require.NotNil(t, token.Audience)
	assert.Equal(t, hosted.String(), *token.Audience)
	require.Len(t, agents.calls, 1)
	assert.Equal(t, call{operator: controller, subject: bot}, agents.calls[0])
	assert.Equal(t, &hosted, agents.audiences[0])

	status, version, _ = tokenRequest(t, client, http.MethodPost, path, "garam_ot_live", nil, `{}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, operator.TokenContractVersion, version, "a missing value is refused naming the one served")
	status, _, _ = tokenRequest(t, client, http.MethodPost, path, "garam_ot_live",
		map[string]string{"Garam-Contract-Version": "machine-token.v0"}, `{}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Len(t, agents.calls, 1, "neither refusal reached the domain")
}

// TestServer_AnAgentTokenCarriesItsExecutionTokenInAHeaderOfItsOwn
// holds the fence an agent token presents (ADR-0100): the token's
// serial and dates stand in for a certificate's, and the execution
// token travels in Garam-Execution because the agent token takes
// Authorization. An operator's token on the same route is refused, as
// it carries no serial for the fence to read — so what admitted the
// agent's is its credential and not the route admitting every token.
func TestServer_AnAgentTokenCarriesItsExecutionTokenInAHeaderOfItsOwn(t *testing.T) {
	bot := agentGRN("acme", "0a1b2c3d4e5f6071")
	controller := operatorGRN("acme")
	messages := &messageService{}
	srv := machineapi.NewServer(&agentService{}, &operatorService{}, messages, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, stubKeyring{})
	url, _ := serveTLSTokens(t, srv, bot, presentedTokens{"garam_at_live": bot, "garam_ot_live": controller},
		machineapi.ConfineTokens)
	client := anonymous(t)
	execution, err := agent.NewToken()
	require.NoError(t, err)
	claims := url + "/agents/" + bot.String() + "/claims"

	status, _, body := tokenRequest(t, client, http.MethodPost, claims, "garam_at_live",
		map[string]string{"Garam-Execution": execution.String()}, "")
	require.Equal(t, http.StatusNoContent, status, body)
	require.Len(t, messages.calls, 1)
	require.Len(t, messages.fences, 1)
	fence := messages.fences[0]
	require.NotNil(t, fence)
	assert.Equal(t, "t"+strings.Repeat("0", 32), fence.Serial, "the token's serial stands in for a certificate's")
	hash := execution.Hash()
	assert.Equal(t, hash[:], fence.TokenHash, "the execution token is read from its own header")

	status, _, body = tokenRequest(t, client, http.MethodPost, claims, "garam_ot_live", nil, "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, "carries none")
	assert.Len(t, messages.calls, 1, "the operator's token reached no claim")
}

// TestServer_AnAgentTokenRenewsNoCertificate holds that an agent's
// token reaches its own certificate route and renews nothing there:
// its assignee mints it another token instead (ADR-0100).
func TestServer_AnAgentTokenRenewsNoCertificate(t *testing.T) {
	bot := agentGRN("acme", "0a1b2c3d4e5f6071")
	srv := machineapi.NewServer(&agentService{}, &operatorService{}, &messageService{}, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, stubKeyring{})
	url, _ := serveTLSTokens(t, srv, bot, presentedTokens{"garam_at_live": bot}, machineapi.ConfineTokens)

	status, _, body := tokenRequest(t, anonymous(t), http.MethodPost, url+"/agents/"+bot.String()+"/certificate",
		"garam_at_live", nil, "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, "no certificate was presented")
}

// TestServer_ADurableSubmissionTakesTheValueItsCredentialTakes holds
// durable-delivery.v2 (ADR-0100): a request an agent token
// authenticated is served under v2 and refused under v1, naming v2.
func TestServer_ADurableSubmissionTakesTheValueItsCredentialTakes(t *testing.T) {
	bot := agentGRN("acme", "0a1b2c3d4e5f6071")
	srv := machineapi.NewServer(&agentService{}, &operatorService{}, &messageService{}, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, stubKeyring{})
	url, certified := serveTLSTokens(t, srv, bot, presentedTokens{"garam_at_live": bot}, machineapi.ConfineTokens)
	client := anonymous(t)

	status, version, body := tokenRequest(t, client, http.MethodPost, url+"/outbox-messages", "garam_at_live",
		map[string]string{"Garam-Contract-Version": message.DeliveryContractVersion}, `{}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, message.TokenDeliveryContractVersion, version)
	assert.Contains(t, body, message.TokenDeliveryContractVersion)

	// The empty body is refused after the contract check, so the
	// refusal names no contract value.
	_, version, body = tokenRequest(t, client, http.MethodPost, url+"/outbox-messages", "garam_at_live",
		map[string]string{"Garam-Contract-Version": message.TokenDeliveryContractVersion}, `{}`)
	assert.NotContains(t, body, "Garam-Contract-Version", "v2 is past the contract check")
	assert.Equal(t, message.TokenDeliveryContractVersion, version)

	status, version, _ = tokenRequest(t, certified, http.MethodPost, url+"/outbox-messages", "",
		map[string]string{"Garam-Contract-Version": message.TokenDeliveryContractVersion}, `{}`)
	assert.Equal(t, http.StatusBadRequest, status, "a certificate is served under v1 and refused under v2")
	assert.Equal(t, message.DeliveryContractVersion, version)
}
