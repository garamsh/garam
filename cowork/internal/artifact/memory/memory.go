// Package memory implements artifact.Repository in memory, for tests
// and for a server run without a database.
package memory

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/garamsh/garam/cowork/internal/artifact"
)

// Repository holds every organization's artifacts behind one lock.
type Repository struct {
	mu        sync.Mutex
	artifacts map[string]artifact.Artifact  // by org + "/" + id
	versions  map[string][]artifact.Version // by org + "/" + id, oldest first
}

var _ artifact.Repository = (*Repository)(nil)

// New answers an empty repository.
func New() *Repository {
	return &Repository{artifacts: map[string]artifact.Artifact{}, versions: map[string][]artifact.Version{}}
}

func key(org, id string) string { return org + "/" + id }

// Create implements artifact.Repository.
func (r *Repository) Create(_ context.Context, a artifact.Artifact, v artifact.Version) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.artifacts[key(a.Org, a.ID)] = a
	r.versions[key(a.Org, a.ID)] = []artifact.Version{v}
	return nil
}

// AddVersion implements artifact.Repository.
func (r *Repository) AddVersion(_ context.Context, org string, v artifact.Version) (artifact.Artifact, artifact.Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	k := key(org, v.Artifact)
	a, ok := r.artifacts[k]
	if !ok {
		return artifact.Artifact{}, artifact.Version{}, artifact.ErrArtifactNotFound
	}
	a.Latest++
	a.UpdatedAt = v.CreatedAt
	v.Number = a.Latest
	r.artifacts[k] = a
	r.versions[k] = append(r.versions[k], v)
	return a, v, nil
}

// Get implements artifact.Repository.
func (r *Repository) Get(_ context.Context, org, id string) (artifact.Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.artifacts[key(org, id)]
	if !ok {
		return artifact.Artifact{}, artifact.ErrArtifactNotFound
	}
	return a, nil
}

// List implements artifact.Repository.
func (r *Repository) List(_ context.Context, org string, limit int) ([]artifact.Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []artifact.Artifact
	for _, a := range r.artifacts {
		if a.Org == org {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b artifact.Artifact) int {
		if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out[:min(limit, len(out))], nil
}

// Versions implements artifact.Repository.
func (r *Repository) Versions(_ context.Context, org, id string) ([]artifact.Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.versions[key(org, id)]), nil
}

// Version implements artifact.Repository.
func (r *Repository) Version(_ context.Context, org, id string, number int) (artifact.Version, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	vs := r.versions[key(org, id)]
	if number < 1 || number > len(vs) {
		return artifact.Version{}, artifact.ErrVersionNotFound
	}
	return vs[number-1], nil
}
