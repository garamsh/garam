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
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machineapi"
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
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil)
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
		&memberGroups{}, &engine{}, nil, nil, nil, nil, nil)
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
