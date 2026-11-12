package integration_test

import (
	"context"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
)

// TestMigration000069_RestatesTheChecksInTheGrammarAndRewritesNoRow
// migrates a schema of its own to 000068 and applies 000069 over a
// database holding names in the five-segment form. Before it, a name in
// ADR-0142's grammar is refused by the check that reads a name at the
// five-segment positions; after it the same name is stored, a name of
// the wrong type or of another organization is still refused, and the
// rows in the five-segment form are as they were: the migration
// rewrites nothing and does not fail on them. 000069 down is applied
// and runs over the result, and 000069 up runs again over that.
func TestMigration000069_RestatesTheChecksInTheGrammarAndRewritesNoRow(t *testing.T) {
	ctx := context.Background()
	const schema = "grn_grammar_000069"
	databaseURL := isolatedSchema(t, pool(t), schema)
	m, err := migrate.New("file://"+migrationsDir(t), databaseURL)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = m.Close() })
	require.NoError(t, m.Migrate(68))
	sp := connect(t, databaseURL, nil)

	former := formerGRN("grm", "org", "grm")
	formerAgent := formerGRN("grm", "agent", "9f2ac1b40d8e7a35")
	org := grn.New(grn.RootOwner, "org", "grm2")
	other := grn.New(grn.RootOwner, "org", "grm3")
	agent := grn.New("grm2", "agent", "9f2ac1b40d8e7a35")
	crossing := grn.New("grm3", "agent", "9f2ac1b40d8e7a35")
	wrongType := grn.New("grm2", "operator", "9f2ac1b40d8e7a35")

	insertAgent := func(agentName, orgName string) error {
		_, err := sp.Exec(ctx, `INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
			VALUES ($1, $2, 'pending', 'unknown', now(), now())`, agentName, orgName)
		return err
	}
	for name, slug := range map[string]string{former: "grm", org.String(): "grm2", other.String(): "grm3"} {
		_, err := sp.Exec(ctx, `INSERT INTO orgs (grn, name, created_at, updated_at) VALUES ($1, $2, now(), now())`, name, slug)
		require.NoError(t, err, name)
	}
	require.NoError(t, insertAgent(formerAgent, former), "a five-segment row is stored before the migration")
	require.Error(t, insertAgent(agent.String(), org.String()),
		"the control: before the migration the grammar's name is refused by a check")

	require.NoError(t, m.Migrate(69), "the migration does not fail on a five-segment row")
	require.NoError(t, insertAgent(agent.String(), org.String()), "the grammar's name is stored")
	require.Error(t, insertAgent(crossing.String(), org.String()),
		"an agent named under one organization is refused under another's org_grn")
	require.Error(t, insertAgent(wrongType.String(), org.String()),
		"a name of another type is refused where an agent is stored")

	var stored string
	require.NoError(t, sp.QueryRow(ctx, `SELECT grn FROM agents WHERE grn = $1`, formerAgent).Scan(&stored))
	assert.Equal(t, formerAgent, stored, "the five-segment row keeps its text")
	_, err = sp.Exec(ctx, `UPDATE agents SET runtime_state = 'unknown' WHERE grn = $1`, formerAgent)
	assert.Error(t, err, "an update of a five-segment row is refused: the check was added NOT VALID, not validated away")

	var trigger int
	require.NoError(t, sp.QueryRow(ctx, `SELECT count(*) FROM pg_trigger
		WHERE tgname = 'policy_attachments_same_org'
		AND tgrelid = 'policy_attachments'::regclass`).Scan(&trigger))
	assert.Equal(t, 1, trigger, "the attachment trigger stays")

	require.NoError(t, m.Migrate(68), "000069 down runs over the rows it left")
	require.NoError(t, m.Migrate(69), "000069 up runs again over the result of its down")
}

// TestGRNFunctions_AgreeWithLibGRN holds the SQL functions 000069
// defines to lib/grn on a table of names: a root-owned organization, a
// project-scoped and a nested name, a name with a locator, and names
// the grammar refuses, for which the functions give the empty string.
func TestGRNFunctions_AgreeWithLibGRN(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	read := func(name string) (org, service, typ, id string) {
		require.NoError(t, p.QueryRow(ctx,
			`SELECT grn_org($1), grn_service($1), grn_type($1), grn_id($1)`, name).Scan(&org, &service, &typ, &id))
		return org, service, typ, id
	}

	for _, name := range []string{
		"grn:root/iam/org/acme",
		"grn:acme/iam/user/alice",
		"grn:acme/iam/agent/9f2ac1b40d8e7a35",
		"grn:acme/iam/project/frontend",
		"grn:acme/cowork/project/frontend/channel/general",
		"grn:acme/cowork/page/home#refs/heads/main:cmd/main.go",
		"grn:acme/cowork/channel/general",
	} {
		g, err := grn.Parse(name)
		require.NoError(t, err, name)
		org, service, typ, id := read(name)
		assert.Equal(t, g.Organization, org, name)
		assert.Equal(t, g.Service, service, name)
		assert.Equal(t, g.ServiceType, typ, name)
		assert.Equal(t, g.Identifier, id, name)
	}

	for _, name := range []string{
		formerGRN("acme", "agent", "bot"),
		"grn:acme/iam/user",
		"grn:acme/iam/user/alice/extra",
		"grn:acme/iam/widget/x",
		"acme/iam/user/alice",
		"",
	} {
		_, err := grn.Parse(name)
		if name == "grn:acme/iam/widget/x" {
			require.Error(t, err, name)
			continue // a well-shaped name of an unknown type: lib/grn refuses it, the functions only read its shape
		}
		require.Error(t, err, name)
		org, service, typ, id := read(name)
		assert.Empty(t, org+service+typ+id, "a name lib/grn refuses is read as empty: %q", name)
	}

	var isNull bool
	require.NoError(t, p.QueryRow(ctx, `SELECT grn_org(NULL) IS NULL`).Scan(&isNull))
	assert.True(t, isNull, "a NULL column stays NULL so a nullable CHECK passes as it did")
}
