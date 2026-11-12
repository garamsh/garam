package cli

import (
	"context"
	"log/slog"
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
// gives them. An address without the rest is refused, and none set is
// the zero Config, which leaves cowork asking the platform.
func policyConfig() (policy.Config, error) {
	cfg := policy.Config{
		Addr:     envOr(envPolicyAddr, ""),
		CAFile:   envOr(envPolicyCAFile, ""),
		TokenURL: envOr(envMachineTokenURL, ""),
		KeyFile:  envOr(envDeploymentKeyFile, ""),
	}
	return cfg, cfg.Validate()
}

// policyClient is the client for cfg, nil where cfg sets nothing.
func policyClient(cfg policy.Config) (*policy.Client, error) {
	if !cfg.Configured() {
		return nil, nil
	}
	tokens, err := policy.NewTokens(cfg, time.Now)
	if err != nil {
		return nil, err
	}
	return policy.Dial(cfg, tokens)
}

// runFeed delivers the feed until ctx ends. It idles while the feed is
// off, and says so once at start where the feed is on and nothing
// delivers it.
func runFeed(ctx context.Context, pool *pgxpool.Pool, client *policy.Client) {
	if client == nil {
		warnIfFeedUndelivered(ctx, feedpg.NewSwitch(pool))
		return
	}
	svc := policyfeed.NewService(feedpg.NewStore(pool), feedgrpc.NewSender(client), slog.Default(),
		policyfeed.Config{Poll: feedPoll, MaxBackoff: feedMaxBackoff})
	go func() { _ = svc.Run(ctx) }()
}

// warnIfFeedUndelivered logs that the outbox is filling with nothing to
// deliver it, which happens where the feed was started and the policy
// settings were then removed.
func warnIfFeedUndelivered(ctx context.Context, sw policyfeed.Switch) {
	on, err := sw.Enabled(ctx)
	if err != nil || !on {
		return
	}
	slog.WarnContext(ctx, "cowork: the policy feed is on and no policy service is configured; "+
		"the outbox grows until it is delivered or stopped",
		slog.String("set", envPolicyAddr), slog.String("stop", "cowork policy-feed stop"))
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
