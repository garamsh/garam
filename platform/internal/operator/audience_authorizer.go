package operator

import (
	"context"

	"github.com/garamsh/garam/platform/internal/grn"
)

// AudienceAuthorizer decides from current state whether a hosted
// operator may perform action on object in org: the groups it belongs
// to read when it is asked, and the organization's persisted policies
// read past any cache, beside an Allow of exactly action on object.
// That Allow is what the operator's grant contributes, and the caller
// has checked the grant covers it; an explicit Deny wins over it. It
// answers nil, a permission-denied error, or the failure that kept it
// from deciding, which refuses too.
type AudienceAuthorizer interface {
	Authorize(ctx context.Context, audience, org, object grn.GRN, action string) error
}
