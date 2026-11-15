package cli

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/cowork/internal/policy"
	"github.com/garamsh/garam/cowork/internal/policyfeed"
	feedgrpc "github.com/garamsh/garam/cowork/internal/policyfeed/grpc"
	feedpg "github.com/garamsh/garam/cowork/internal/policyfeed/postgres"
	"github.com/garamsh/garam/lib/errorx"
)

// The feed's pace: a pass every second while idle, and no longer than
// a minute between passes while the service is failing.
const (
	feedPoll       = time.Second
	feedMaxBackoff = time.Minute
)

// policyConfig is the policy service's settings as the environment
// gives them. cowork asks that service alone, so a setting missing is
// refused, and every one missing is named.
func policyConfig() (policy.Config, error) {
	settings := []struct{ key, value string }{
		{envPolicyAddr, envOr(envPolicyAddr, "")},
		{envPolicyCAFile, envOr(envPolicyCAFile, "")},
		{envMachineTokenURL, envOr(envMachineTokenURL, "")},
		{envDeploymentKeyFile, envOr(envDeploymentKeyFile, "")},
	}
	var missing []string
	for _, s := range settings {
		if s.value == "" {
			missing = append(missing, s.key)
		}
	}
	if len(missing) > 0 {
		return policy.Config{}, errorx.New(errorx.KindInvalidArgument,
			"cowork asks the policy service alone; set "+strings.Join(missing, ", "))
	}
	return policy.Config{
		Addr: settings[0].value, CAFile: settings[1].value, TokenURL: settings[2].value, KeyFile: settings[3].value,
	}, nil
}

// policyClient is the client for cfg.
func policyClient(cfg policy.Config) (*policy.Client, error) {
	tokens, err := policy.NewTokens(cfg, time.Now)
	if err != nil {
		return nil, err
	}
	return policy.Dial(cfg, tokens)
}

// runFeed delivers the feed until ctx ends, and returns the gate that
// holds a request until its entity is delivered. It idles while the
// feed is off.
func runFeed(ctx context.Context, pool *pgxpool.Pool, client *policy.Client) policyfeed.Gate {
	store := feedpg.NewStore(pool)
	svc := policyfeed.NewService(store, feedgrpc.NewSender(client), slog.Default(),
		policyfeed.Config{Poll: feedPoll, MaxBackoff: feedMaxBackoff})
	go func() { _ = svc.Run(ctx) }()
	return policyfeed.NewGate(store, svc, policyfeed.Bound)
}

// newPolicyFeedCommand constructs `cowork policy-feed start|stop`.
func newPolicyFeedCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy-feed",
		Short: "Switch the feed of cowork's entities to the policy service on or off",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "start",
			Short: "Mark every entity and deliver changes to the policy service from now on",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return withSwitch(cmd, func(ctx context.Context, sw policyfeed.Switch) error {
					marked, err := sw.Start(ctx)
					if err != nil {
						return err
					}
					cmd.Printf("policy feed on: %d entities marked\n", marked)
					return nil
				})
			},
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop marking changes and drop the marks not yet delivered",
			RunE: func(cmd *cobra.Command, _ []string) error {
				return withSwitch(cmd, func(ctx context.Context, sw policyfeed.Switch) error {
					removed, err := sw.Stop(ctx)
					if err != nil {
						return err
					}
					cmd.Printf("policy feed off: %d marks removed\n", removed)
					return nil
				})
			},
		},
	)
	return cmd
}

func withSwitch(cmd *cobra.Command, run func(context.Context, policyfeed.Switch) error) error {
	url := envOr(envDatabaseURL, "")
	if url == "" {
		return errorx.New(errorx.KindInvalidArgument, envDatabaseURL+" is required")
	}
	pool, err := pgxpool.New(cmd.Context(), url)
	if err != nil {
		return errorx.Wrap(err, "database", errorx.KindInvalidArgument)
	}
	defer pool.Close()
	return run(cmd.Context(), feedpg.NewSwitch(pool))
}
