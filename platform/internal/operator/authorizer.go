package operator

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// Authorizer decides from current state whether a user may perform
// action on object in org, as the policy service reads that state when
// it is asked (ADR-0146 item 14). It answers nil, a permission-denied
// error, or the failure that kept it from deciding, which refuses too.
// Nothing here falls back to a cached answer.
type Authorizer interface {
	Authorize(ctx context.Context, actor, org, object grn.GRN, action string) error
}
