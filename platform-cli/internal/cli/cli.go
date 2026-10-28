// Package cli owns the cobra command tree for the garam client binary
// (ADR-0120, ADR-0132): the user commands a person signs in and reads
// garam's objects with — `login`, `logout`, `whoami`, `orgs`,
// `operators`, `agents` — and the commands derived from the browser
// API (ADR-0122). They reach the platform through lib/platform-sdk-go
// alone. Commands are constructed here; cmd/garam only executes the
// root.
//
// Configuration is via environment variables:
//
//	GARAM_ENDPOINT         the garam a user command talks to (default
//	                       "https://garam.sh"; --endpoint overrides it)
//	XDG_CONFIG_HOME        where a user command keeps its credentials
//	                       file, under garam/ (default ~/.config)
package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// NewRootCommand constructs the garam client's command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "garam",
		Short: "garam is the workflow management core: identity, orgs, authorization",
		// cmd/garam prints what Execute returns, so cobra printing it
		// as well is the same line twice. The caller's is the one kept,
		// because it goes through the logger the process already writes
		// its other lines with.
		SilenceErrors: true,
	}
	for _, user := range []*cobra.Command{
		newLoginCommand(), newLogoutCommand(), newWhoamiCommand(),
		newOrgsCommand(), newAgentsCommand(),
	} {
		root.AddCommand(user)
	}
	addAPICommands(root, apiOperations)
	for _, name := range userCommandNames(root) {
		for _, c := range root.Commands() {
			if c.Name() == name {
				markUserCommands(c)
			}
		}
	}
	silenceUsageOnRun(root)
	return root
}

// silenceUsageOnRun stops a command that fails while running from
// printing its usage block, and leaves the block on one that fails
// before it runs: "how is this invoked" answers a bad flag rather than
// a refusal from the API.
//
// cobra runs ValidateRequiredFlags after every hook, so the line goes
// inside RunE: from a hook it would silence a command missing a
// required flag, which is a usage error and keeps its block.
func silenceUsageOnRun(cmd *cobra.Command) {
	for _, child := range cmd.Commands() {
		silenceUsageOnRun(child)
	}
	run := cmd.RunE
	if run == nil {
		return
	}
	cmd.RunE = func(c *cobra.Command, args []string) error {
		c.SilenceUsage = true
		return run(c, args)
	}
}

// envName is the name of an environment variable the client reads.
// Every name it reads is declared below and reaches os.Getenv from
// there.
type envName string

// The environment the client reads.
const (
	envEndpoint      envName = "GARAM_ENDPOINT"
	envXDGConfigHome envName = "XDG_CONFIG_HOME"
)

// envOr reads a declared name, or the fallback when it is unset or
// empty. It holds the module's only os.Getenv call.
func envOr(key envName, fallback string) string {
	if v := os.Getenv(string(key)); v != "" {
		return v
	}
	return fallback
}
