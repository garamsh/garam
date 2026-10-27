package cli

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/platform/sdk/errorx"
)

// newMigrateCommand constructs `cowork migrate`: apply every pending
// migration to cowork's own database.
func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			url := envOr(envDatabaseURL, "")
			if url == "" {
				return errorx.New(errorx.KindInvalidArgument, envDatabaseURL+" is required")
			}
			if err := Migrate(url, envOr(envMigrationsDir, "migrations")); err != nil {
				return err
			}
			cmd.Println("migrations applied")
			return nil
		},
	}
}

// Migrate applies every pending migration in dir to the database at
// url. A database a failed run left dirty is refused, and recovering it
// is a person's step: the message names the version to force to.
func Migrate(url, dir string) error {
	m, err := migrate.New("file://"+dir, url)
	if err != nil {
		return errorx.Wrap(err, "migrations", errorx.KindUnavailable)
	}
	defer func() { _, _ = m.Close() }()
	err = m.Up()
	var dirty migrate.ErrDirty
	switch {
	case err == nil, errors.Is(err, migrate.ErrNoChange):
		return nil
	case errors.As(err, &dirty):
		return errorx.New(errorx.KindFailedPrecondition, fmt.Sprintf(
			"database is dirty at version %d: a migrate run failed applying it and this run applied nothing. "+
				"Read that run's log, fix the migration, force the version to the one before it, and run migrate again",
			dirty.Version))
	}
	return errorx.Wrap(err, "migrations", errorx.KindUnavailable)
}
