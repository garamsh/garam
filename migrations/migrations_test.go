// Package migrations_test guards the schema's source of truth. It needs
// no database, so `make ci` reports a missing or emptied migrations
// directory as itself, rather than leaving it to surface as a migration
// failure in every test of the e2e suite.
package migrations_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrationsDir_HasUpMigrations counts the `.up.sql` files golang-migrate
// applies. A bare count of directory entries would also count this file, and
// so would pass with every migration deleted.
func TestMigrationsDir_HasUpMigrations(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	var up []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			up = append(up, e.Name())
		}
	}
	assert.NotEmpty(t, up, "no .up.sql migrations beside this test")
}
