package api

import (
	"context"

	"github.com/garamsh/garam/platform/internal/grn"
)

// AuthorityReader reports whether an organization still holds a
// retired certificate authority, whose sealed row deleting the
// organization destroys (ADR-0118). The composition root satisfies it
// from the organization store's presence read: this package names an
// organization by GRN and never reads key material.
//
// Presence is the whole of what it answers because presence is the
// whole of what stating a consequence needs.
type AuthorityReader interface {
	// HasAuthority reports whether the organization holds a
	// certificate authority.
	HasAuthority(ctx context.Context, orgGRN grn.GRN) (bool, error)
}
