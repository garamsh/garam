// Package memory implements blob.Store in memory, for tests and for a
// server run without an object store.
package memory

import (
	"context"
	"slices"
	"sync"

	"github.com/garamsh/garam/cowork/internal/blob"
)

// Store holds every blob behind one lock.
type Store struct {
	mu    sync.Mutex
	blobs map[string]blob.Blob
}

var _ blob.Store = (*Store)(nil)

// New answers an empty store.
func New() *Store { return &Store{blobs: map[string]blob.Blob{}} }

// Put implements blob.Store.
func (s *Store) Put(_ context.Context, key string, b blob.Blob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.blobs[key]; ok {
		return blob.ErrExists
	}
	s.blobs[key] = blob.Blob{ContentType: b.ContentType, Body: slices.Clone(b.Body)}
	return nil
}

// Get implements blob.Store.
func (s *Store) Get(_ context.Context, key string) (blob.Blob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.blobs[key]
	if !ok {
		return blob.Blob{}, blob.ErrNotFound
	}
	return blob.Blob{ContentType: b.ContentType, Body: slices.Clone(b.Body)}, nil
}

// Delete implements blob.Store.
func (s *Store) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.blobs, key)
	return nil
}
