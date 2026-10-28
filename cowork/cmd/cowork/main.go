// Command cowork is cowork's single binary (ADR-0127). All wiring
// lives in internal/cli; main only executes the command tree.
package main

import (
	"log/slog"
	"os"

	"github.com/garamsh/garam/cowork/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		slog.Error("cowork exited", slog.String("error", err.Error()))
		os.Exit(cli.ExitCode(err))
	}
}
