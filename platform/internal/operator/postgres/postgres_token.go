package postgres

import (
	"context"
	"slices"
	"strings"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/operator/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
)

// Compile-time check that Store satisfies TokenStore.
var _ operator.TokenStore = (*Store)(nil)

// holdRegistrations holds the client's registration, and the
// audience's when one is named, before an exchange touches any other
// row: an issuance or a deletion takes the registration first too, so
// neither waits on a row the exchange holds while it holds one the
// exchange needs (#1399). It takes the two in GRN byte order, the
// order an organization's deletion takes all of them in (000047), so
// neither holds one while waiting on the other's (#1424). The token's
// foreign keys take the same KEY SHARE lock later. A missing client
// answers refused, a missing audience ErrAudienceInvalid.
func holdRegistrations(ctx context.Context, q *db.Queries, client string, audience *grn.GRN, refused error) error {
	type hold struct {
		grn, what string
		refused   error
	}
	holds := []hold{{client, "hold operator", refused}}
	if audience != nil {
		holds = append(holds, hold{audience.String(), "hold audience", operator.ErrAudienceInvalid})
	}
	slices.SortFunc(holds, func(a, b hold) int { return strings.Compare(a.grn, b.grn) })
	for _, h := range holds {
		if _, err := q.HoldOperatorRegistration(ctx, h.grn); err != nil {
			return pgxerr.Classify(err, h.what, h.refused, nil)
		}
	}
	return nil
}

// recordTokens writes an exchange's token rows so that it never waits
// on a token while holding one another transaction could want (#1424):
// record, which waits on nothing; trim, which takes what it deletes in
// token_hash order while the exchange holds only the token it wrote;
// then sweep the operator's expired tokens, skipping any another
// transaction holds, so it never waits.
func recordTokens(ctx context.Context, q *db.Queries, operatorGRN string, record, trim func() error) error {
	if err := record(); err != nil {
		return pgxerr.Classify(err, "record operator token", nil, nil)
	}
	if err := trim(); err != nil {
		return pgxerr.Classify(err, "trim operator tokens", nil, nil)
	}
	if err := q.SweepOperatorTokens(ctx, operatorGRN); err != nil {
		return pgxerr.Classify(err, "sweep operator tokens", nil, nil)
	}
	return nil
}

// ResolveToken answers what a live token names. The statement joins
// the registration and filters on expiry, so a deleted operator, a
// withdrawn key and an expired token are one answer: none.
func (s *Store) ResolveToken(ctx context.Context, tokenHash string) (operator.TokenHolder, error) {
	row, err := s.q.ResolveOperatorToken(ctx, tokenHash)
	if err != nil {
		return operator.TokenHolder{}, pgxerr.Classify(err, "resolve operator token", operator.ErrTokenInvalid, nil)
	}
	g, err := storedOperatorGRN(row.OperatorGrn)
	if err != nil {
		return operator.TokenHolder{}, err
	}
	holder := operator.TokenHolder{Operator: g, ExpiresAt: row.ExpiresAt.Time}
	if row.AudienceGrn.Valid {
		audience, err := storedOperatorGRN(row.AudienceGrn.String)
		if err != nil {
			return operator.TokenHolder{}, err
		}
		holder.Audience = &audience
	}
	return holder, nil
}

// storedOperatorGRN parses an operator GRN a row holds, which the
// schema already constrained, so a failure is the database's fault.
func storedOperatorGRN(s string) (grn.GRN, error) {
	g, err := grn.Parse(s)
	if err != nil {
		return grn.GRN{}, errorx.Wrap(err, "stored operator grn", errorx.KindInternal)
	}
	if g.ServiceType != operator.ServiceType {
		return grn.GRN{}, errorx.Wrap(operator.ErrNotAnOperator, "stored operator grn", errorx.KindInternal)
	}
	return g, nil
}
