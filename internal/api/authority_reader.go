package api

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// AuthorityReader reports whether an organization has a certificate
// authority. It is the seam `internal/ca` and the composition root
// satisfy between them, in the shape the agents and operators domains
// already reach that authority through: this package names an
// organization by GRN and never reads key material, so neither the
// sealed key nor the key-encryption key that opens it comes near the
// browser listener.
//
// Presence is the whole of what it answers because presence is the
// whole of what stating a consequence needs. An organization that has
// needed no certificate yet has no authority, and telling its owner
// that deleting it destroys one would be false.
type AuthorityReader interface {
	// HasAuthority reports whether the organization holds a
	// certificate authority.
	HasAuthority(ctx context.Context, orgGRN grn.GRN) (bool, error)
}
