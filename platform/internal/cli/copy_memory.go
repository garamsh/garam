package cli

import (
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/internal/workload"
)

// newCopyMemoryCommand constructs `garam copy-memory`: what the
// standalone supervisor's helper runs, in garam's image, to copy an
// agent's stored state to a new volume during a move (ADR-0089). It
// holds the source's writer lock for the whole copy, so a writer that
// has not stopped refuses it.
func newCopyMemoryCommand() *cobra.Command {
	var from, to string
	cmd := &cobra.Command{
		Use:   workload.CopyCommand,
		Short: "Copy a stopped agent's memory store to an empty directory, holding its writer lock",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := workload.CopyStore(from, to); err != nil {
				return err
			}
			cmd.Printf("copied the store under %s to %s\n", from, to)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "the directory holding the store, mounted read-only (required)")
	cmd.Flags().StringVar(&to, "to", "", "the empty directory the copy goes to (required)")
	_ = cmd.MarkFlagRequired("from")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}
