package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apigen "github.com/garamsh/garam/platform/api"
	agentpg "github.com/garamsh/garam/platform/internal/agent/postgres"
	"github.com/garamsh/garam/platform/internal/api"
	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/cli"
	"github.com/garamsh/garam/platform/internal/grn"
	messagepg "github.com/garamsh/garam/platform/internal/message/postgres"
	operatorpg "github.com/garamsh/garam/platform/internal/operator/postgres"
	"github.com/garamsh/garam/platform/internal/org"
	orgpg "github.com/garamsh/garam/platform/internal/org/postgres"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// signedInCLI composes the browser API as serve api does, with key
// issuance configured, behind the console's /api prefix, and leaves a
// credentials file holding ada's session as garam login would. The
// sign-in itself is the subject of
// TestCLI_LogsInByTheDeviceGrantAndReadsTheOrgsObjects, and these tests
// are about what a signed-in CLI reaches.
func signedInCLI(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	p := pool(t)
	truncate(t, p)
	policies, engine := policyEngine(t, p)
	orgs := orgpg.New(p)
	users := userpg.New(p)
	acme := orgRootGRN("acme")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	ada := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, "ada")
	require.NoError(t, users.Create(ctx, user.New(ada, "ada")))
	require.NoError(t, orgs.AddMember(ctx, org.NewMembership(acme, ada, org.RoleOwner)))
	token, err := authn.NewToken()
	require.NoError(t, err)
	sessions := authnpg.NewSessionStore(p)
	require.NoError(t, sessions.Create(ctx, token, authn.NewSession(ada, time.Hour)))

	srv := api.NewServer(users, orgs, agentpg.New(p), operatorpg.New(p), messagepg.New(p), nil,
		sessions, nil, nil, nil, nil,
		api.NewGRPCEvaluator(engine), api.NewGRPCPolicyManager(policyService(t, policies)), heldAuthorities{orgs},
		cli.NewAuthorityService(p, engine), nil,
		api.Config{SessionTTL: time.Hour, TokenURL: "https://machine.garam.test/oauth/token"})
	handler := apigen.HandlerWithOptions(srv, apigen.StdHTTPServerOptions{ErrorHandlerFunc: api.BindingError})
	browser := httptest.NewServer(http.StripPrefix("/api", handler))
	t.Cleanup(browser.Close)

	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("GARAM_ENDPOINT", browser.URL)
	file, err := json.Marshal(map[string]any{"endpoints": map[string]any{browser.URL: map[string]any{
		"accessToken": token, "accessExpiresAt": time.Now().Add(time.Hour),
		"refreshToken": "unused", "refreshExpiresAt": time.Now().Add(24 * time.Hour), "org": "acme",
	}}})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(config, "garam"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(config, "garam", "credentials"), file, 0o600))
	return browser.URL
}

// TestCLIOperators_RegisterIssueAKeyListRevokeAndDelete drives the
// operators group the CLI derives from the specification against the
// real API (ADR-0122): what each command writes is read back by the
// next, and the issued key reaches the file --out names and no other
// output.
func TestCLIOperators_RegisterIssueAKeyListRevokeAndDelete(t *testing.T) {
	signedInCLI(t)

	out, err := runCLI(t, io.Discard, "operators", "register", "--identifier", "hosted", "--name", "Hosted", "-o", "json")
	require.NoError(t, err)
	var op apigen.Operator
	require.NoError(t, json.Unmarshal([]byte(out), &op))
	assert.Equal(t, "grn:acme:default:operator:hosted", op.Grn)

	out, err = runCLI(t, io.Discard, "operators", "update", "hosted", "--description", "runs the agents")
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^DESCRIPTION\s+runs the agents$`, out, "one answer is shown as field and value")
	out, err = runCLI(t, io.Discard, "operators", "list")
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^grn:acme:default:operator:hosted\s+Hosted\s+runs the agents`, out)

	keyFile := filepath.Join(t.TempDir(), "hosted-key.json")
	stderr := &lockedBuffer{}
	out, err = runCLI(t, stderr, "operators", "keys", "issue", "hosted", "--rotation", "planned", "--out", keyFile)
	require.NoError(t, err, stderr.String())
	info, err := os.Stat(keyFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	var issued apigen.IssuedOperatorKey
	raw, err := os.ReadFile(keyFile)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &issued))
	require.Contains(t, issued.PrivateKeyPem, "BEGIN PRIVATE KEY", "the file holds the key garam answered")
	assert.Contains(t, out, issued.Key.Kid, "stdout names the key it issued")
	for _, line := range strings.Split(strings.TrimSpace(issued.PrivateKeyPem), "\n") {
		assert.NotContains(t, out, line, "no line of the key reaches stdout")
		assert.NotContains(t, stderr.String(), line, "or stderr")
	}

	out, err = runCLI(t, io.Discard, "operators", "keys", "list", "hosted")
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^`+issued.Key.Kid+`\s+RS256\s+active`, out)
	// A kid is base64url and may begin with "-", so it follows "--", as
	// any argument that could read as a flag does.
	_, err = runCLI(t, io.Discard, "operators", "keys", "revoke", "--", "hosted", issued.Key.Kid)
	require.NoError(t, err)
	out, err = runCLI(t, io.Discard, "operators", "keys", "list", "hosted")
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^`+issued.Key.Kid+`\s+RS256\s+revoked`, out)

	_, err = runCLI(t, io.Discard, "operators", "delete", "hosted")
	require.NoError(t, err)
	_, err = runCLI(t, io.Discard, "operators", "get", "hosted")
	require.Error(t, err, "a deleted operator is not found")
}

// heldAuthorities answers whether an organization holds a retired
// certificate authority, as serve api's own adapter does, for the
// deletion preview.
type heldAuthorities struct{ orgs *orgpg.Store }

func (h heldAuthorities) HasAuthority(ctx context.Context, g grn.GRN) (bool, error) {
	return h.orgs.HoldsCertificateAuthority(ctx, g)
}
