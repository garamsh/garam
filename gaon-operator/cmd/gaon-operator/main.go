// Command gaon-operator is the operator that renders garam's agents
// into gaon (ADR-0151). All wiring lives in internal/cli.
package main

import (
	"context"
	"os"

	"github.com/garamsh/garam/gaon-operator/internal/cli"
)

func main() {
	if err := cli.Run(context.Background(), os.Getenv, os.Stderr); err != nil {
		os.Exit(1)
	}
}
