package machineapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/operator"
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

// supervisorService is an agent.SupervisorService answering a token
// for the one credential it was given. Approving, enrolling and
// revoking reach it through the embedded interface and panic: no test
// here drives them.
type supervisorService struct {
	agent.SupervisorService
	supervisor grn.GRN
	credential string
}

func (s *supervisorService) Exchange(_ context.Context, credential string) (*agent.SupervisorToken, error) {
	if credential != s.credential {
		return nil, agent.ErrSupervisorCredentialInvalid
	}
	return &agent.SupervisorToken{Value: "garam_st_answered", Supervisor: s.supervisor,
		ExpiresAt: time.Date(2026, time.October, 9, 13, 0, 0, 0, time.UTC)}, nil
}

// TestServer_ExchangesASupervisorsCredentialForAToken is the exchange
// seen from the wire: a caller with no token presents its
// credential and the contract, and is answered the token the domain
// minted with the contract named on the answer. The refusals beside it
// each lack one thing the accepted call has, so what refuses each is
// that thing. An operator's retired shared-secret credential is one a
// supervisor does not hold, and is refused as any other (ADR-0117).
func TestServer_ExchangesASupervisorsCredentialForAToken(t *testing.T) {
	subject := operatorGRN("acme")
	supervisor := grn.New("acme", grn.DefaultProject, agent.ServiceTypeSupervisor, "host-1")
	supervisors := &supervisorService{supervisor: supervisor, credential: "garam_sc_held"}
	url, _ := serveTLS(t, machineapi.NewServer(&agentService{}, nil, &messageService{}, consenting(),
		unwatched(t), &memberGroups{}, &engine{}, nil, nil, nil, nil, supervisors, nil, stubKeyring{}), subject)
	client := anonymous(t)

	status, version, body := exchange(t, client, url, "Garam-Credential garam_sc_held", operator.TokenContractVersion, `{}`)
	require.Equal(t, http.StatusCreated, status, body)
	assert.Equal(t, operator.TokenContractVersion, version)
	var token machinegen.MachineToken
	require.NoError(t, json.Unmarshal([]byte(body), &token))
	assert.Equal(t, "garam_st_answered", token.Token)
	assert.Equal(t, supervisor.String(), token.Grn)
	assert.Nil(t, token.Audience)

	status, _, _ = exchange(t, client, url, "Garam-Credential garam_sc_held", operator.TokenContractVersion,
		`{"audience":"grn:acme:default:operator:control"}`)
	assert.Equal(t, http.StatusBadRequest, status, "a supervisor's token names no audience")

	status, version, _ = exchange(t, client, url, "Garam-Credential garam_sc_held", "", `{}`)
	assert.Equal(t, http.StatusBadRequest, status, "a missing contract value is refused")
	assert.Equal(t, operator.TokenContractVersion, version, "and the refusal names the one this route serves")

	status, version, _ = exchange(t, client, url, "Garam-Credential garam_sc_held", "machine-token.v0", `{}`)
	assert.Equal(t, http.StatusBadRequest, status, "another contract value is refused")
	assert.Equal(t, operator.TokenContractVersion, version, "and the refusal names the one served")

	status, _, _ = exchange(t, client, url, "Garam-Credential garam_sc_other", operator.TokenContractVersion, `{}`)
	assert.Equal(t, http.StatusUnauthorized, status, "a credential no supervisor holds is refused")

	status, _, _ = exchange(t, client, url, "Garam-Credential garam_oc_"+strings.Repeat("0", 64),
		operator.TokenContractVersion, `{}`)
	assert.Equal(t, http.StatusUnauthorized, status, "an operator's retired credential is refused")

	status, _, body = exchange(t, client, url, "Garam-Token garam_ot_x", operator.TokenContractVersion, `{}`)
	assert.Equal(t, http.StatusUnauthorized, status, "a token mints no token")
	assert.Contains(t, body, "Garam-Credential")
}

// TestSpecification_EveryOperationButTheExemptOnesTakesAToken holds the
// specification to what the listener does (ADR-0118): every operation
// lists machineToken but the four that authenticate by what else a
// request carries, and the Bearer alias is listed exactly where
// machineToken is (ADR-0102). The count of operations read is asserted
// too, so a file that failed to parse into paths could not pass as one
// listing none.
func TestSpecification_EveryOperationButTheExemptOnesTakesAToken(t *testing.T) {
	raw, err := os.ReadFile("../../api/machine.yaml")
	require.NoError(t, err)
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string                `yaml:"operationId"`
			Security    []map[string][]string `yaml:"security"`
		} `yaml:"paths"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &spec))

	exempt := map[string]bool{standaloneEnrollmentRoute: true, tokenExchangeRoute: true,
		assertionExchangeRoute: true, keySetRoute: true}
	listed, bearer, every := map[string]bool{}, map[string]bool{}, map[string]bool{}
	operations := 0
	for path, methods := range spec.Paths {
		for method, operation := range methods {
			if operation.OperationID == "" {
				continue
			}
			operations++
			route := strings.ToUpper(method) + " " + path
			if !exempt[route] {
				every[route] = true
			}
			for _, requirement := range operation.Security {
				if _, ok := requirement["machineToken"]; ok {
					listed[route] = true
				}
				if _, ok := requirement["machineBearer"]; ok {
					bearer[route] = true
				}
			}
		}
	}
	require.Equal(t, reflect.TypeOf((*machinegen.ServerInterface)(nil)).Elem().NumMethod(), operations,
		"every operation the generated interface declares was read")
	assert.Equal(t, every, listed, "every operation but the exempt ones takes a token")
	assert.Equal(t, listed, bearer, "the Bearer alias is listed exactly where machineToken is (ADR-0102)")
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
	srv := machineapi.NewServer(agents, nil, &messageService{}, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, nil, stubKeyring{})
	url, _ := serveTLSTokens(t, srv, controller, presentedTokens{"garam_ot_live": controller})
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
// serial and dates are what the fence reads, and the execution token
// travels in Garam-Execution because the agent token takes
// Authorization. An operator's token on the same route is refused, as
// it carries no serial for the fence to read — so what admitted the
// agent's is its credential and not the route admitting every token.
func TestServer_AnAgentTokenCarriesItsExecutionTokenInAHeaderOfItsOwn(t *testing.T) {
	bot := agentGRN("acme", "0a1b2c3d4e5f6071")
	controller := operatorGRN("acme")
	messages := &messageService{}
	srv := machineapi.NewServer(&agentService{}, nil, messages, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, nil, stubKeyring{})
	url, _ := serveTLSTokens(t, srv, bot, presentedTokens{"garam_at_live": bot, "garam_ot_live": controller})
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
	assert.Equal(t, "t"+strings.Repeat("0", 32), fence.Serial, "the fence reads the token's serial")
	hash := execution.Hash()
	assert.Equal(t, hash[:], fence.TokenHash, "the execution token is read from its own header")

	status, _, body = tokenRequest(t, client, http.MethodPost, claims, "garam_ot_live", nil, "")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, "carries none")
	assert.Len(t, messages.calls, 1, "the operator's token reached no claim")
}

// TestServer_ADurableSubmissionIsServedUnderV2Alone holds
// durable-delivery.v2 (ADR-0100, ADR-0118): a durable submission is
// served under v2 and refused under the retired v1, naming v2.
func TestServer_ADurableSubmissionIsServedUnderV2Alone(t *testing.T) {
	bot := agentGRN("acme", "0a1b2c3d4e5f6071")
	srv := machineapi.NewServer(&agentService{}, nil, &messageService{}, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, nil, stubKeyring{})
	url, _ := serveTLSTokens(t, srv, bot, presentedTokens{"garam_at_live": bot})
	client := anonymous(t)

	status, version, body := tokenRequest(t, client, http.MethodPost, url+"/outbox-messages", "garam_at_live",
		map[string]string{"Garam-Contract-Version": "durable-delivery.v1"}, `{}`)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, message.TokenDeliveryContractVersion, version)
	assert.Contains(t, body, message.TokenDeliveryContractVersion)

	// The empty body is refused after the contract check, so the
	// refusal names no contract value.
	_, version, body = tokenRequest(t, client, http.MethodPost, url+"/outbox-messages", "garam_at_live",
		map[string]string{"Garam-Contract-Version": message.TokenDeliveryContractVersion}, `{}`)
	assert.NotContains(t, body, "Garam-Contract-Version", "v2 is past the contract check")
	assert.Equal(t, message.TokenDeliveryContractVersion, version)
}

// TestServer_ABearerTokenTakesWhatAGaramTokenTakes holds the alias at
// the routes (ADR-0102): the same agent token under Bearer is served
// under the v2 contract value and refused under v1 naming v2, and
// carries its execution token in Garam-Execution. Each assertion is
// made beside the same request under Garam-Token, which is what the
// alias must equal.
func TestServer_ABearerTokenTakesWhatAGaramTokenTakes(t *testing.T) {
	bot := agentGRN("acme", "0a1b2c3d4e5f6071")
	messages := &messageService{}
	srv := machineapi.NewServer(&agentService{}, nil, messages, consenting(), unwatched(t),
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil, nil, stubKeyring{})
	url, _ := serveTLSTokens(t, srv, bot, presentedTokens{"garam_at_live": bot})
	client := anonymous(t)
	execution, err := agent.NewToken()
	require.NoError(t, err)

	for _, scheme := range []string{"Garam-Token", "Bearer"} {
		authorization := map[string]string{"Authorization": scheme + " garam_at_live"}
		with := func(extra map[string]string) map[string]string {
			headers := map[string]string{"Authorization": authorization["Authorization"]}
			for k, v := range extra {
				headers[k] = v
			}
			return headers
		}

		status, version, body := tokenRequest(t, client, http.MethodPost, url+"/outbox-messages", "",
			with(map[string]string{"Garam-Contract-Version": "durable-delivery.v1"}), `{}`)
		assert.Equal(t, http.StatusBadRequest, status, scheme)
		assert.Equal(t, message.TokenDeliveryContractVersion, version, scheme)
		assert.Contains(t, body, message.TokenDeliveryContractVersion, scheme)

		before := len(messages.fences)
		status, _, body = tokenRequest(t, client, http.MethodPost, url+"/agents/"+bot.String()+"/claims", "",
			with(map[string]string{"Garam-Execution": execution.String()}), "")
		require.Equal(t, http.StatusNoContent, status, "%s: %s", scheme, body)
		require.Len(t, messages.fences, before+1, scheme)
		hash := execution.Hash()
		assert.Equal(t, hash[:], messages.fences[before].TokenHash, "%s: the execution token is read from its own header", scheme)
	}
}
