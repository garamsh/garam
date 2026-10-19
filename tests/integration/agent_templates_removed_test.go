package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigration000057_LeavesNoAgentTemplate holds that garam keeps no
// agent template of its own (ADR-0117): the table and the guard 000034
// wrote over it are gone. agent_definitions, which 000026 extended in
// the same migration that created the table, is the control: it is
// still there, so what is missing is the template table and not a
// schema that failed to migrate.
func TestMigration000057_LeavesNoAgentTemplate(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	var table, guard, definitions *string
	require.NoError(t, p.QueryRow(ctx,
		`SELECT to_regclass('agent_templates')::text, to_regproc('agent_templates_retired')::text,
		        to_regclass('agent_definitions')::text`).Scan(&table, &guard, &definitions))
	assert.Nil(t, table, "agent_templates is gone")
	assert.Nil(t, guard, "its guard went with it")
	require.NotNil(t, definitions)
	assert.Equal(t, "agent_definitions", *definitions)
}
