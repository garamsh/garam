package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// MemberAuthorizer decides from current state whether a user may
// perform action on object in org: membership, roles and groups read
// when it is asked, and the organization's persisted policies read
// past any cache, an explicit Deny winning. It answers nil, a
// permission-denied error, or the failure that kept it from deciding,
// which refuses too.
type MemberAuthorizer interface {
	Authorize(ctx context.Context, actor, org, object grn.GRN, action string) error
}
