package machineapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/operator"
	operatormocks "github.com/garamsh/garam/internal/operator/mocks"
)

// authorityListener serves the authority routes over the machine
// listener to a client holding caller's certificate.
func authorityListener(t *testing.T, authority operator.AuthorityService, caller grn.GRN) (string, *http.Client) {
	t.Helper()
	return serveTLS(t, machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, authority, nil, nil, nil, nil, nil, nil), caller)
}

// versioned sends a request with the contract version header set to
// version, or without one where it is empty, and answers the status,
// the version the answer carries, and the body.
func versioned(t *testing.T, client *http.Client, method, url, version, body string) (int, string, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if version != "" {
		req.Header.Set(operator.ContractVersionHeader, version)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get(operator.ContractVersionHeader), string(got)
}

func configureBinding(audience grn.GRN) operator.Binding {
	return operator.Binding{
		Ref: "ref-1", Grant: "grant-1", Org: grn.New("root", "default", "org", "acme"),
		Actor: grn.New("root", "default", "user", "member"), Audience: audience,
		Operation: operator.OperationAgentConfigure, Target: agentGRN("acme", "0a1b2c3d4e5f6071"),
		Assignment: &operator.Assignment{Assignee: grn.New("acme", "default", "operator", "controller"), Epoch: "7"},
		RequestID:  "req-1", Digest: operator.BodyDigest(strings.Repeat("0", 64)),
	}
}

// TestIntrospectOperationAuthority_AnswersTheAudienceTheTokenNames
// holds that the caller the service is asked for is the one the
// handshake proved, never one the request names, and that the answer
// carries the binding with the token's expiry and the contract version.
func TestIntrospectOperationAuthority_AnswersTheAudienceTheTokenNames(t *testing.T) {
	hosted := grn.New("acme", "default", "operator", "hosted")
	expiresAt := time.Date(2026, time.October, 4, 12, 5, 0, 0, time.UTC)
	authority := operatormocks.NewMockAuthorityService(t)
	authority.EXPECT().Introspect(mock.Anything, hosted, "secret").
		Return(operator.Introspected{Binding: configureBinding(hosted), ExpiresAt: expiresAt}, nil).Once()
	url, client := authorityListener(t, authority, hosted)

	status, version, body := versioned(t, client, http.MethodPost, url+"/operation-authorities/introspection",
		operator.TokenAuthorityContractVersion, `{"authority":"secret"}`)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, operator.TokenAuthorityContractVersion, version)
	var got machinegen.OperationBinding
	require.NoError(t, json.Unmarshal([]byte(body), &got))
	require.NotNil(t, got.ExpiresAt)
	assert.True(t, expiresAt.Equal(*got.ExpiresAt))
	require.NotNil(t, got.Assignment)
	assert.Equal(t, machinegen.BoundAssignment{OperatorGrn: "grn:acme:default:operator:controller", Epoch: "7"}, *got.Assignment)
	assert.Equal(t, "ref-1", got.OperationRef)
}

// TestOperationAuthorityRoutes_RefuseAMissingOrUnsupportedVersion holds
// that both routes refuse a request carrying no version or another one,
// naming the version they serve and carrying it on the refusal. The
// service has no expectation, so reaching it fails the test; the test
// above is the control.
func TestOperationAuthorityRoutes_RefuseAMissingOrUnsupportedVersion(t *testing.T) {
	hosted := grn.New("acme", "default", "operator", "hosted")
	url, client := authorityListener(t, operatormocks.NewMockAuthorityService(t), hosted)
	for _, route := range []struct{ method, path, body string }{
		{http.MethodPost, "/operation-authorities/introspection", `{"authority":"secret"}`},
		{http.MethodGet, "/operation-references/ref-1", ""},
	} {
		for _, v := range []string{"", "operation-authority.v0"} {
			status, version, body := versioned(t, client, route.method, url+route.path, v, route.body)
			assert.Equal(t, http.StatusBadRequest, status, route.path)
			assert.Equal(t, operator.TokenAuthorityContractVersion, version, route.path)
			assert.Contains(t, body, operator.TokenAuthorityContractVersion, route.path)
		}
	}
}

// TestOperationAuthorityRoutes_MapRefusals holds the contract's
// statuses: one 404 for an authority the caller may not learn about,
// 403 for one current state no longer authorizes, and 503 naming no
// cause for one garam could not decide.
func TestOperationAuthorityRoutes_MapRefusals(t *testing.T) {
	hosted := grn.New("acme", "default", "operator", "hosted")
	cases := []struct {
		err    error
		status int
	}{
		{operator.ErrAuthorityNotFound, http.StatusNotFound},
		{operator.ErrAuthorityRevoked, http.StatusForbidden},
		{errorx.Wrap(errorx.New(errorx.KindInternal, "policy store"), "current authorization", errorx.KindUnavailable), http.StatusServiceUnavailable},
	}
	for _, c := range cases {
		authority := operatormocks.NewMockAuthorityService(t)
		authority.EXPECT().Reference(mock.Anything, hosted, operator.OperationRef("ref-1")).Return(operator.Binding{}, c.err).Once()
		url, client := authorityListener(t, authority, hosted)
		status, version, body := versioned(t, client, http.MethodGet, url+"/operation-references/ref-1", operator.TokenAuthorityContractVersion, "")
		assert.Equal(t, c.status, status, c.err.Error())
		assert.Equal(t, operator.TokenAuthorityContractVersion, version)
		if c.status == http.StatusServiceUnavailable {
			assert.Contains(t, body, `"message":"internal error"`)
		}
	}
}

// TestCheckOperationReference_AnswersNoExpiry holds that a reference
// check answers the binding without a token expiry: a reference is not
// a token and does not run out with one.
func TestCheckOperationReference_AnswersNoExpiry(t *testing.T) {
	hosted := grn.New("acme", "default", "operator", "hosted")
	authority := operatormocks.NewMockAuthorityService(t)
	authority.EXPECT().Reference(mock.Anything, hosted, operator.OperationRef("ref-1")).Return(configureBinding(hosted), nil).Once()
	url, client := authorityListener(t, authority, hosted)

	status, _, body := versioned(t, client, http.MethodGet, url+"/operation-references/ref-1", operator.TokenAuthorityContractVersion, "")
	require.Equal(t, http.StatusOK, status, body)
	assert.NotContains(t, body, "expiresAt")
	assert.Contains(t, body, `"operationRef":"ref-1"`)
}
