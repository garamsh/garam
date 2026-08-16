package ca

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// Store persists one Authority per organization. An Authority is
// written once: nothing here updates one, because a new key is a new
// authority rather than a rewritten row.
type Store interface {
	// Create persists a new Authority, or ErrAuthorityAlreadyExists
	// when the organization already has one.
	Create(ctx context.Context, a *Authority) error
	// Get returns the organization's Authority, or
	// ErrAuthorityNotFound.
	Get(ctx context.Context, orgGRN grn.GRN) (*Authority, error)
}
