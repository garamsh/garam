// Command garam is the platform's server binary (ADR-0003, ADR-0132).
// All wiring lives in internal/cli; main only executes the command
// tree. A failure ends the process with 1, whichever command failed.
package main

import (
	"log/slog"
	"os"

	"github.com/garamsh/garam/platform/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		slog.Error("garam exited", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
