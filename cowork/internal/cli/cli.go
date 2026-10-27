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

	// Where artifact versions are kept (ADR-0127 §2): s3 or memory.
	envBlobStore      = "COWORK_BLOB_STORE"
	envS3Endpoint     = "COWORK_S3_ENDPOINT"
	envS3Region       = "COWORK_S3_REGION"
	envS3Bucket       = "COWORK_S3_BUCKET"
	envS3AccessKey    = "COWORK_S3_ACCESS_KEY"
	envS3SecretKey    = "COWORK_S3_SECRET_KEY"
	envS3PathStyle    = "COWORK_S3_PATH_STYLE"
	envS3CreateBucket = "COWORK_S3_CREATE_BUCKET"

	// The user-content origin HTML artifacts are served from (§8).
	envUserContentOrigin = "COWORK_USERCONTENT_ORIGIN"
	envUserContentAddr   = "COWORK_USERCONTENT_ADDR"
	envUserContentSecret = "COWORK_USERCONTENT_SECRET"
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
		newPagesCommand(flags), newChannelsCommand(flags), newMessagesCommand(flags), newTasksCommand(flags),
		newSearchCommand(flags),
		newMCPCommand(flags),
	}
	for _, cmd := range client {
		flags.bind(cmd)
	}
	root.AddCommand(client...)
	return root
}
