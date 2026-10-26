package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/authn"
	authnpg "github.com/garamsh/garam/internal/authn/postgres"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/user"
	userpg "github.com/garamsh/garam/internal/user/postgres"
)

// cliJSON runs one command with -o json and decodes the answer.
func cliJSON(t *testing.T, into any, args ...string) {
	t.Helper()
	out, err := runCLI(t, io.Discard, append(args, "-o", "json")...)
	require.NoError(t, err, strings.Join(args, " "))
	require.NoError(t, json.Unmarshal([]byte(out), into), out)
}

// identifierOf is a GRN's last segment, which a command names an
// object by.
func identifierOf(g string) string { return g[strings.LastIndex(g, ":")+1:] }

// TestCLIOrgs_CreateAnOrgAndItsTeamsProjectsMembersAndInvitations
// drives #1632's orgs group against the API as serve api composes it:
// what each command writes the next reads back. An invitation's token
// reaches the file --out names, a person signed out reads it, and
// another accepts it into the organization's members.
func TestCLIOrgs_CreateAnOrgAndItsTeamsProjectsMembersAndInvitations(t *testing.T) {
	endpoint := signedInCLI(t)

	var created struct{ Grn, Name string }
	cliJSON(t, &created, "orgs", "create", "--name", "Globex")
	globex := identifierOf(created.Grn)
	out, err := runCLI(t, io.Discard, "orgs", "update", globex, "--name", "Globex Corp")
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^NAME\s+Globex Corp$`, out)
	out, err = runCLI(t, io.Discard, "orgs", "list")
	require.NoError(t, err)
	assert.Contains(t, out, created.Grn)

	var team struct{ Grn string }
	cliJSON(t, &team, "groups", "create", "--name", "eng", "--org", globex)
	eng := identifierOf(team.Grn)
	ada := "grn:root:default:user:ada"
	_, err = runCLI(t, io.Discard, "groups", "members", "add", eng, "--member-grn", ada, "--org", globex)
	require.NoError(t, err)
	out, err = runCLI(t, io.Discard, "groups", "members", "list", eng, "--org", globex)
	require.NoError(t, err)
	assert.Contains(t, out, ada)
	_, err = runCLI(t, io.Discard, "groups", "members", "remove", eng, ada, "--org", globex)
	require.NoError(t, err)
	out, err = runCLI(t, io.Discard, "groups", "members", "list", eng, "--org", globex)
	require.NoError(t, err)
	assert.NotContains(t, out, ada, "removed")

	var project struct{ Grn string }
	cliJSON(t, &project, "projects", "create", "--name", "web", "--org", globex)
	web := identifierOf(project.Grn)
	_, err = runCLI(t, io.Discard, "projects", "members", "add", web, "--member-grn", ada, "--org", globex)
	require.NoError(t, err)
	out, err = runCLI(t, io.Discard, "projects", "members", "list", web, "--org", globex)
	require.NoError(t, err)
	assert.Contains(t, out, ada)

	invitation := filepath.Join(t.TempDir(), "invitation.json")
	stderr := &lockedBuffer{}
	out, err = runCLI(t, stderr, "invitations", "create", "--role", "member", "--out", invitation, "--org", globex)
	require.NoError(t, err, stderr.String())
	raw, err := os.ReadFile(invitation)
	require.NoError(t, err)
	var invite struct{ Token string }
	require.NoError(t, json.Unmarshal(raw, &invite))
	require.NotEmpty(t, invite.Token, "the file holds the token garam answered")
	assert.NotContains(t, out, invite.Token, "the token reaches no output")
	assert.NotContains(t, stderr.String(), invite.Token)

	// A person who is not signed in reads what the invitation admits to.
	signedOut := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", signedOut)
	out, err = runCLI(t, io.Discard, "invitations", "get", "--", invite.Token)
	require.NoError(t, err)
	assert.Regexp(t, `(?m)^STATE\s+pending$`, out)

	// mel accepts it, signed in as herself.
	signInAs(t, endpoint, "mel")
	_, err = runCLI(t, io.Discard, "invitations", "accept", "--", invite.Token)
	require.NoError(t, err)
	signInAs(t, endpoint, "ada")
	out, err = runCLI(t, io.Discard, "members", "list", "--org", globex)
	require.NoError(t, err)
	assert.Contains(t, out, "grn:root:default:user:mel")

	_, err = runCLI(t, io.Discard, "orgs", "deletion-preview", "--org", globex)
	require.NoError(t, err)
	_, err = runCLI(t, io.Discard, "orgs", "delete", globex)
	require.NoError(t, err)
	_, err = runCLI(t, io.Discard, "orgs", "get", globex)
	require.Error(t, err, "a deleted organization is not found")
}

// signInAs leaves a credentials file holding a fresh session for the
// user id names, creating the user where it does not exist, under its
// own XDG_CONFIG_HOME.
func signInAs(t *testing.T, endpoint, id string) {
	t.Helper()
	ctx := context.Background()
	// connect and not pool: pool truncates every table, which would
	// take the organization this test is reading with it.
	p := connect(t, os.Getenv("GARAM_TEST_DATABASE_URL"), nil)
	g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, id)
	users := userpg.New(p)
	if _, err := users.Get(ctx, g); err != nil {
		require.NoError(t, users.Create(ctx, user.New(g, id)))
	}
	token, err := authn.NewToken()
	require.NoError(t, err)
	require.NoError(t, authnpg.NewSessionStore(p).Create(ctx, token, authn.NewSession(g, time.Hour)))
	config := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	file, err := json.Marshal(map[string]any{"endpoints": map[string]any{endpoint: map[string]any{
		"accessToken": token, "accessExpiresAt": time.Now().Add(time.Hour),
		"refreshToken": "unused", "refreshExpiresAt": time.Now().Add(24 * time.Hour), "org": "acme",
	}}})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(config, "garam"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(config, "garam", "credentials"), file, 0o600))
}
