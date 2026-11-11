package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/platform/internal/policyfeed"
	policyfeedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
)

// newPolicyFeedCommand constructs `garam policy-feed start|stop`: how a
// deployment turns on the feed of the policy service and turns it off
// (ADR-0140 §10). They are siblings of `serve`, so no serving process
// switches the feed.
func newPolicyFeedCommand() *cobra.Command {
	feed := &cobra.Command{
		Use:   "policy-feed",
		Short: "Turn the feed of the policy service on or off",
	}
	feed.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Enable the feed and mark every live entity",
			Long: "Enable the feed and mark every live entity.\n\n" +
				"It locks the source tables against writes while it runs, " +
				"so run it at low traffic. A write that Postgres aborts " +
				"for a deadlock with it is rolled back whole; repeat that " +
				"write, or run the command again if it was the one aborted.",
			Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return switchFeed(cmd, func(s policyfeed.Switch) (string, error) {
					marked, err := s.Start(cmd.Context())
					return fmt.Sprintf("policy feed started: %d entities marked", marked), err
				})
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Disable the feed and empty the outbox",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return switchFeed(cmd, func(s policyfeed.Switch) (string, error) {
					removed, err := s.Stop(cmd.Context())
					return fmt.Sprintf("policy feed stopped: %d marks removed", removed), err
				})
			},
		},
	)
	return feed
}

func switchFeed(cmd *cobra.Command, do func(policyfeed.Switch) (string, error)) error {
	pool, err := openPool(cmd.Context())
	if err != nil {
		return err
	}
	defer pool.Close()
	line, err := do(policyfeedpg.New(pool))
	if err != nil {
		return err
	}
	cmd.Println(line)
	return nil
}
