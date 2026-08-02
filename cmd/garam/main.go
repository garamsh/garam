// Command garam is the single deliverable binary (ADR-0003). All
// wiring lives in internal/cli; main only executes the command tree.
package main

import (
	"log"

	"github.com/garamsh/garam/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		log.Fatal(err)
	}
}
