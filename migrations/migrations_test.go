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

// TestMigrationsDir_EveryMigrationCarriesAUniqueVersion holds two things
// about the same parse. That every `.sql` file beside this one carries a
// version golang-migrate can read: a name it cannot parse is a name it
// skips, so the migration is never applied and its counterpart is
// orphaned, and the suffix count above passes on it. And that no two
// share a version and a direction: golang-migrate refuses the repeat
// while opening the source, so a collision applies no migration at all
// rather than one of the two, and the count passes either way — two files
// carrying one version between them are still two files.
func TestMigrationsDir_EveryMigrationCarriesAUniqueVersion(t *testing.T) {
	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	type key struct {
		version   uint
		direction source.Direction
	}
	seen := make(map[key]string)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		// The parse golang-migrate applies, which reads the version as a
		// number: `12_a` and `000012_b` collide here as they collide there.
		m, err := source.DefaultParse(e.Name())
		if !assert.NoErrorf(t, err, "%s carries no version golang-migrate can read", e.Name()) {
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
	assert.NotEmpty(t, seen, "no migration beside this test carries a version")
}
