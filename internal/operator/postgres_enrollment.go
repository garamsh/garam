package operator

import (
	"context"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
)

// Compile-time check that PostgresStore satisfies EnrollmentStore.
var _ EnrollmentStore = (*PostgresStore)(nil)

// Spend consumes the token and returns the operator it enrols.
//
// The delete is the whole of the rule: the statement removes the row
// where it is still live, so single use and expiry are decided by the
// statement rather than by this process, and two callers presenting one
// token leave one certificate. A statement that removed nothing is the
// one refusal, whichever of unknown, spent and expired it was.
func (s *PostgresStore) Spend(ctx context.Context, tokenHash string) (grn.GRN, error) {
	row, err := s.q.SpendOperatorEnrollment(ctx, tokenHash)
	if err != nil {
		return grn.GRN{}, pgxerr.Classify(err, "spend enrollment token", ErrEnrollmentInvalid, nil)
	}
	g, err := grn.Parse(row)
	if err != nil {
		return grn.GRN{}, errorx.Wrap(err, "enrollment operator grn", errorx.KindInternal)
	}
	if g.ServiceType != ServiceType {
		return grn.GRN{}, errorx.Wrap(ErrNotAnOperator, "enrollment operator grn", errorx.KindInternal)
	}
	return g, nil
}
