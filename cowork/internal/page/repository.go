package page

import "context"

// Repository keeps the page tree. Every method is scoped to an
// organization, so a page of another one answers ErrPageNotFound.
type Repository interface {
	// List answers the organization's pages, by parent and then
	// position.
	List(ctx context.Context, org string) ([]Page, error)
	Get(ctx context.Context, org, id string) (Page, error)
	// Create stores p after its last sibling and answers it with the
	// position it took. A parent that does not exist is
	// ErrPageNotFound.
	Create(ctx context.Context, p Page) (Page, error)
	// Update applies in to the page and answers the page as it now
	// stands, renumbering the siblings it leaves and joins. It refuses
	// a parent that is the page or one of its descendants with
	// ErrPageCycle.
	Update(ctx context.Context, org, id string, in UpdateInput) (Page, error)
	// Delete removes the page and every page beneath it.
	Delete(ctx context.Context, org, id string) error
}
