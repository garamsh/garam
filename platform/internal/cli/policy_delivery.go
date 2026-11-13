package cli

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/garamsh/garam/lib/errorx"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	policyfeedgrpc "github.com/garamsh/garam/platform/internal/policyfeed/grpc"
	policyfeedpg "github.com/garamsh/garam/platform/internal/policyfeed/postgres"
	"github.com/garamsh/garam/platform/internal/signer"
)

// policyDeliveryConfig is what GARAM_POLICY_* name.
type policyDeliveryConfig struct {
	addr, caFile, keyFile, issuer string
}

// readPolicyDeliveryConfig reads the settings. It returns nil when
// GARAM_POLICY_ADDR is unset, which is a deployment with no policy
// service (ADR-0140 §8), and refuses an address without the rest: a
// connection that cannot be verified or authenticated is not one to
// open.
func readPolicyDeliveryConfig() (*policyDeliveryConfig, error) {
	c := policyDeliveryConfig{
		addr:    envOr(envPolicyAddr, ""),
		caFile:  envOr(envPolicyCAFile, ""),
		keyFile: envOr(envPolicySigningKeyFile, ""),
		issuer:  envOr(envPolicyTokenIssuer, ""),
	}
	if c.addr == "" {
		return nil, nil
	}
	for _, req := range []struct {
		name  envName
		value string
	}{
		{envPolicyCAFile, c.caFile},
		{envPolicySigningKeyFile, c.keyFile},
		{envPolicyTokenIssuer, c.issuer},
	} {
		if req.value == "" {
			return nil, errorx.New(errorx.KindInvalidArgument,
				string(req.name)+" is required when "+string(envPolicyAddr)+" is set")
		}
	}
	return &c, nil
}

// Pace of the delivery loop: an idle pass is cheap, and a failing one
// backs off to a cap.
const (
	policyPoll       = time.Second
	policyMaxBackoff = 30 * time.Second
)

// startPolicyDelivery starts the delivery loop when a policy service
// is configured, and returns the gate a membership change waits at and
// what stops the loop and waits for it. With none configured it starts
// nothing and returns no gate, so no request waits for a delivery that
// never comes, and logs an error when the feed is on and so nothing
// delivers it (ADR-0140 §9).
func startPolicyDelivery(
	ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, registerer prometheus.Registerer,
) (policyfeed.Gate, func(), error) {
	cfg, err := readPolicyDeliveryConfig()
	if err != nil {
		return nil, nil, err
	}
	store := policyfeedpg.NewStore(pool)
	if cfg == nil {
		warnIfFeedUndelivered(ctx, policyfeedpg.New(pool), logger)
		// Nothing delivers, so the outbox is all the standing there is.
		if err := policyfeed.NewCollector(registerer, store, nil); err != nil {
			return nil, nil, err
		}
		return nil, func() {}, nil
	}
	key, err := signer.LoadKey(cfg.keyFile)
	if err != nil {
		return nil, nil, errorx.Wrap(err, string(envPolicySigningKeyFile))
	}
	conn, err := policyfeedgrpc.Dial(cfg.addr, cfg.caFile, cfg.issuer, key)
	if err != nil {
		return nil, nil, err
	}
	service := policyfeed.NewService(
		store,
		policyfeedgrpc.NewSender(policyv1.NewPolicyServiceClient(conn)),
		logger,
		policyfeed.Config{Poll: policyPoll, MaxBackoff: policyMaxBackoff},
	)
	if err := policyfeed.NewCollector(registerer, store, service.LastPass); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = service.Run(runCtx)
	}()
	logger.Info("delivering the policy feed", slog.String("addr", cfg.addr))
	return policyfeed.NewGate(store, service, policyfeed.Bound), func() {
		cancel()
		<-done
		_ = conn.Close()
	}, nil
}

func warnIfFeedUndelivered(ctx context.Context, sw policyfeed.Switch, logger *slog.Logger) {
	enabled, err := sw.Enabled(ctx)
	if err != nil {
		logger.Error("could not read whether the policy feed is on", slog.String("error", err.Error()))
		return
	}
	if enabled {
		logger.Error("the policy feed is on and " + string(envPolicyAddr) +
			" is unset, so nothing delivers it and the outbox grows with every write")
	}
}
