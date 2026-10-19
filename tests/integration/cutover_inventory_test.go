package integration_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// runInventory runs the shipped command against the test database and
// answers what it printed, one decoded JSON object per line.
func runInventory(t *testing.T, slug string) ([]map[string]any, string, error) {
	t.Helper()
	t.Setenv("GARAM_DATABASE_URL", os.Getenv("GARAM_TEST_DATABASE_URL"))
	var out bytes.Buffer
	root := cli.NewRootCommand()
	root.SetOut(&out)
	root.SetArgs([]string{"cutover-inventory", "--org", slug})
	if err := root.Execute(); err != nil {
		return nil, out.String(), err
	}
	var lines []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(out.Bytes()))
	for scanner.Scan() {
		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line), "every line is one JSON object: %s", scanner.Text())
		lines = append(lines, line)
	}
	return lines, out.String(), nil
}

// TestCutoverInventory_AccountsForEverySourceRecordWithoutAValue lays
// out one organization holding each kind of record the cutover meets:
// a claimed definition, one whose stored values no longer parse, one
// no operator claimed, an agent no definition describes, and one
// already activated. The inventory answers one line each,
// closed by counts equal to the organization's own, and prints a
// digest of every value and no value.
func TestCutoverInventory_AccountsForEverySourceRecordWithoutAValue(t *testing.T) {
	ctx := context.Background()
	f := newCutover(t, agent.Values{"model": "inventory-secret-one"})
	acme := orgRootGRN("acme")

	broken := defineAgent(t, f.p, f.agents, f.controller, agent.Values{"model": "x"})
	_, err := f.agents.Claim(ctx, broken.Agent(), f.controller)
	require.NoError(t, err)
	withDefinitionsOpen(t, f.p, func(tx pgx.Tx) {
		_, err := tx.Exec(ctx, `UPDATE agent_definitions SET agent_values = '{"tools.pins.git":"inventory-secret-two"}' WHERE agent_grn = $1`,
			broken.Agent().String())
		require.NoError(t, err)
	})
	unclaimed := defineAgent(t, f.p, f.agents, f.controller, agent.Values{"model": "inventory-secret-three"})

	bare := seedAgent(t, f.agents, acme)
	_, err = f.agents.Assign(ctx, bare, f.controller)
	require.NoError(t, err)
	activated := f
	activated.agent = seedAgent(t, f.agents, acme)
	_, err = f.agents.Assign(ctx, activated.agent, f.controller)
	require.NoError(t, err)
	activated.switched(t, "import-1")
	_, _, err = f.exec.Activate(ctx, first(t, activated))
	require.NoError(t, err)

	lines, printed, err := runInventory(t, "acme")
	require.NoError(t, err)
	byGRN := map[string]map[string]any{}
	for _, l := range lines[:len(lines)-1] {
		byGRN[l["grn"].(string)] = l
	}
	expect := map[grn.GRN][2]string{
		f.agent:           {"agent", "eligible"},
		broken.Agent():    {"agent", "blocked:unparseable"},
		unclaimed.Agent(): {"definition", "ineligible:unclaimed"},
		bare:              {"agent", "eligible"},
		activated.agent:   {"agent", "not-applicable"},
	}
	for g, want := range expect {
		got, ok := byGRN[g.String()]
		require.True(t, ok, "%s has a line", g)
		assert.Equal(t, want[0], got["record"], "%s", g)
		assert.Equal(t, want[1], got["class"], "%s", g)
	}

	eligible := byGRN[f.agent.String()]
	assert.Equal(t, "import", eligible["disposition"])
	assert.Equal(t, agent.SourceDigest(f.agent, f.controller, agent.Values{"model": "inventory-secret-one"}).String(), eligible["sourceDigest"])
	assert.Equal(t, map[string]any{"model": agent.ValueDigest("inventory-secret-one").String()}, eligible["valueSha256"])
	assert.Equal(t, agent.SourceDigest(bare, f.controller, agent.Values{}).String(), byGRN[bare.String()]["sourceDigest"],
		"an agent with no definition is digested with empty values under its assignee")
	assert.Equal(t, "preserve-and-block", byGRN[broken.Agent().String()]["disposition"])
	assert.Equal(t, []any{"tools.pins.git"}, byGRN[broken.Agent().String()]["keys"])
	assert.NotContains(t, byGRN[broken.Agent().String()], "sourceDigest")
	assert.Equal(t, map[string]any{"importId": "import-1", "stage": "switched"}, byGRN[activated.agent.String()]["attempt"])

	counts, err := f.agents.CountByOrg(ctx, acme)
	require.NoError(t, err)
	summary := lines[len(lines)-1]
	assert.Equal(t, "summary", summary["record"])
	assert.EqualValues(t, counts.Agents, summary["agents"])
	assert.EqualValues(t, counts.Definitions, summary["definitions"])
	assert.NotContains(t, summary, "templates", "garam keeps no template to count (ADR-0117)")
	assert.Len(t, lines, counts.Agents+1+1, "one line per agent and unclaimed definition")

	for _, secret := range []string{"inventory-secret-one", "inventory-secret-two", "inventory-secret-three"} {
		assert.NotContains(t, printed, secret)
	}
	assert.Contains(t, printed, agent.ValueDigest("inventory-secret-three").String(),
		"control: the value is in the organization, and the line names it by its digest")
}

func TestCutoverInventory_RefusesAnOrganizationThatDoesNotExist(t *testing.T) {
	newCutover(t, agent.Values{})
	_, _, err := runInventory(t, "acme")
	require.NoError(t, err, "control: the organization that exists")
	_, printed, err := runInventory(t, "globex")
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound), "got %v", err)
	assert.Empty(t, printed)
}
