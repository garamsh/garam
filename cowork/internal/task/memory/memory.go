// Package memory implements task.Repository in memory, for tests and
// for a server run without a database.
package memory

import (
	"context"
	"slices"
	"strings"
	"sync"

	"github.com/garamsh/garam/cowork/internal/task"
)

// Repository holds every organization's tasks behind one lock.
type Repository struct {
	mu       sync.Mutex
	tasks    map[string]task.Task      // by org + "/" + id
	comments map[string][]task.Comment // by org + "/" + task
}

var _ task.Repository = (*Repository)(nil)

// New answers an empty repository.
func New() *Repository {
	return &Repository{tasks: map[string]task.Task{}, comments: map[string][]task.Comment{}}
}

func key(org, id string) string { return org + "/" + id }

// copyOf answers t holding links of its own, so a caller changing them
// changes nothing stored.
func copyOf(t task.Task) task.Task {
	t.Pages, t.Messages = slices.Clone(t.Pages), slices.Clone(t.Messages)
	return t
}

// Create implements task.Repository.
func (r *Repository) Create(_ context.Context, t task.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tasks[key(t.Org, t.ID)] = copyOf(t)
	return nil
}

// Get implements task.Repository.
func (r *Repository) Get(_ context.Context, org, id string) (task.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.tasks[key(org, id)]
	if !ok {
		return task.Task{}, task.ErrTaskNotFound
	}
	return copyOf(t), nil
}

func matches(t task.Task, f task.Filter) bool {
	return (f.Status == "" || t.Status == f.Status) &&
		(f.Assignee.IsZero() || t.Assignee == f.Assignee) &&
		(f.Page == "" || slices.Contains(t.Pages, f.Page)) &&
		(f.Message == "" || slices.Contains(t.Messages, f.Message))
}

// List implements task.Repository.
func (r *Repository) List(_ context.Context, org string, f task.Filter, limit int) ([]task.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []task.Task
	for _, t := range r.tasks {
		if t.Org == org && matches(t, f) {
			out = append(out, copyOf(t))
		}
	}
	slices.SortFunc(out, func(a, b task.Task) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out[:min(limit, len(out))], nil
}

// Update implements task.Repository.
func (r *Repository) Update(_ context.Context, t task.Task) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tasks[key(t.Org, t.ID)]; !ok {
		return task.ErrTaskNotFound
	}
	r.tasks[key(t.Org, t.ID)] = copyOf(t)
	return nil
}

// AddComment implements task.Repository.
func (r *Repository) AddComment(_ context.Context, c task.Comment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tasks[key(c.Org, c.Task)]; !ok {
		return task.ErrTaskNotFound
	}
	k := key(c.Org, c.Task)
	r.comments[k] = append(r.comments[k], c)
	return nil
}

// Comments implements task.Repository.
func (r *Repository) Comments(_ context.Context, org, id string) ([]task.Comment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.comments[key(org, id)]), nil
}
