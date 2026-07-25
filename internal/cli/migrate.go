package cli

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/spf13/cobra"
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
				return errors.New("GAON_DATABASE_URL is required")
			}
			dir := envOr("GAON_MIGRATIONS_DIR", "migrations")
			m, err := migrate.New("file://"+dir, databaseURL)
			if err != nil {
				return fmt.Errorf("migrations: %w", err)
			}
			defer func() { _, _ = m.Close() }()
			if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
				return fmt.Errorf("migrations: %w", err)
			}
			cmd.Println("migrations applied")
			return nil
		},
	}
}
