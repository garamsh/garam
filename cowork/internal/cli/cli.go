// Package cli is the cowork binary's command tree and its composition
// site: `serve` wires every port to its adapter here, and nowhere
// else (ADR-0127).
package cli

import (
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// The environment cowork reads.
const (
	envListenAddr    = "COWORK_LISTEN_ADDR"
	envDatabaseURL   = "COWORK_DATABASE_URL"
	envPlatformURL   = "COWORK_PLATFORM_URL"
	envWebOrigins    = "COWORK_WEB_ORIGINS"
	envMigrationsDir = "COWORK_MIGRATIONS_DIR"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// listOf splits a comma-separated value, dropping empty items.
func listOf(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// NewRootCommand is the `cowork` command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "cowork",
		Short:         "Pages, messages and tools people and agents share: the server and its client in one binary",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newServeCommand(), newMigrateCommand(), newProbeCommand())
	flags := &clientFlags{}
	client := []*cobra.Command{
		newLoginCommand(flags), newLogoutCommand(flags), newWhoamiCommand(flags), newWorkspacesCommand(flags),
		newPagesCommand(flags), newChannelsCommand(flags), newMessagesCommand(flags), newSearchCommand(flags),
		newMCPCommand(flags),
	}
	for _, cmd := range client {
		flags.bind(cmd)
	}
	root.AddCommand(client...)
	return root
}
