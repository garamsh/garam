// Package migrations_test guards the schema's source of truth. It needs
// no database, so `make ci` reports a missing or emptied migrations
// directory as itself, rather than leaving it to surface as a migration
// failure in every test of the e2e suite.
package migrations_test

import (
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4/source"
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

// TestMigrationsDir_VersionsAreUnique holds that no two migrations share a
// version and a direction. golang-migrate refuses the repeat while opening
// the source, so a collision applies no migration at all rather than one of
// the two, and the count above passes either way — two files carrying one
// version between them are still two files.
func TestMigrationsDir_VersionsAreUnique(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	type key struct {
		version   uint
		direction source.Direction
	}
	seen := make(map[key]string)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		// The parse golang-migrate applies, which reads the version as a
		// number: `12_a` and `000012_b` collide here as they collide there.
		// A name it cannot parse is a name it skips, this file included.
		m, err := source.DefaultParse(e.Name())
		if err != nil {
			continue
		}
		k := key{version: m.Version, direction: m.Direction}
		if first, dup := seen[k]; dup {
			assert.Failf(t, "two migrations share a version",
				"%s and %s are both version %d %s", first, e.Name(), m.Version, m.Direction)
			continue
		}
		seen[k] = e.Name()
	}
}
