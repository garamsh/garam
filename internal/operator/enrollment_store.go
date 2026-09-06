package operator

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// EnrollmentStore spends the tokens [Store] minted. It is separate from
// the two stores beside it because it answers a third question: not
// what the organization registered and not what may still be renewed,
// but whether this token is still worth anything.
//
// There is no read, and no delete a caller here reaches. A token is
// written by the registration or the re-mint that minted it, and it
// leaves one of two ways: the call that spends it, or the registration
// going, which the row's foreign key carries it with (ADR-0070). Only
// the first is this interface's, so nothing here can answer a token
// without also spending it — which is what makes single use a property
// of the store rather than a check a caller performs.
type EnrollmentStore interface {
	// Spend consumes the token and returns the operator it enrols, or
	// ErrEnrollmentInvalid where no live token has that hash. Spending
	// is what makes it single-use: the row is gone when this returns,
	// so two callers presenting one token leave one certificate.
	Spend(ctx context.Context, tokenHash string) (grn.GRN, error)
}
