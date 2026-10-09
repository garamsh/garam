package cli

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/message"
)

// newRetireMessageSigningKeyCommand constructs `garam
// retire-message-signing-key <kid>`: how a person retires a message
// signing key at once, which is how a compromised key is withdrawn
// (ADR-0101). The key stops signing and leaves the published set
// immediately; the next hand-out signs under a new key from that moment.
// It is a sibling of `serve`, as `issue-certificate` is, so no serving
// process withdraws a key. It needs no key-encryption key, because it
// opens nothing.
func newRetireMessageSigningKeyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "retire-message-signing-key <kid>",
		Short: "Stop a message signing key signing and remove it from the published set at once",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return retireMessageSigningKey(cmd, message.KeyID(args[0]))
		},
	}
}

func retireMessageSigningKey(cmd *cobra.Command, id message.KeyID) error {
	ctx := cmd.Context()
	databaseURL := envOr(envDatabaseURL, "")
	if databaseURL == "" {
		return errorx.New(errorx.KindInvalidArgument, "GARAM_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return errorx.Wrap(err, "postgres", errorx.KindUnavailable)
	}
	defer pool.Close()
	if err := message.NewPostgresStore(pool).WithdrawSigningKey(ctx, id); err != nil {
		return err
	}
	cmd.Println("retired message signing key " + string(id))
	return nil
}
