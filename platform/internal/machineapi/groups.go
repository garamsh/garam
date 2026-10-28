package machineapi

import (
	"context"

	"github.com/garamsh/garam/platform/internal/grn"
)

// Groups reports which of its organization's groups a machine identity
// belongs to, so a policy attached to one of them decides what that
// identity sends (ADR-0008). Group membership is org data, and this
// surface holds no store: the composition root satisfies this seam from
// internal/org, as it satisfies the trust root internal/machine reads.
type Groups interface {
	List(ctx context.Context, subject grn.GRN) ([]grn.GRN, error)
}
