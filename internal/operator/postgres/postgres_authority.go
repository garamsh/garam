package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/operator/postgres/db"
)

// Compile-time check that Store satisfies operator.AuthorityStore.
var _ operator.AuthorityStore = (*Store)(nil)

// MintAuthority records the binding or replaces its token, in the one
// statement MintOperationAuthority is. No row back means the identifier
// holds a different binding.
func (s *Store) MintAuthority(ctx context.Context, b operator.Binding, tokenHash string, expiresAt, now time.Time) (operator.OperationRef, error) {
	var assignee, epoch pgtype.Text
	if b.Assignment != nil {
		assignee = pgtype.Text{String: b.Assignment.Operator.String(), Valid: true}
		epoch = pgtype.Text{String: b.Assignment.Epoch, Valid: true}
	}
	ref, err := s.q.MintOperationAuthority(ctx, db.MintOperationAuthorityParams{
		Ref: string(b.Ref), OrgGrn: b.Org.String(), AudienceGrn: b.Audience.String(),
		RequestID: string(b.RequestID), DelegationID: string(b.Grant), ActorGrn: b.Actor.String(),
		Operation: string(b.Operation), TargetGrn: b.Target.String(),
		AssigneeGrn: assignee, AssignmentEpoch: epoch, BodySha256: string(b.Digest),
		TokenHash: tokenHash, TokenExpiresAt: stamp(expiresAt), CreatedAt: stamp(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", operator.ErrAuthorityRequestConflict
	}
	if err != nil {
		return "", storageError(err, "mint operation authority", nil, nil)
	}
	return operator.OperationRef(ref), nil
}

// AuthorityByToken answers the binding a token hash names.
func (s *Store) AuthorityByToken(ctx context.Context, tokenHash string) (operator.Introspected, error) {
	row, err := s.q.GetOperationAuthorityByToken(ctx, tokenHash)
	if err != nil {
		return operator.Introspected{}, storageError(err, "get operation authority", operator.ErrAuthorityNotFound, nil)
	}
	b, err := bindingFromRow(row)
	if err != nil {
		return operator.Introspected{}, err
	}
	return operator.Introspected{Binding: b, ExpiresAt: row.TokenExpiresAt.Time.UTC()}, nil
}

// AuthorityByRef answers the binding a reference names.
func (s *Store) AuthorityByRef(ctx context.Context, ref operator.OperationRef) (operator.Binding, error) {
	row, err := s.q.GetOperationAuthority(ctx, string(ref))
	if err != nil {
		return operator.Binding{}, storageError(err, "get operation authority", operator.ErrAuthorityNotFound, nil)
	}
	return bindingFromRow(row)
}

// bindingFromRow converts a stored authority. A value that no longer
// parses is data corruption, hence internal.
func bindingFromRow(row db.OperationAuthority) (operator.Binding, error) {
	b := operator.Binding{
		Ref: operator.OperationRef(row.Ref), Grant: operator.GrantID(row.DelegationID),
		RequestID: operator.RequestID(row.RequestID), Digest: operator.BodyDigest(row.BodySha256),
		Operation: operator.Operation(row.Operation),
	}
	var err error
	if b.Org, err = grn.Parse(row.OrgGrn); err != nil {
		return operator.Binding{}, errorx.Wrap(err, "stored authority org", errorx.KindInternal)
	}
	if b.Actor, err = grn.Parse(row.ActorGrn); err != nil {
		return operator.Binding{}, errorx.Wrap(err, "stored authority actor", errorx.KindInternal)
	}
	if b.Audience, err = grn.Parse(row.AudienceGrn); err != nil {
		return operator.Binding{}, errorx.Wrap(err, "stored authority audience", errorx.KindInternal)
	}
	if b.Target, err = grn.Parse(row.TargetGrn); err != nil {
		return operator.Binding{}, errorx.Wrap(err, "stored authority target", errorx.KindInternal)
	}
	if row.AssigneeGrn.Valid {
		assignee, err := grn.Parse(row.AssigneeGrn.String)
		if err != nil {
			return operator.Binding{}, errorx.Wrap(err, "stored authority assignee", errorx.KindInternal)
		}
		b.Assignment = &operator.Assignment{Operator: assignee, Epoch: row.AssignmentEpoch.String}
	}
	return b, nil
}
