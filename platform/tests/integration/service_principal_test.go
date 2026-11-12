package integration_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/operator"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
)

// machineRefusal reads body as the machine listener's OAuth error.
func machineRefusal(t *testing.T, body string) machinegen.OAuthErrorError {
	t.Helper()
	var refusal machinegen.OAuthError
	require.NoError(t, json.Unmarshal([]byte(body), &refusal), body)
	return refusal.Error
}

// TestServicePrincipal_ExchangeAnswersTheIssuersTokenForPolicy runs the
// exchange over Postgres for the principal of cowork in an
// organization: the token verifies against the issuer's key and names
// the principal, the policy service and at most 300 seconds. It logs
// the exchange's response exactly as served, which is what the cowork
// part of #1758 reads its token from. The key is generated here.
//
//	go test ./tests/integration -v \
//		-run TestServicePrincipal_ExchangeAnswersTheIssuersTokenForPolicy
func TestServicePrincipal_ExchangeAnswersTheIssuersTokenForPolicy(t *testing.T) {
	s := newIssuerStack(t)

	form := url.Values{
		"grant_type": {operator.GrantTypeClientCredentials}, "client_id": {s.coworkID.String()},
		"client_assertion":      {principalAssertion(t, s.cowork, s.coworkID, s.machine+"/oauth/token", "")},
		"client_assertion_type": {operator.ClientAssertionTypeJWT},
		"audience":              {identity.AudiencePolicy},
	}
	req, err := http.NewRequest(http.MethodPost, s.machine+"/oauth/token", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, dump := serve(t, req)
	t.Log("POST /oauth/token (client_id " + s.coworkID.String() + ", audience policy)\n" + dump)
	require.Equal(t, http.StatusOK, resp.StatusCode, dump)

	var granted machinegen.OAuthToken
	require.NoError(t, json.Unmarshal([]byte(bodyOf(dump)), &granted))
	claims, err := s.key.Verify(granted.AccessToken, time.Now())
	require.NoError(t, err)
	assert.Equal(t, issuerURL, claims.Issuer)
	assert.Equal(t, "grn:globex/iam/service/cowork", claims.Subject)
	assert.Equal(t, identity.AudiencePolicy, claims.Audience)
	assert.LessOrEqual(t, claims.Lifetime, 300*time.Second)
	assert.Empty(t, claims.SessionID, "no session stands behind a principal's token")
	assert.Equal(t, 300, granted.ExpiresIn)
}

// TestServicePrincipal_ExchangeRefusesWhatIsNotTheServicesLivePrincipal
// holds the refusals the exchange makes over Postgres: an audience
// other than policy, a key of another service, a replay on the same
// listener and on a second one, a principal whose organization is
// deleted, and the issuer off.
func TestServicePrincipal_ExchangeRefusesWhatIsNotTheServicesLivePrincipal(t *testing.T) {
	s := newIssuerStack(t)
	ask := func(listener string, jti, audience string) (int, string) {
		return s.exchangeAs(t, listener, s.cowork, s.coworkID, jti, audience)
	}

	for name, audience := range map[string]string{"another audience": identity.AudienceCodehub, "none": ""} {
		status, body := ask(s.machine, "", audience)
		assert.Equal(t, http.StatusBadRequest, status, name+": "+body)
		assert.Equal(t, machinegen.InvalidRequest, machineRefusal(t, body), name)
	}

	status, body := s.exchangeAs(t, s.machine, s.policy, s.coworkID, "", identity.AudiencePolicy)
	assert.Equal(t, http.StatusBadRequest, status, "a key of another service: "+body)
	assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body))

	second := s.machineListener(t, s.issuer)
	status, body = ask(s.machine, "replayed-once-0123456789", identity.AudiencePolicy)
	require.Equal(t, http.StatusOK, status, body)
	for name, listener := range map[string]string{"the same listener": s.machine, "a second one": second} {
		status, body = s.exchangeAs(t, listener, s.cowork, s.coworkID, "replayed-once-0123456789", identity.AudiencePolicy)
		assert.Equal(t, http.StatusBadRequest, status, "a replay at "+name+": "+body)
	}

	off := s.machineListener(t, nil)
	status, body = s.exchangeAs(t, off, s.cowork, s.coworkID, "", identity.AudiencePolicy)
	assert.Equal(t, http.StatusServiceUnavailable, status, "the issuer off: "+body)

	status, body = ask(s.machine, "", identity.AudiencePolicy)
	require.Equal(t, http.StatusOK, status, "the control: the principal is live before its organization goes: "+body)
	require.NoError(t, orgpg.New(s.pool).DeleteOrg(t.Context(), orgRootGRN(s.globex)))
	assert.Equal(t, 0, countRows(t, s.pool, "SELECT count(*) FROM service_principals WHERE grn = $1", s.coworkID.String()),
		"the organization's deletion removes its principals")
	status, body = ask(s.machine, "", identity.AudiencePolicy)
	assert.Equal(t, http.StatusBadRequest, status, "the organization is deleted: "+body)
	assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body))
}

// TestServicePrincipal_IntrospectionAdmitsTheAudiencesOwnPrincipalOnce
// holds introspection's caller over Postgres: the principal of a
// service that is an audience reads a token of that audience, another
// service's principal reads it as inactive, the root-owned form the
// issuer used to admit is refused, and an assertion admitted by one
// listener is refused by a second that shares the database.
func TestServicePrincipal_IntrospectionAdmitsTheAudiencesOwnPrincipalOnce(t *testing.T) {
	s := newIssuerStack(t)
	resp, dump := s.exchange(t, s.approvedTokens(t).AccessToken, identity.AudiencePolicy)
	require.Equal(t, http.StatusOK, resp.StatusCode, dump)
	var granted struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal([]byte(bodyOf(dump)), &granted))

	status, body := s.introspect(t, granted.AccessToken)
	require.Equal(t, http.StatusOK, status, body)
	var read machinegen.Introspection
	require.NoError(t, json.Unmarshal([]byte(body), &read))
	assert.True(t, read.Active, "the policy service's principal reads its own audience")

	status, body = s.introspectAs(t, s.machine, s.cowork, s.coworkID, "", granted.AccessToken)
	require.Equal(t, http.StatusOK, status, body)
	assert.JSONEq(t, `{"active":false}`, body, "cowork's principal reads no policy token")

	root := identity.PrincipalGRN("root", identity.AudiencePolicy)
	status, body = s.introspectAs(t, s.machine, s.policy, root, "", granted.AccessToken)
	assert.Equal(t, http.StatusBadRequest, status, "grn:root/iam/service/policy is no caller: "+body)
	assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body))

	second := s.machineListener(t, s.issuer)
	status, body = s.introspectAs(t, s.machine, s.policy, s.policyID, "spent-once-0123456789ab", granted.AccessToken)
	require.Equal(t, http.StatusOK, status, body)
	status, body = s.introspectAs(t, second, s.policy, s.policyID, "spent-once-0123456789ab", granted.AccessToken)
	assert.Equal(t, http.StatusBadRequest, status, "a second listener refuses what the first admitted: "+body)
	assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body))
}

// TestServicePrincipal_CreateOrgWritesTheFirstPartyPrincipals holds
// that an organization made by the store as production builds it
// carries cowork's principal and no other.
func TestServicePrincipal_CreateOrgWritesTheFirstPartyPrincipals(t *testing.T) {
	s := newIssuerStack(t)
	assert.Equal(t, 1, countRows(t, s.pool, "SELECT count(*) FROM service_principals WHERE grn = 'grn:acme/iam/service/cowork'"))
	assert.Equal(t, 1, countRows(t, s.pool, "SELECT count(*) FROM service_principals WHERE grn LIKE 'grn:acme/%'"),
		"acme was made by the store as production builds it, so cowork's is its only principal")
	assert.Equal(t, 2, countRows(t, s.pool, "SELECT count(*) FROM service_principals WHERE grn LIKE 'grn:globex/%'"),
		"globex was made through the seam with two services")
}
