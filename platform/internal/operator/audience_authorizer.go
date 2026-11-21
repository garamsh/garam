package operator

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// AudienceAuthorizer decides from current state whether a hosted
// operator may perform action on object in org, as the policy service
// reads that state when it is asked: the operations fed on the
// operator, and the organization's policies, a guardrail's forbid among
// them (ADR-0146 item 15). The caller has checked the operator's grant
// covers the action. It answers nil, a permission-denied error, or the
// failure that kept it from deciding, which refuses too.
type AudienceAuthorizer interface {
	Authorize(ctx context.Context, audience, org, object grn.GRN, action string) error
}
