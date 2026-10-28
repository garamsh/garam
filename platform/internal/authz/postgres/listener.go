package postgres

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/authz"
)

// PolicyChannel is the Postgres LISTEN/NOTIFY channel on which policy
// writes announce the org whose cached statements went stale.
const PolicyChannel = "garam_policy_change"

// How long the listener waits before reopening a connection: the first
// attempt after a failure waits policyReconnectMin, doubling up to
// policyReconnectMax while attempts keep failing, and returning to the
// minimum once one connects. The minimum is short because the cache is
// stale until it does, and the maximum is what keeps a database that is
// down from being retried into an outage of its own.
const (
	policyReconnectMin = 100 * time.Millisecond
	policyReconnectMax = 30 * time.Second
)

// ListenPolicyChanges invalidates cache entries as policy and
// attachment writes arrive on PolicyChannel, until ctx is done. It
// covers writes made by other processes; writes made through this
// engine invalidate directly. Every failure — a Postgres failover
// breaking the connection, a payload nobody can parse — is answered by
// reconnecting, and only ctx ends this.
func ListenPolicyChanges(ctx context.Context, pool *pgxpool.Pool, cache *authz.PolicyCache) error {
	delay := policyReconnectMin
	for {
		connected, err := receivePolicyChanges(ctx, pool, cache)
		if ctx.Err() != nil {
			return errorx.Wrap(err, "policy listener: stopped", errorx.KindUnavailable)
		}
		if connected {
			delay = policyReconnectMin
		}
		slog.WarnContext(ctx, "policy listener reconnecting",
			slog.String("error", err.Error()),
			slog.Duration("in", delay))
		select {
		case <-ctx.Done():
			return errorx.Wrap(ctx.Err(), "policy listener: stopped", errorx.KindUnavailable)
		case <-time.After(delay):
		}
		delay = min(delay*2, policyReconnectMax)
	}
}

// receivePolicyChanges holds one connection for as long as it lasts,
// and reports whether it was established — which is what tells a
// database that is down from one that dropped a live connection.
//
// A notification raised while no connection was open is gone, so every
// org cached before this one registered LISTEN is dropped: what the
// engine read before the break is what it cannot know is current.
func receivePolicyChanges(ctx context.Context, pool *pgxpool.Pool, cache *authz.PolicyCache) (bool, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return false, errorx.Wrap(err, "policy listener: acquire", errorx.KindUnavailable)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+PolicyChannel); err != nil {
		return false, errorx.Wrap(err, "policy listener: listen", errorx.KindUnavailable)
	}
	cache.InvalidateAll()
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return true, errorx.Wrap(err, "policy listener: wait", errorx.KindUnavailable)
		}
		orgGRN, err := grn.Parse(n.Payload)
		if err != nil {
			return true, errorx.Wrap(err, "policy listener: payload")
		}
		cache.Invalidate(orgGRN)
	}
}
