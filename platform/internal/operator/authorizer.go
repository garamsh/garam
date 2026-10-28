package operator

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// Authorizer decides from current state whether a user may perform
// action on object in org: membership, roles and groups read when it is
// asked, and the organization's persisted policies read past any cache.
// It answers nil, a permission-denied error, or the failure that kept
// it from deciding, which refuses too. Nothing here falls back to a
// cached answer.
type Authorizer interface {
	Authorize(ctx context.Context, actor, org, object grn.GRN, action string) error
}
