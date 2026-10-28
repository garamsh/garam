package cli_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
)

// TestMigrate_RefusesWithoutADatabaseURL covers the check `migrate`
// makes before it opens anything. What it asserts is the refusal this
// check raises rather than that the command failed: everything after it
// fails too when the environment names no database, so a test reading
// only the presence of an error passes with the check deleted.
//
// The second half is the control. With the variable set the command
// gets past this check and fails at the next one, so what the first
// refusal answers is the empty value rather than the command.
func TestMigrate_RefusesWithoutADatabaseURL(t *testing.T) {
	// A directory that does not exist, so the failure past the check is
	// the migration source and never a database this test dialled.
	t.Setenv("GARAM_MIGRATIONS_DIR", filepath.Join(t.TempDir(), "absent"))

	t.Setenv("GARAM_DATABASE_URL", "")
	_, err := runRoot(t, "migrate")
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "got %v", err)
	assert.Contains(t, err.Error(), "GARAM_DATABASE_URL")

	t.Setenv("GARAM_DATABASE_URL", "postgres://garam@127.0.0.1:1/garam")
	_, err = runRoot(t, "migrate")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "GARAM_DATABASE_URL",
		"past the check the failure is another one")
}
