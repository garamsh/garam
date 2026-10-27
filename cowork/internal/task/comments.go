package task

import (
	"context"
	"strings"
	"time"

	"github.com/garamsh/garam/cowork/internal/platform"
)

// Comment adds the caller's comment to a task it may write.
func (s *service) Comment(ctx context.Context, caller platform.Caller, org, id, body string) (Comment, error) {
	if strings.TrimSpace(body) == "" || len(body) > maxBody {
		return Comment{}, ErrInvalidComment
	}
	if err := platform.Require(ctx, s.platform, caller, org, platform.OnTask(ActionWrite, id)); err != nil {
		return Comment{}, err
	}
	c := Comment{ID: newID(), Org: org, Task: id, Author: caller.GRN, Body: body, CreatedAt: time.Now()}
	if err := s.repo.AddComment(ctx, c); err != nil {
		return Comment{}, err
	}
	return c, nil
}

// Comments answers the comments on a task the caller may read.
func (s *service) Comments(ctx context.Context, caller platform.Caller, org, id string) ([]Comment, error) {
	if err := platform.Require(ctx, s.platform, caller, org, platform.OnTask(ActionRead, id)); err != nil {
		return nil, err
	}
	if _, err := s.repo.Get(ctx, org, id); err != nil {
		return nil, err
	}
	return s.repo.Comments(ctx, org, id)
}
