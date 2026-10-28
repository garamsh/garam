package agent

import (
	"context"

	"github.com/garamsh/garam/platform/internal/grn"
)

// RegisteredOperators answers whether an organization holds a record
// of an operator. A token's audience is one, so a mint names only an
// operator that exists, and the per-agent bound cannot be spread over
// audiences nobody registered (ADR-0100). The composition root
// satisfies it from internal/operator; this domain reads no record of
// its own.
type RegisteredOperators interface {
	// Registered returns nil where the organization holds a record of
	// operator, and a KindNotFound error where it holds none.
	Registered(ctx context.Context, operator grn.GRN) error
}
