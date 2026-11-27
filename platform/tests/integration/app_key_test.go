package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/app"
	apppg "github.com/garamsh/garam/platform/internal/app/postgres"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/operator"
)

// TestAppKey_AnInstallationExchangesForTheAudiencesItsAppLists runs
// ADR-0192 decisions 5 and 7 over Postgres: an installation of an app
// whose slug holds keys in the service keys file, and whose record
// lists the policy service, exchanges an assertion its app key signed
// for the issuer's token, `sub` the installation. The audience its app
// does not list is refused under the installation's hold and spends
// nothing, a replay is refused on a second listener, and once the app
// is uninstalled the same key proves nothing.
func TestAppKey_AnInstallationExchangesForTheAudiencesItsAppLists(t *testing.T) {
	s := newIssuerStack(t)
	ctx := t.Context()
	execSQL(t, s.pool, `INSERT INTO apps (grn, slug, org_grn, name, visibility, instances, delivery, issuer_audiences)
		VALUES ('grn:globex/iam/app/deployer', 'deployer', 'grn:root/iam/org/globex', 'Deployer', 'private',
		        'none', 'pull', '{policy}')`)
	records := apppg.New(s.pool)
	deployer, err := records.GetApp(ctx, grn.New(s.globex, app.AppType, "deployer"))
	require.NoError(t, err)
	assert.Equal(t, []string{identity.AudiencePolicy}, deployer.IssuerAudiences, "the record reads its audiences")
	installation, err := app.NewInstallation(orgRootGRN(s.globex), deployer, scopedGRN(s.globex, "user", "ada"), app.Scope{})
	require.NoError(t, err)
	_, err = records.CreateInstallation(ctx, installation)
	require.NoError(t, err)
	key := serviceKey(t)
	s.keys["deployer"] = []operator.Key{publicKey(t, key)}
	listener := s.machineListener(t, s.issuer)

	status, body := s.exchangeAs(t, listener, key, installation.GRN, "deployer-unlisted-0123456", identity.AudienceCodehub)
	assert.Equal(t, http.StatusBadRequest, status, "an audience its app does not list: "+body)
	assert.Equal(t, machinegen.InvalidRequest, machineRefusal(t, body))

	status, body = s.exchangeAs(t, listener, key, installation.GRN, "deployer-unlisted-0123456", identity.AudiencePolicy)
	require.Equal(t, http.StatusOK, status, "the refused jti was not spent: "+body)
	var granted machinegen.OAuthToken
	require.NoError(t, json.Unmarshal([]byte(body), &granted))
	claims, err := s.key.Verify(granted.AccessToken, time.Now())
	require.NoError(t, err)
	assert.Equal(t, installation.GRN.String(), claims.Subject)
	assert.Equal(t, identity.AudiencePolicy, claims.Audience)
	assert.Empty(t, claims.SessionID)

	status, body = s.exchangeAs(t, s.machineListener(t, s.issuer), key, installation.GRN,
		"deployer-unlisted-0123456", identity.AudiencePolicy)
	assert.Equal(t, http.StatusBadRequest, status, "a replay on a second listener: "+body)
	assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body))

	_, err = records.DeleteInstallation(ctx, installation.GRN)
	require.NoError(t, err)
	status, body = s.exchangeAs(t, listener, key, installation.GRN, "", identity.AudiencePolicy)
	assert.Equal(t, http.StatusBadRequest, status, "an uninstalled app: "+body)
	assert.Equal(t, machinegen.InvalidClient, machineRefusal(t, body))
}
