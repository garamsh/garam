package cli

import (
	"errors"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/spf13/cobra"

	"github.com/garamsh/gaon/internal/errorx"
)

// newMigrateCommand constructs `gaon migrate`: apply all pending
// database migrations.
func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Apply pending database migrations",
		RunE: func(cmd *cobra.Command, _ []string) error {
			databaseURL := envOr("GAON_DATABASE_URL", "")
			if databaseURL == "" {
				return errorx.New(errorx.KindInvalidArgument, "GAON_DATABASE_URL is required")
			}
			dir := envOr("GAON_MIGRATIONS_DIR", "migrations")
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
