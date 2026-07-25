package authz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"

	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
)

// NewMemoryManager returns an in-memory [Manager] with its own
// changelog. Callers may inject a [RoleStore] for persistence.
func NewMemoryManager(roles RoleStore) Manager {
	return &memoryManager{
		roles:   roles,
		changes: make([]Change, 0),
		data:    make(map[string]any),
	}
}

type memoryManager struct {
	mu      sync.RWMutex
	roles   RoleStore
	changes []Change
	data    map[string]any
}

func (m *memoryManager) Grant(ctx context.Context, c Change) error {
	if err := validateChange(c); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if c.ID == "" {
		c.ID = newID()
	}
	if c.Timestamp.IsZero() {
		c.Timestamp = time.Now().UTC()
	}
	c.Action = ActionGrant
	m.changes = append(m.changes, c)

	if c.Role != "" {
		scope := c.Object
		if scope.IsZero() {
			scope = c.Subject
		}
		if err := m.roles.Assign(ctx, RoleAssignment{
			SubjectGRN: c.Subject,
			Role:       c.Role,
			Scope:      scope,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *memoryManager) Revoke(ctx context.Context, c Change) error {
	if err := validateChange(c); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if c.ID == "" {
		c.ID = newID()
	}
	if c.Timestamp.IsZero() {
		c.Timestamp = time.Now().UTC()
	}
	c.Action = ActionRevoke
	m.changes = append(m.changes, c)

	if c.Role != "" {
		scope := c.Object
		if scope.IsZero() {
			scope = c.Subject
		}
		if err := m.roles.Unassign(ctx, RoleAssignment{
			SubjectGRN: c.Subject,
			Role:       c.Role,
			Scope:      scope,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *memoryManager) ListChanges(_ context.Context, f ChangeFilter) ([]Change, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Change, 0, len(m.changes))
	for _, c := range m.changes {
		if !f.Subject.IsZero() && c.Subject != f.Subject {
			continue
		}
		if !f.Object.IsZero() && c.Object != f.Object {
			continue
		}
		if !f.Since.IsZero() && c.Timestamp.Before(f.Since) {
			continue
		}
		if !f.Until.IsZero() && c.Timestamp.After(f.Until) {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

// Explain records the grant chain for the subject-object pair. The
// Manager relies on the Evaluator to actually compute the final
// decision; the trace here is sourced from the changelog.
func (m *memoryManager) Explain(ctx context.Context, subject, object grn.GRN, action string) (Explanation, error) {
	changes, err := m.ListChanges(ctx, ChangeFilter{Subject: subject, Object: object, Limit: 100})
	if err != nil {
		return Explanation{}, err
	}
	traces := make([]DecisionTrace, 0, len(changes))
	allowed := false
	for _, c := range changes {
		traces = append(traces, DecisionTrace{
			PolicyID: c.PolicyID,
			Decision: policyActionToDecision(c.Action),
			Matched:  true,
			Reason:   c.Reason,
		})
		if c.Role == "admin" || c.Action == ActionGrant {
			allowed = true
		}
	}
	return Explanation{
		Subject: subject,
		Object:  object,
		Action:  action,
		Allowed: allowed,
		Traces:  traces,
	}, nil
}

func validateChange(c Change) error {
	if c.Subject.IsZero() {
		return errorx.New(errorx.KindInvalidArgument, "change: empty subject")
	}
	return nil
}

func policyActionToDecision(a ChangeAction) Decision {
	if a == ActionGrant {
		return DecisionAllow
	}
	return DecisionDeny
}

func newID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}
