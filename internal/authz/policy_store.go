package authz

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/garamsh/gaon/internal/errorx"
)

type memoryPolicyStore struct {
	mu       sync.RWMutex
	policies map[PolicyID]storedPolicy
}

type storedPolicy struct {
	description string
	source      string
}

// NewMemoryPolicyStore returns an in-memory [PolicyStore]. Callers
// seed policies via [MemoryPolicyStore.Put] before evaluating.
func NewMemoryPolicyStore() *MemoryPolicyStore {
	return &MemoryPolicyStore{
		Store: &memoryPolicyStore{policies: make(map[PolicyID]storedPolicy)},
	}
}

// MemoryPolicyStore is a [PolicyStore] with an additional Put method
// for seeding policies from tests or wire-up code.
type MemoryPolicyStore struct {
	Store PolicyStore
}

// Put adds or replaces a policy.
func (s *MemoryPolicyStore) Put(id PolicyID, description, source string) {
	inner := s.Store.(*memoryPolicyStore)
	inner.mu.Lock()
	defer inner.mu.Unlock()
	inner.policies[id] = storedPolicy{description: description, source: source}
}

func (s *memoryPolicyStore) Get(_ context.Context, id PolicyID) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.policies[id]
	if !ok {
		return "", errorx.New(errorx.KindNotFound, fmt.Sprintf("policy %q not found", id))
	}
	return p.source, nil
}

func (s *memoryPolicyStore) List(_ context.Context) ([]Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Policy, 0, len(s.policies))
	for id, p := range s.policies {
		out = append(out, Policy{ID: id, Description: p.description})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
