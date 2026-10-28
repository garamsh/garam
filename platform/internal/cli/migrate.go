package cli

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/lib/errorx"
)

// newMigrateCommand constructs `garam migrate`: apply all pending
// database migrations.
func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			databaseURL := envOr(envDatabaseURL, "")
			if databaseURL == "" {
				return errorx.New(errorx.KindInvalidArgument, "GARAM_DATABASE_URL is required")
			}
			dir := envOr(envMigrationsDir, "migrations")
			// The source is opened here rather than inside migrate.New
			// so that a refusal can ask it which version precedes the
			// dirty one.
			src, err := source.Open("file://" + dir)
			if err != nil {
				return errorx.Wrap(err, "migrations")
			}
			m, err := migrate.NewWithSourceInstance("file", src, databaseURL)
			if err != nil {
				_ = src.Close()
				return errorx.Wrap(err, "migrations")
			}
			defer func() { _, _ = m.Close() }()

			err = m.Up()
			var dirty migrate.ErrDirty
			if errors.As(err, &dirty) {
				return errorx.New(errorx.KindFailedPrecondition, dirtyRefusal(src, dir, dirty.Version))
			}
			if err != nil && !errors.Is(err, migrate.ErrNoChange) {
				return errorx.Wrap(err, "migrations")
			}
			cmd.Println("migrations applied")
			return nil
		},
	}
}

// dirtyRefusal states what an operator has to do about the flag a failed
// migration left behind. golang-migrate's own message names neither the
// version to force nor whether the schema has to be read before forcing,
// and those are the two things the operator decides.
func dirtyRefusal(src source.Driver, dir string, version int) string {
	force := fmt.Sprintf("%s holds no migration before %d, so force the schema version to -1, the state "+
		"before any migration ran, once you have checked that %d is the first migration there",
		dir, version, version)
	if prev, ok := precedingVersion(src, version); ok {
		force = fmt.Sprintf("force the schema version to %d, the migration before %d in %s", prev, version, dir)
	}
	return fmt.Sprintf(`database is dirty at version %d: an earlier migrate run failed applying it, and this run applied nothing.

What failed is in that run's log and nowhere else; this message names the symptom. Recovery is a human step, and migrate never clears the flag itself — a run that cleared it would carry on against a schema nobody had checked. Read that log, fix migration %d, %s, and run migrate again.

Force only once the schema is known to be intact. Every migration this project ships is applied inside a transaction and leaves nothing behind when it fails; one that cannot be — CREATE INDEX CONCURRENTLY is the usual reason — can have applied part of itself, so read the schema against migration %d before forcing past it.`,
		version, version, force, version)
}

// precedingVersion returns the version to force back to after a failure
// at version, and whether dir holds one. It is not version-1: the
// sequence has gaps, and golang-migrate cannot resume from a version its
// source does not hold.
func precedingVersion(src source.Driver, version int) (uint, bool) {
	if version < 0 {
		return 0, false
	}
	prev, err := src.Prev(uint(version))
	return prev, err == nil
}
