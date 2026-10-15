package operator

import (
	"context"

	"github.com/garamsh/garam/internal/operator/db"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/pgxtype"
)

// Compile-time check that PostgresStore satisfies CertificateStore.
var _ CertificateStore = (*PostgresStore)(nil)

// Adopt records current as the operator's certificate, whatever it held
// before.
func (s *PostgresStore) Adopt(ctx context.Context, current Current) error {
	err := s.q.AdoptOperatorCertificate(ctx, db.AdoptOperatorCertificateParams{
		OperatorGrn: current.Operator.String(),
		Serial:      string(current.Serial),
		IssuedAt:    pgxtype.Timestamptz(current.IssuedAt),
	})
	return pgxerr.Classify(err, "adopt", nil, nil)
}

// Advance records current in place of predecessor, or ErrSuperseded.
//
// The comparison is the statement's, not this process's: the row moves
// only where it still holds predecessor, so two callers renewing from
// one certificate at once leave one successor rather than two.
func (s *PostgresStore) Advance(ctx context.Context, current Current, predecessor Serial) error {
	moved, err := s.q.AdvanceOperatorCertificate(ctx, db.AdvanceOperatorCertificateParams{
		OperatorGrn: current.Operator.String(),
		Serial:      string(current.Serial),
		IssuedAt:    pgxtype.Timestamptz(current.IssuedAt),
		Predecessor: string(predecessor),
	})
	if err != nil {
		return pgxerr.Classify(err, "advance", nil, nil)
	}
	if moved == 0 {
		return ErrSuperseded
	}
	return nil
}
