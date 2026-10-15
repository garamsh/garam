package cli

import (
	"encoding/json"

	"github.com/spf13/cobra"

	agentpg "github.com/garamsh/garam/internal/agent/postgres"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// newCutoverInventoryCommand constructs `garam cutover-inventory --org
// <slug>`: a read of every source record an organization's cutover
// accounts for, one JSON line each, closed by a line counting them
// (ADR-0086). It writes nothing and prints no value, only a SHA-256 of
// each. Like issue-certificate, it reaches the one database its own
// configuration names and takes no other.
func newCutoverInventoryCommand() *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "cutover-inventory",
		Short: "Print what an organization's agent cutover accounts for, without values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cutoverInventory(cmd, slug)
		},
	}
	cmd.Flags().StringVar(&slug, "org", "", "slug of the organization to inventory (required)")
	_ = cmd.MarkFlagRequired("org")
	return cmd
}

func cutoverInventory(cmd *cobra.Command, slug string) error {
	ctx := cmd.Context()
	orgGRN := grn.Org(slug)
	if err := orgGRN.Validate(); err != nil {
		return errorx.Wrap(err, "org", errorx.KindInvalidArgument)
	}
	pool, err := openPool(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	// An organization that does not exist holds nothing, and an empty
	// inventory of it would read as one that holds no agent.
	if _, err := org.NewPostgresStore(pool).GetOrg(ctx, orgGRN); err != nil {
		return err
	}
	records, summary, err := agentpg.New(pool).Inventory(ctx, orgGRN)
	if err != nil {
		return err
	}
	out := json.NewEncoder(cmd.OutOrStdout())
	for _, r := range records {
		if err := out.Encode(r); err != nil {
			return errorx.Wrap(err, "write inventory")
		}
	}
	if err := out.Encode(summary); err != nil {
		return errorx.Wrap(err, "write inventory")
	}
	return nil
}
