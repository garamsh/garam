package cli

import (
	"errors"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/internal/errorx"
)

// newMigrateCommand constructs `garam migrate`: apply all pending
// database migrations.
func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			databaseURL := envOr("GARAM_DATABASE_URL", "")
			if databaseURL == "" {
				return errorx.New(errorx.KindInvalidArgument, "GARAM_DATABASE_URL is required")
			}
			dir := envOr("GARAM_MIGRATIONS_DIR", "migrations")
			m, err := migrate.New("file://"+dir, databaseURL)
			if err != nil {
				return errorx.Wrap(err, "migrations")
			}
			defer func() { _, _ = m.Close() }()
			if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
				return errorx.Wrap(err, "migrations")
			}
			cmd.Println("migrations applied")
			return nil
		},
	}
}
