package page

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/platform"
)

// Delete removes a page the caller writes, and every page beneath it:
// a page holds its children, as a folder does.
func (s *service) Delete(ctx context.Context, caller platform.Caller, org, id string) error {
	if err := platform.Require(ctx, s.platform, caller, org, platform.OnPage(platform.ActionPageWrite, id)); err != nil {
		return err
	}
	return s.repo.Delete(ctx, org, id)
}
