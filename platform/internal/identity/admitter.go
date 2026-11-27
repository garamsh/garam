package identity

import (
	"context"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// ErrOrganizationUnknown is what a verified, timely assertion of a
// principal is refused with where garam holds no organization of the id
// its client_id names. It is given only by [Admitter.Admit], after the
// signature verified, so a caller without the service's key never
// reads it; the machine API answers it `unauthorized_client`. A
// principal gone from an organization that exists is not this case.
var ErrOrganizationUnknown = errorx.New(errorx.KindPermissionDenied, "identity: no such organization")

// Admitter is what [Principals] admits a verified assertion through.
type Admitter interface {
	// Admit admits a, an assertion of principal that verified, in one
	// transaction on the database's clock: the principal's record must
	// be live, the assertion timely, and its jti unspent. It spends the
	// jti where every replica reads it, so a second replica refuses a
	// replay. Where the record is gone and no organization of the
	// principal's id exists, a timely assertion is
	// [ErrOrganizationUnknown] and spends nothing. Anything else is
	// [operator.ErrClientInvalid]; an error of another kind is a store
	// that could not be read.
	Admit(ctx context.Context, principal grn.GRN, a operator.VerifiedAssertion) error
	// AdmitInstallation admits a, an assertion of an installation that
	// verified, as Admit admits a principal's, the installation's
	// record held in its place. An installation whose app does not list
	// audience is [ErrAudienceNotListed], and spends nothing.
	AdmitInstallation(ctx context.Context, installation grn.GRN, audience string, a operator.VerifiedAssertion) error
}

// ErrAudienceNotListed refuses an installation's exchange for an
// audience its app's record does not list (ADR-0192 decision 7). It is
// given only after the signature verified, as [ErrOrganizationUnknown]
// is.
var ErrAudienceNotListed = errorx.New(errorx.KindInvalidArgument, "identity: the installation's app lists no such audience")
