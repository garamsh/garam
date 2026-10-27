// Package memory implements page.Repository in memory, for tests and
// for a server run without a database.
package memory

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/garamsh/garam/cowork/internal/page"
)

// Repository holds every organization's pages behind one lock.
type Repository struct {
	mu    sync.Mutex
	pages map[string]page.Page // by org + "/" + id
	now   func() time.Time
}

var _ page.Repository = (*Repository)(nil)

// New answers an empty repository.
func New() *Repository {
	return &Repository{pages: map[string]page.Page{}, now: time.Now}
}

func key(org, id string) string { return org + "/" + id }

// siblings answers the pages under parent in org, by position.
func (r *Repository) siblings(org, parent string) []page.Page {
	var out []page.Page
	for _, p := range r.pages {
		if p.Org == org && p.Parent == parent {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b page.Page) int { return a.Position - b.Position })
	return out
}

// renumber stores the siblings at positions 0, 1, 2 ... in order.
func (r *Repository) renumber(pages []page.Page) {
	for i, p := range pages {
		p.Position = i
		r.pages[key(p.Org, p.ID)] = p
	}
}

// List implements page.Repository.
func (r *Repository) List(_ context.Context, org string) ([]page.Page, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []page.Page
	for _, p := range r.pages {
		if p.Org == org {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b page.Page) int {
		if a.Parent != b.Parent {
			if a.Parent < b.Parent {
				return -1
			}
			return 1
		}
		return a.Position - b.Position
	})
	return out, nil
}

// Get implements page.Repository.
func (r *Repository) Get(_ context.Context, org, id string) (page.Page, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pages[key(org, id)]
	if !ok {
		return page.Page{}, page.ErrPageNotFound
	}
	return p, nil
}

// Create implements page.Repository.
func (r *Repository) Create(_ context.Context, p page.Page) (page.Page, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p.Parent != "" {
		if _, ok := r.pages[key(p.Org, p.Parent)]; !ok {
			return page.Page{}, page.ErrPageNotFound
		}
	}
	p.Position = len(r.siblings(p.Org, p.Parent))
	p.CreatedAt = r.now()
	p.UpdatedAt = p.CreatedAt
	r.pages[key(p.Org, p.ID)] = p
	return p, nil
}

// Update implements page.Repository.
func (r *Repository) Update(_ context.Context, org, id string, in page.UpdateInput) (page.Page, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pages[key(org, id)]
	if !ok {
		return page.Page{}, page.ErrPageNotFound
	}
	if in.Title != nil {
		p.Title = *in.Title
	}
	parent := p.Parent
	if in.Parent != nil {
		parent = *in.Parent
		for at := parent; at != ""; {
			if at == id {
				return page.Page{}, page.ErrPageCycle
			}
			above, ok := r.pages[key(org, at)]
			if !ok {
				return page.Page{}, page.ErrPageNotFound
			}
			at = above.Parent
		}
	}
	if in.Parent != nil || in.Position != nil {
		left := slices.DeleteFunc(r.siblings(org, p.Parent), func(s page.Page) bool { return s.ID == id })
		r.renumber(left)
		joined := slices.DeleteFunc(r.siblings(org, parent), func(s page.Page) bool { return s.ID == id })
		at := len(joined)
		if in.Position != nil {
			at = min(*in.Position, len(joined))
		}
		p.Parent = parent
		r.renumber(slices.Insert(joined, at, p))
		p = r.pages[key(org, id)]
	}
	p.UpdatedAt = r.now()
	r.pages[key(org, id)] = p
	return p, nil
}

// Delete implements page.Repository.
func (r *Repository) Delete(_ context.Context, org, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pages[key(org, id)]
	if !ok {
		return page.ErrPageNotFound
	}
	var remove func(string)
	remove = func(at string) {
		delete(r.pages, key(org, at))
		for _, child := range r.siblings(org, at) {
			remove(child.ID)
		}
	}
	remove(id)
	r.renumber(r.siblings(org, p.Parent))
	return nil
}
