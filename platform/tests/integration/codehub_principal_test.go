package integration_test

import (
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
)

// TestCodehubPrincipal_ExchangeAnswersTheIssuersTokenForPolicy runs
// the exchange over Postgres for the principal of codehub in an
// organization: the token verifies against the issuer's key, names the
// principal and the policy service. The policy service refusing that
// token until it holds a caller row for codehub is its own change.
func TestCodehubPrincipal_ExchangeAnswersTheIssuersTokenForPolicy(t *testing.T) {
	s := newIssuerStack(t)

	status, body := s.exchangeAs(t, s.machine, s.codehub, s.codehubID, "", identity.AudiencePolicy)
	require.Equal(t, http.StatusOK, status, body)
	var granted machinegen.OAuthToken
	require.NoError(t, json.Unmarshal([]byte(body), &granted))
	claims, err := s.key.Verify(granted.AccessToken, time.Now())
	require.NoError(t, err)
	assert.Equal(t, issuerURL, claims.Issuer)
	assert.Equal(t, "grn:globex/iam/service/codehub", claims.Subject)
	assert.Equal(t, identity.AudiencePolicy, claims.Audience)
	assert.LessOrEqual(t, claims.Lifetime, 300*time.Second)
}

// TestCodehubPrincipal_ExchangeRefusesWhatIsNotCodehubsLiveAssertion
// holds the exchange's refusals for codehub's principal: cowork's key,
// a key no configuration holds, a replayed jti on the same listener
// and on a second, and an audience other than policy. The success
// before each is its control, so a refusal is the mechanism's and not
// a request that never could have passed.
func TestCodehubPrincipal_ExchangeRefusesWhatIsNotCodehubsLiveAssertion(t *testing.T) {
	s := newIssuerStack(t)
	status, body := s.exchangeAs(t, s.machine, s.codehub, s.codehubID, "", identity.AudiencePolicy)
	require.Equal(t, http.StatusOK, status, "the control: codehub's own key is accepted: "+body)

	for name, key := range map[string]func() (int, string){
		"cowork's key": func() (int, string) {
			return s.exchangeAs(t, s.machine, s.cowork, s.codehubID, "", identity.AudiencePolicy)
		},
		"a key no configuration holds": func() (int, string) {
			return s.exchangeAs(t, s.machine, serviceKey(t), s.codehubID, "", identity.AudiencePolicy)
		},
	} {
		status, body := key()
		assert.Equal(t, http.StatusBadRequest, status, name+": "+body)
		assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body), name)
	}

	second := s.machineListener(t, s.issuer)
	status, body = s.exchangeAs(t, s.machine, s.codehub, s.codehubID, "replayed-once-0123456789", identity.AudiencePolicy)
	require.Equal(t, http.StatusOK, status, body)
	for name, listener := range map[string]string{"the same listener": s.machine, "a second one": second} {
		status, body = s.exchangeAs(t, listener, s.codehub, s.codehubID, "replayed-once-0123456789", identity.AudiencePolicy)
		assert.Equal(t, http.StatusBadRequest, status, "a replay at "+name+": "+body)
		assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body), name)
	}

	status, body = s.exchangeAs(t, s.machine, s.codehub, s.codehubID, "", identity.AudienceCodehub)
	assert.Equal(t, http.StatusBadRequest, status, "another audience: "+body)
	assert.Equal(t, machinegen.InvalidRequest, machineRefusal(t, body))
}

// TestCodehubPrincipal_IntrospectsTheTokensOfItsOwnAudienceAlone holds
// that codehub's principal reads a token issued for codehub as active
// and a token issued for policy as inactive, the same token read by
// the policy service's principal being active.
func TestCodehubPrincipal_IntrospectsTheTokensOfItsOwnAudienceAlone(t *testing.T) {
	s := newIssuerStack(t)
	tokens := s.approvedTokens(t)
	token := func(audience string) string {
		resp, dump := s.exchange(t, tokens.AccessToken, audience)
		require.Equal(t, http.StatusOK, resp.StatusCode, dump)
		var granted struct {
			AccessToken string `json:"access_token"`
		}
		require.NoError(t, json.Unmarshal([]byte(bodyOf(dump)), &granted))
		return granted.AccessToken
	}
	forCodehub, forPolicy := token(identity.AudienceCodehub), token(identity.AudiencePolicy)

	status, body := s.introspectAs(t, s.machine, s.codehub, s.codehubID, "", forCodehub)
	require.Equal(t, http.StatusOK, status, body)
	var read machinegen.Introspection
	require.NoError(t, json.Unmarshal([]byte(body), &read))
	assert.True(t, read.Active, "codehub's principal reads a token of audience codehub")
	assert.Equal(t, identity.AudienceCodehub, *read.Aud)

	status, body = s.introspectAs(t, s.machine, s.policy, s.policyID, "", forPolicy)
	require.Equal(t, http.StatusOK, status, body)
	require.NoError(t, json.Unmarshal([]byte(body), &read))
	assert.True(t, read.Active, "the control: the token of audience policy is live")

	status, body = s.introspectAs(t, s.machine, s.codehub, s.codehubID, "", forPolicy)
	require.Equal(t, http.StatusOK, status, body)
	assert.JSONEq(t, `{"active":false}`, body, "codehub's principal reads no token of audience policy")
}

// TestMigration000074_WritesCodehubsPrincipalForEachExistingOrganization
// migrates a schema to 000073 holding an organization with cowork's
// principal, applies 000074, and reads that the organization gained
// codehub's and kept cowork's, then that its down removes codehub's
// alone.
func TestMigration000074_WritesCodehubsPrincipalForEachExistingOrganization(t *testing.T) {
	const schema = "migrate_000074_codehub"
	p := pool(t)
	m, err := migrate.New("file://"+migrationsDir(t), isolatedSchema(t, p, schema))
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(73))

	root := orgRootGRN("aged").String()
	_, err = p.Exec(t.Context(), `INSERT INTO `+schema+`.orgs (grn, name, created_at, updated_at) VALUES ($1, 'aged', now(), now())`, root)
	require.NoError(t, err)
	_, err = p.Exec(t.Context(), `INSERT INTO `+schema+`.service_principals (grn, org_grn) VALUES ('grn:aged/iam/service/cowork', $1)`, root)
	require.NoError(t, err)
	require.Equal(t, 0, countIn(t, p, schema, "service_principals", "grn = 'grn:aged/iam/service/codehub'"),
		"the setup: the organization predates codehub's principal")

	require.NoError(t, m.Migrate(74))
	assert.Equal(t, 1, countIn(t, p, schema, "service_principals", "grn = 'grn:aged/iam/service/codehub' AND org_grn = $1", root))
	assert.Equal(t, 1, countIn(t, p, schema, "service_principals", "grn = 'grn:aged/iam/service/cowork'"), "cowork's stayed")

	require.NoError(t, m.Migrate(73))
	assert.Equal(t, 0, countIn(t, p, schema, "service_principals", "grn LIKE '%/iam/service/codehub'"), "the down removed codehub's")
	assert.Equal(t, 1, countIn(t, p, schema, "service_principals", "grn = 'grn:aged/iam/service/cowork'"), "the down left cowork's")
}

// TestCreateOrg_WritesCodehubsPrincipalBesideCoworks holds that a new
// organization written by the store as production builds it carries
// both principals, each owned by it.
func TestCreateOrg_WritesCodehubsPrincipalBesideCoworks(t *testing.T) {
	p := pool(t)
	created := orgRootGRN("fresh")
	require.NoError(t, orgpg.New(p).CreateOrg(t.Context(), org.NewOrganization(created, "fresh")))

	for _, service := range []string{identity.ServiceCowork, identity.ServiceCodehub} {
		assert.Equal(t, 1, countRows(t, p, "SELECT count(*) FROM service_principals WHERE grn = $1 AND org_grn = $2",
			identity.PrincipalGRN("fresh", service).String(), created.String()), service)
	}
	assert.Equal(t, 2, countRows(t, p, "SELECT count(*) FROM service_principals WHERE org_grn = $1", created.String()))
}

// TestCodehubPrincipal_ExchangeTellsAnOrganizationNotHeldFromAWrongKey
// holds the two answers for an assertion naming an organization garam
// holds none of: `unauthorized_client` where codehub's key signed it,
// `invalid_client` where another key did, so a caller without the key
// learns nothing about which organizations exist. A principal gone from
// an organization that exists is neither, and answers `invalid_client`
// as it did. The first request is the control: the same assertion for
// an organization that exists is accepted.
func TestCodehubPrincipal_ExchangeTellsAnOrganizationNotHeldFromAWrongKey(t *testing.T) {
	s := newIssuerStack(t)
	status, body := s.exchangeAs(t, s.machine, s.codehub, s.codehubID, "", identity.AudiencePolicy)
	require.Equal(t, http.StatusOK, status, "the control: an organization garam holds: "+body)

	absent := identity.PrincipalGRN("ghost", identity.ServiceCodehub)
	status, body = s.exchangeAs(t, s.machine, s.codehub, absent, "", identity.AudiencePolicy)
	assert.Equal(t, http.StatusBadRequest, status, body)
	assert.Equal(t, machinegen.UnauthorizedClient, machineRefusal(t, body), "signed by codehub's key")

	for name, key := range map[string]*rsa.PrivateKey{"cowork's key": s.cowork, "a key no configuration holds": serviceKey(t)} {
		status, body = s.exchangeAs(t, s.machine, key, absent, "", identity.AudiencePolicy)
		assert.Equal(t, http.StatusBadRequest, status, name+": "+body)
		assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body), "signed by "+name)
	}

	p := pool(t)
	created := orgRootGRN("gone")
	require.NoError(t, orgpg.New(p).CreateOrg(t.Context(), org.NewOrganization(created, "gone")))
	revoked := identity.PrincipalGRN("gone", identity.ServiceCodehub)
	_, err := p.Exec(t.Context(), `DELETE FROM service_principals WHERE grn = $1`, revoked.String())
	require.NoError(t, err)
	status, body = s.exchangeAs(t, s.machine, s.codehub, revoked, "", identity.AudiencePolicy)
	assert.Equal(t, http.StatusBadRequest, status, body)
	assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body), "a principal gone from an organization that exists")
}
