package page

import (
	"context"

	"github.com/garamsh/garam/cowork/internal/platform"
)

// Service is the page tree as a caller acts on it. Every method asks
// the platform before it reads or writes (ADR-0127): reading the tree
// needs cowork:page-read on the organization, and each page listed
// cowork:page-read on that page; creating needs cowork:page-write on
// the organization and on the parent; reading a page needs
// cowork:page-read on it, and changing or deleting it cowork:page-write
// on it and on any parent it moves under.
type Service interface {
	List(ctx context.Context, caller platform.Caller, org string) ([]Page, error)
	Get(ctx context.Context, caller platform.Caller, org, id string) (Page, error)
	Create(ctx context.Context, caller platform.Caller, org string, in CreateInput) (Page, error)
	Update(ctx context.Context, caller platform.Caller, org, id string, in UpdateInput) (Page, error)
	Delete(ctx context.Context, caller platform.Caller, org, id string) error
}

type service struct {
	repo     Repository
	platform platform.Platform
}

// NewService answers the page tree over repo, deciding every act
// through p.
func NewService(repo Repository, p platform.Platform) Service {
	return &service{repo: repo, platform: p}
}
