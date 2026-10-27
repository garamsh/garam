package task

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/channel"
	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/platform"
)

// Pages reads a page as a caller may, which is what linking one asks:
// a task links only a page its writer may read.
type Pages interface {
	Get(ctx context.Context, caller platform.Caller, org, id string) (page.Page, error)
}

// Messages reads a message's thread as a caller may, which is what
// linking a message asks: a task links only a message its writer may
// read.
type Messages interface {
	Thread(ctx context.Context, caller platform.Caller, org, message string) ([]channel.Message, error)
}
