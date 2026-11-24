package machineapi_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	connectiongen "github.com/garamsh/garam/platform/api/cluster-connection/v1alpha1"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/operator"
)

const connectionTokenURL = "https://machine.garam.test/oauth/token"

var connectedGRN = grn.New("acme", cluster.ServiceType, "5f60710a1b2c3d4e")

// connectionClusters is cluster.Service answering err, or what it was
// asked, and recording the arguments.
type connectionClusters struct {
	cluster.Service
	err    error
	caller grn.GRN
	jwk    []byte
	token  string
}

func (c *connectionClusters) Register(_ context.Context, _, token string, jwk []byte) (grn.GRN, error) {
	c.token, c.jwk = token, jwk
	return connectedGRN, c.err
}

func (c *connectionClusters) ExchangeAssertion(_ context.Context, _ string, client grn.GRN, _ string) (*cluster.Token, error) {
	c.caller = client
	if c.err != nil {
		return nil, c.err
	}
	return &cluster.Token{Value: cluster.TokenPrefix + "answered", Cluster: client, ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (c *connectionClusters) Rotate(_ context.Context, caller grn.GRN, jwk []byte, _, _ string) (cluster.Rotation, error) {
	c.caller, c.jwk = caller, jwk
	return cluster.Rotation{KID: "successor", PredecessorRetiresAt: time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)}, c.err
}

// serveConnection serves cluster-connection behind the machine
// listener, which resolves "cluster" to the connected cluster and
// "operator" to an operator.
func serveConnection(t *testing.T, clusters cluster.Service) (string, *http.Client) {
	t.Helper()
	served := machine.NewServer(presentedTokens{
		"cluster":  connectedGRN,
		"operator": grn.New("acme", operator.ServiceType, "one"),
	}, nil, loopback(t), connectiongen.HandlerWithOptions(machineapi.NewClusterConnection(clusters, connectionTokenURL),
		connectiongen.StdHTTPServerOptions{ErrorHandlerFunc: machineapi.ConnectionBindingError}))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = served.ServeTLS(lis, "", "") }()
	t.Cleanup(func() { _ = served.Close() })
	transport := &http.Transport{TLSClientConfig: &tls.Config{
		//nolint:gosec // the listener's own certificate is not under test.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}}
	t.Cleanup(transport.CloseIdleConnections)
	return "https://" + lis.Addr().String() + "/cluster-connection/v1alpha1", &http.Client{Transport: transport}
}

// connect sends body to path, presenting token where one is named, and
// answers the status, the decoded body and the response.
func connect(t *testing.T, client *http.Client, method, url, token, body string) (int, map[string]any, *http.Response) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", machine.TokenScheme+" "+token)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded), string(raw))
	return resp.StatusCode, decoded, resp
}

const registration = `{"registrationToken":"garam_crt_x","jwk":{"kty":"RSA","n":"n","e":"AQAB","d":"private"},"connector":{"version":"0.1.0"}}`

// TestClusterConnection_RegisterAnswersTheRecordAndHandsTheKeyOnWhole
// holds REG-6's answer, and that the key reaches the service as sent,
// private members included, for it to refuse (REG-2).
func TestClusterConnection_RegisterAnswersTheRecordAndHandsTheKeyOnWhole(t *testing.T) {
	clusters := &connectionClusters{}
	base, client := serveConnection(t, clusters)

	status, body, _ := connect(t, client, http.MethodPost, base+"/registrations", "", registration)
	assert.Equal(t, http.StatusCreated, status)
	assert.Equal(t, map[string]any{"cluster": connectedGRN.String(), "clientId": connectedGRN.String(),
		"tokenUrl": connectionTokenURL}, body)
	assert.Equal(t, "garam_crt_x", clusters.token)
	assert.JSONEq(t, `{"kty":"RSA","n":"n","e":"AQAB","d":"private"}`, string(clusters.jwk))
}

// TestClusterConnection_RefusalsCarryTheInterfacesCodes holds T-5: each
// refusal the interface names is answered with its code and status,
// and a failure of garam's own with neither its kind nor its text.
func TestClusterConnection_RefusalsCarryTheInterfacesCodes(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"a refused token", cluster.ErrRegistrationRefused, http.StatusUnauthorized, "registration_refused"},
		{"a refused key", cluster.ErrInvalidKey, http.StatusBadRequest, "invalid_request"},
		{"a refused proof", cluster.ErrInvalidProof, http.StatusBadRequest, "invalid_proof"},
		{"a key held before", cluster.ErrKeyHeld, http.StatusBadRequest, "invalid_request"},
		{"a third key", cluster.ErrTooManyKeys, http.StatusConflict, "too_many_keys"},
		{"a throttle", &operator.Exceeded{RetryAfter: 30 * time.Second}, http.StatusTooManyRequests, "throttled"},
		{"a missing record", errorx.New(errorx.KindNotFound, "cluster not found"), http.StatusNotFound, "not_found"},
		{"a rotation garam does not serve", cluster.ErrRotationUnavailable, http.StatusServiceUnavailable, "internal"},
		{"garam's own failure", errorx.Wrap(errors.New(`relation "cluster_keys" SQLSTATE 42P01`), "rotate",
			errorx.KindInternal), http.StatusInternalServerError, "internal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base, client := serveConnection(t, &connectionClusters{err: c.err})
			status, body, resp := connect(t, client, http.MethodPost, base+"/clusters/self/keys", "cluster",
				`{"jwk":{},"authorization":"a","proof":"p"}`)
			assert.Equal(t, c.status, status)
			assert.Equal(t, c.code, body["error"])
			assert.NotContains(t, body["message"], "SQLSTATE")
			if c.status == http.StatusTooManyRequests {
				assert.Equal(t, "30", resp.Header.Get("Retry-After"))
			}
		})
	}
}

// TestClusterConnection_RoutesButRegistrationAnswerAClustersTokenAlone
// holds AUTH-1 and AUTH-2 at the transport: no token, a token that
// proves nothing and another caller's token are each answered 401, and
// the cluster's own is the control.
func TestClusterConnection_RoutesButRegistrationAnswerAClustersTokenAlone(t *testing.T) {
	clusters := &connectionClusters{}
	base, client := serveConnection(t, clusters)
	rotation := `{"jwk":{"kty":"RSA"},"authorization":"a","proof":"p"}`

	for name, token := range map[string]string{"no token": "", "a voided token": "voided", "an operator's token": "operator"} {
		for _, route := range []struct{ method, path, body string }{
			{http.MethodPost, "/clusters/self/keys", rotation},
			{http.MethodGet, "/clusters/self/declarations", ""},
			{http.MethodPost, "/clusters/self/reports", "{}"},
		} {
			status, body, _ := connect(t, client, route.method, base+route.path, token, route.body)
			assert.Equal(t, http.StatusUnauthorized, status, name+" "+route.path)
			assert.Equal(t, "unauthorized", body["error"], name+" "+route.path)
		}
	}
	assert.Nil(t, clusters.jwk, "no refused request reached the service")

	status, body, _ := connect(t, client, http.MethodPost, base+"/clusters/self/keys", "cluster", rotation)
	assert.Equal(t, http.StatusCreated, status)
	assert.Equal(t, map[string]any{"kid": "successor", "predecessorRetiresAt": "2026-11-25T00:00:00Z"}, body)
	assert.Equal(t, connectedGRN, clusters.caller)
}

// TestClusterConnection_UnbuiltRoutesAnswerAFailureTheConnectorBacksOff
// holds that a route garam does not serve yet is a 5xx in the
// interface's body, which T-6 has the connector back off from.
func TestClusterConnection_UnbuiltRoutesAnswerAFailureTheConnectorBacksOff(t *testing.T) {
	base, client := serveConnection(t, &connectionClusters{})
	status, body, _ := connect(t, client, http.MethodGet, base+"/clusters/self/declarations", "cluster", "")
	assert.Equal(t, http.StatusNotImplemented, status)
	assert.Equal(t, "internal", body["error"])

	status, body, _ = connect(t, client, http.MethodGet, base+"/clusters/self/declarations?wait=fifty", "cluster", "")
	assert.Equal(t, http.StatusBadRequest, status, "a parameter the router cannot bind")
	assert.Equal(t, "invalid_request", body["error"])
}

// TestExchangeClientAssertion_SendsAClustersAssertionToItsRecord holds
// that /oauth/token picks the cluster's profile by client_id's GRN type
// (ADR-0186 decision 4): a cluster's assertion reaches the cluster
// service and no operator's, and an audience refuses it. An operator's
// assertion, the control, reaches the operator service.
func TestExchangeClientAssertion_SendsAClustersAssertionToItsRecord(t *testing.T) {
	operators, clusters := &assertionService{}, &connectionClusters{}
	base, _ := serveTLS(t, machineapi.NewServer(&agentService{}, operators, &messageService{}, unwatched(t),
		nil, nil, nil, nil, nil, nil, stubKeyring{}).WithClusters(clusters), connectedGRN)
	form := validForm()
	form.Set("client_id", connectedGRN.String())

	resp, body := exchangeForm(t, anonymous(t), base, form.Encode(), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Contains(t, body, cluster.TokenPrefix+"answered")
	assert.Equal(t, connectedGRN, clusters.caller)
	assert.Empty(t, operators.calls, "no operator's exchange ran")

	form.Set("audience", "grn:acme/iam/operator/control")
	resp, body = exchangeForm(t, anonymous(t), base, form.Encode(), nil)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode, body)
	assert.Equal(t, "invalid_request", oauthError(t, body))

	resp, body = exchangeForm(t, anonymous(t), base, validForm().Encode(), nil)
	require.Equal(t, http.StatusOK, resp.StatusCode, body)
	assert.Len(t, operators.calls, 1, "the control: an operator's assertion reaches the operator service")
}
