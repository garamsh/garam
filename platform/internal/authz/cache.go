package authz

import (
	"context"
	"sync"

	"github.com/garamsh/garam/lib/grn"
)

// loadedPolicy is one active policy's GRN, parsed statements and the
// subjects it is attached to. An empty subject set applies org-wide.
type loadedPolicy struct {
	grn        grn.GRN
	statements []Statement
	subjects   map[grn.GRN]bool
}

// reach reports whether the policy applies to any of subjects, and
// which of them it is attached to: none for an org-wide policy.
func (p loadedPolicy) reach(subjects []grn.GRN) ([]grn.GRN, bool) {
	if len(p.subjects) == 0 {
		return nil, true
	}
	var attached []grn.GRN
	for _, s := range subjects {
		if p.subjects[s] {
			attached = append(attached, s)
		}
	}
	return attached, len(attached) > 0
}

// PolicyCache is the per-org parsed policy cache (ADR-0002). Entries
// are filled on first use and dropped when the org's policies or their
// attachments change.
type PolicyCache struct {
	store Store
	mu    sync.RWMutex
	byOrg map[grn.GRN][]loadedPolicy
	// gen counts invalidations. A fill reads the store outside the
	// lock, so an invalidation can land while that read is in flight
	// and find no entry to drop; installing the snapshot afterwards
	// would put the org back with nothing left to drop it again. The
	// fill reads gen before the store and installs only if it has not
	// moved, which is what makes an invalidation raised at any point
	// during a fill take effect.
	gen uint64
}

// Compile-time check that PolicyCache satisfies PolicySource.
var _ PolicySource = (*PolicyCache)(nil)

// NewPolicyCache constructs a cache over store.
func NewPolicyCache(store Store) *PolicyCache {
	return &PolicyCache{store: store, byOrg: make(map[grn.GRN][]loadedPolicy)}
}

// Statements returns the statements the org's active policies grant
// subjects, loading and parsing the org once per cache generation.
func (c *PolicyCache) Statements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]Grant, error) {
	policies, err := c.policies(ctx, orgGRN)
	if err != nil {
		return nil, err
	}
	return statementsFor(policies, subjects), nil
}

// CurrentStatements returns what Statements returns, read from the
// store on every call. It neither reads nor fills the cache, so an
// invalidation that has not arrived cannot reach its answer.
func (c *PolicyCache) CurrentStatements(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]Grant, error) {
	policies, err := c.load(ctx, orgGRN)
	if err != nil {
		return nil, err
	}
	return statementsFor(policies, subjects), nil
}

// statementsFor returns the statements of the policies that apply to
// any of subjects, each with its policy and the subjects it reached.
func statementsFor(policies []loadedPolicy, subjects []grn.GRN) []Grant {
	var grants []Grant
	for _, p := range policies {
		attached, ok := p.reach(subjects)
		if !ok {
			continue
		}
		for _, st := range p.statements {
			grants = append(grants, Grant{Statement: st, Policy: p.grn, AttachedTo: attached})
		}
	}
	return grants
}

// policies returns the org's cached entry, filling it on a miss.
func (c *PolicyCache) policies(ctx context.Context, orgGRN grn.GRN) ([]loadedPolicy, error) {
	c.mu.RLock()
	cached, ok := c.byOrg[orgGRN]
	gen := c.gen
	c.mu.RUnlock()
	if ok {
		return cached, nil
	}
	policies, err := c.load(ctx, orgGRN)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	// A gen that has moved means an invalidation was raised after this
	// snapshot was read, so it is not installed. The caller is still
	// answered from it — it is what the store held when the request
	// arrived — and the next miss reads the store again. Counting
	// invalidations rather than tracking them per org costs an unrelated
	// org's fill a re-read and never an answer.
	if c.gen == gen {
		c.byOrg[orgGRN] = policies
	}
	c.mu.Unlock()
	return policies, nil
}

// load reads the org's active policies from the store.
func (c *PolicyCache) load(ctx context.Context, orgGRN grn.GRN) ([]loadedPolicy, error) {
	active, err := c.store.ActivePolicies(ctx, orgGRN)
	if err != nil {
		return nil, err
	}
	policies := make([]loadedPolicy, 0, len(active))
	for _, a := range active {
		subjects := make(map[grn.GRN]bool, len(a.Subjects))
		for _, s := range a.Subjects {
			subjects[s] = true
		}
		policies = append(policies, loadedPolicy{grn: a.GRN, statements: a.Document.Statement, subjects: subjects})
	}
	return policies, nil
}

// Invalidate drops the org's entry so the next check reloads it.
func (c *PolicyCache) Invalidate(orgGRN grn.GRN) {
	c.mu.Lock()
	delete(c.byOrg, orgGRN)
	c.gen++
	c.mu.Unlock()
}

// InvalidateAll drops every entry, so the next check of any org
// reloads it.
func (c *PolicyCache) InvalidateAll() {
	c.mu.Lock()
	clear(c.byOrg)
	c.gen++
	c.mu.Unlock()
}
