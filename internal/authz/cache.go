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

// PolicySource yields the statements an org's active policies grant a
// set of subjects. Check and Explain read through it, so the engine
// never parses a stored document on the hot path.
type PolicySource interface {
	// Statements returns the org-wide statements plus those of the
	// policies attached to any of subjects (ADR-0008).
	Statements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]Statement, error)
}

// cachedPolicy is one active policy's parsed statements and the
// subjects it is attached to. An empty subject set applies org-wide.
type cachedPolicy struct {
	statements []Statement
	subjects   map[grn.GRN]bool
}

// appliesTo reports whether the policy applies to any of subjects.
func (p cachedPolicy) appliesTo(subjects []grn.GRN) bool {
	if len(p.subjects) == 0 {
		return true
	}
	for _, s := range subjects {
		if p.subjects[s] {
			return true
		}
	}
	return false
}

// PolicyCache is the per-org parsed policy cache (ADR-0002). Entries
// are filled on first use and dropped when the org's policies or their
// attachments change.
type PolicyCache struct {
	store Store
	mu    sync.RWMutex
	byOrg map[grn.GRN][]cachedPolicy
}

// Compile-time check that PolicyCache satisfies PolicySource.
var _ PolicySource = (*PolicyCache)(nil)

// NewPolicyCache constructs a cache over store.
func NewPolicyCache(store Store) *PolicyCache {
	return &PolicyCache{store: store, byOrg: make(map[grn.GRN][]cachedPolicy)}
}

// Statements returns the statements the org's active policies grant
// subjects, loading and parsing the org once per cache generation.
func (c *PolicyCache) Statements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]Statement, error) {
	policies, err := c.policies(ctx, orgGRN)
	if err != nil {
		return nil, err
	}
	var statements []Statement
	for _, p := range policies {
		if p.appliesTo(subjects) {
			statements = append(statements, p.statements...)
		}
	}
	return statements, nil
}

// policies returns the org's cached entry, filling it on a miss.
func (c *PolicyCache) policies(ctx context.Context, orgGRN grn.GRN) ([]cachedPolicy, error) {
	c.mu.RLock()
	cached, ok := c.byOrg[orgGRN]
	c.mu.RUnlock()
	if ok {
		return cached, nil
	}
	active, err := c.store.ActivePolicies(ctx, orgGRN)
	if err != nil {
		return nil, err
	}
	policies := make([]cachedPolicy, 0, len(active))
	for _, a := range active {
		subjects := make(map[grn.GRN]bool, len(a.Subjects))
		for _, s := range a.Subjects {
			subjects[s] = true
		}
		policies = append(policies, cachedPolicy{statements: a.Document.Statement, subjects: subjects})
	}
	c.mu.Lock()
	c.byOrg[orgGRN] = policies
	c.mu.Unlock()
	return policies, nil
}

// Invalidate drops the org's entry so the next check reloads it.
func (c *PolicyCache) Invalidate(orgGRN grn.GRN) {
	c.mu.Lock()
	delete(c.byOrg, orgGRN)
	c.mu.Unlock()
}

// ListenPolicyChanges invalidates cache entries as policy and
// attachment writes arrive on PolicyChannel, until ctx is done. It
// covers writes made by other processes; writes made through this
// engine invalidate directly.
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
