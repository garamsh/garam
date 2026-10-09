package operator

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator/db"
	"github.com/garamsh/garam/internal/pgxerr"
)

// Compile-time check that PostgresStore satisfies TokenStore.
var _ TokenStore = (*PostgresStore)(nil)

// CredentialHolder answers the operator a presented credential belongs
// to. The read joins the registration, so a credential whose operator
// was deleted answers as no credential at all.
func (s *PostgresStore) CredentialHolder(ctx context.Context, credentialHash string) (CredentialHolder, error) {
	row, err := s.q.GetOperatorCredentialHolder(ctx, credentialHash)
	if err != nil {
		return CredentialHolder{}, pgxerr.Classify(err, "read operator credential", ErrCredentialInvalid, nil)
	}
	g, err := storedOperatorGRN(row.OperatorGrn)
	if err != nil {
		return CredentialHolder{}, err
	}
	return CredentialHolder{ID: row.ID, Operator: g}, nil
}

// RecordToken stores the token under the credential issuance holder
// names, and keeps that issuance's newest LiveTokensPerCredential, in
// one transaction holding the issuance's row. Holding the row is what
// keeps two exchanges at once inside the bound, and finding it gone is
// the refusal of a credential re-issued or withdrawn after it was read.
func (s *PostgresStore) RecordToken(ctx context.Context, holder CredentialHolder, t *Token) error {
	audience := pgtype.Text{}
	if t.Audience != nil {
		audience = pgtype.Text{String: t.Audience.String(), Valid: true}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if _, err := q.LockOperatorCredential(ctx, holder.ID); err != nil {
		return pgxerr.Classify(err, "hold operator credential", ErrCredentialInvalid, nil)
	}
	if err := q.RecordOperatorToken(ctx, db.RecordOperatorTokenParams{
		TokenHash:    t.Hash(),
		CredentialID: holder.ID,
		OperatorGrn:  holder.Operator.String(),
		AudienceGrn:  audience,
		IssuedAt:     stamp(t.IssuedAt),
		ExpiresAt:    stamp(t.ExpiresAt),
	}); err != nil {
		// The credential's row is held, so the one key a missing row
		// can refuse here is the audience's: an operator the
		// organization never registered, or deleted meanwhile.
		return pgxerr.Classify(err, "record operator token", ErrAudienceInvalid, nil)
	}
	if err := q.TrimOperatorTokens(ctx, db.TrimOperatorTokensParams{
		CredentialID: holder.ID, Keep: LiveTokensPerCredential,
	}); err != nil {
		return pgxerr.Classify(err, "trim operator tokens", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "commit")
	}
	return nil
}

// ResolveToken answers what a live token names. The statement joins
// the registration and filters on expiry, so a deleted operator, a
// re-issued credential and an expired token are one answer: none.
func (s *PostgresStore) ResolveToken(ctx context.Context, tokenHash string) (TokenHolder, error) {
	row, err := s.q.ResolveOperatorToken(ctx, tokenHash)
	if err != nil {
		return TokenHolder{}, pgxerr.Classify(err, "resolve operator token", ErrTokenInvalid, nil)
	}
	g, err := storedOperatorGRN(row.OperatorGrn)
	if err != nil {
		return TokenHolder{}, err
	}
	holder := TokenHolder{Operator: g, ExpiresAt: row.ExpiresAt.Time}
	if row.AudienceGrn.Valid {
		audience, err := storedOperatorGRN(row.AudienceGrn.String)
		if err != nil {
			return TokenHolder{}, err
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
	if g.ServiceType != ServiceType {
		return grn.GRN{}, errorx.Wrap(ErrNotAnOperator, "stored operator grn", errorx.KindInternal)
	}
	return g, nil
}
