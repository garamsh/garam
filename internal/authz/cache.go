package authz

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
)

// PolicyChannel is the Postgres LISTEN/NOTIFY channel on which policy
// writes announce the org whose cached statements went stale.
const PolicyChannel = "gaon_policy_change"

// PolicySource yields the statements an org's active policies grant.
// Check and Explain read through it, so the engine never parses a
// stored document on the hot path.
type PolicySource interface {
	OrgStatements(ctx context.Context, orgGRN grn.GRN) ([]Statement, error)
}

// PolicyCache is the per-org parsed statement cache (ADR-0002). Entries
// are filled on first use and dropped when the org's policies change.
type PolicyCache struct {
	store Store
	mu    sync.RWMutex
	byOrg map[grn.GRN][]Statement
}

// Compile-time check that PolicyCache satisfies PolicySource.
var _ PolicySource = (*PolicyCache)(nil)

// NewPolicyCache constructs a cache over store.
func NewPolicyCache(store Store) *PolicyCache {
	return &PolicyCache{store: store, byOrg: make(map[grn.GRN][]Statement)}
}

// OrgStatements returns the org's active statements, loading and
// parsing them once per cache generation.
func (c *PolicyCache) OrgStatements(ctx context.Context, orgGRN grn.GRN) ([]Statement, error) {
	c.mu.RLock()
	cached, ok := c.byOrg[orgGRN]
	c.mu.RUnlock()
	if ok {
		return cached, nil
	}
	docs, err := c.store.ActiveDocuments(ctx, orgGRN)
	if err != nil {
		return nil, err
	}
	var statements []Statement
	for _, doc := range docs {
		statements = append(statements, doc.Statement...)
	}
	c.mu.Lock()
	c.byOrg[orgGRN] = statements
	c.mu.Unlock()
	return statements, nil
}

// Invalidate drops the org's entry so the next check reloads it.
func (c *PolicyCache) Invalidate(orgGRN grn.GRN) {
	c.mu.Lock()
	delete(c.byOrg, orgGRN)
	c.mu.Unlock()
}

// ListenPolicyChanges invalidates cache entries as policy writes arrive
// on PolicyChannel, until ctx is done. It covers writes made by other
// processes; writes made through this engine invalidate directly.
func ListenPolicyChanges(ctx context.Context, pool *pgxpool.Pool, cache *PolicyCache) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return errorx.Wrap(err, "policy listener: acquire", errorx.KindUnavailable)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+PolicyChannel); err != nil {
		return errorx.Wrap(err, "policy listener: listen", errorx.KindUnavailable)
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return errorx.Wrap(err, "policy listener: wait", errorx.KindUnavailable)
		}
		orgGRN, err := grn.Parse(n.Payload)
		if err != nil {
			return errorx.Wrap(err, "policy listener: payload")
		}
		cache.Invalidate(orgGRN)
	}
}
