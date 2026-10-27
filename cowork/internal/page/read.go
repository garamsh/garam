package page

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/platform"
)

// maxChecksPerAsk is how many pairs the platform answers in one
// permission check request.
const maxChecksPerAsk = 50

// List answers the pages the caller may read, of a tree the caller may
// read. A page it may not read is left out, and so are the pages
// beneath it, which the tree would otherwise show without their place.
func (s *service) List(ctx context.Context, caller platform.Caller, org string) ([]Page, error) {
	if err := platform.Require(ctx, s.platform, caller, org, platform.Check{Action: platform.ActionPageRead}); err != nil {
		return nil, err
	}
	pages, err := s.repo.List(ctx, org)
	if err != nil {
		return nil, err
	}
	readable := make(map[string]bool, len(pages))
	for start := 0; start < len(pages); start += maxChecksPerAsk {
		batch := pages[start:min(start+maxChecksPerAsk, len(pages))]
		checks := make([]platform.Check, len(batch))
		for i, p := range batch {
			checks[i] = platform.Check{Action: platform.ActionPageRead, Page: p.ID}
		}
		allowed, err := s.platform.Allowed(ctx, caller, org, checks)
		if err != nil {
			return nil, err
		}
		for i, p := range batch {
			readable[p.ID] = allowed[i]
		}
	}
	byID := make(map[string]Page, len(pages))
	for _, p := range pages {
		byID[p.ID] = p
	}
	visible := func(p Page) bool {
		for {
			if !readable[p.ID] {
				return false
			}
			if p.Parent == "" {
				return true
			}
			p = byID[p.Parent]
		}
	}
	var out []Page
	for _, p := range pages {
		if visible(p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// Get answers one page the caller may read.
func (s *service) Get(ctx context.Context, caller platform.Caller, org, id string) (Page, error) {
	if err := platform.Require(ctx, s.platform, caller, org, platform.Check{Action: platform.ActionPageRead, Page: id}); err != nil {
		return Page{}, err
	}
	return s.repo.Get(ctx, org, id)
}
