// Command garam is the single deliverable binary (ADR-0003). All
// wiring lives in internal/cli; main only executes the command tree.
package main

import (
	"log/slog"
	"os"

	"github.com/garamsh/garam/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		slog.Error("garam exited", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
