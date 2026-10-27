package page

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/platform"
)

// Update changes a page the caller writes, and moves it only under a
// parent the caller writes.
func (s *service) Update(ctx context.Context, caller platform.Caller, org, id string, in UpdateInput) (Page, error) {
	if in.Title != nil && len(*in.Title) > maxTitle {
		return Page{}, ErrTitleTooLong
	}
	if in.Position != nil && *in.Position < 0 {
		return Page{}, ErrPositionNegative
	}
	checks := []platform.Check{{Action: platform.ActionPageWrite, Page: id}}
	if in.Parent != nil && *in.Parent != "" {
		if *in.Parent == id {
			return Page{}, ErrPageCycle
		}
		checks = append(checks, platform.Check{Action: platform.ActionPageWrite, Page: *in.Parent})
	}
	if err := platform.Require(ctx, s.platform, caller, org, checks...); err != nil {
		return Page{}, err
	}
	return s.repo.Update(ctx, org, id, in)
}
