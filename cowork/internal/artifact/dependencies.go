package artifact

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/platform"
)

// Pages reads a page as a caller may: freezing one into a document is
// reading it.
type Pages interface {
	Get(ctx context.Context, caller platform.Caller, org, id string) (page.Page, error)
}

// Documents reads a page's blocks as they now stand.
type Documents interface {
	Read(ctx context.Context, room collab.Room) ([]collab.Block, error)
}

// Viewer gives the address on the user-content origin an HTML
// version is viewed at, good for a short while and for that version
// alone (ADR-0127 §8).
type Viewer interface {
	URL(org, id string, number int) string
}
