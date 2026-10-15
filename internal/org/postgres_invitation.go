package org

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org/db"
	"github.com/garamsh/garam/internal/pgxerr"
)

// CreateInvitation persists a new pending Invitation, and deletes the
// pending Invitations that have expired. An accepted one is kept: it
// records where a membership came from.
func (s *PostgresStore) CreateInvitation(ctx context.Context, inv *Invitation) error {
	err := s.q.CreateInvitation(ctx, db.CreateInvitationParams{
		Token:     inv.token,
		OrgGrn:    inv.orgGRN.String(),
		Role:      string(inv.role),
		CreatedBy: inv.createdBy.String(),
		CreatedAt: stamp(inv.createdAt),
		ExpiresAt: stamp(inv.expiresAt),
	})
	return pgxerr.Classify(err, "create invitation", nil, nil)
}

// GetInvitation returns the Invitation with the given token, or
// ErrInvitationNotFound.
func (s *PostgresStore) GetInvitation(ctx context.Context, token string) (*Invitation, error) {
	row, err := s.q.GetInvitation(ctx, token)
	if err != nil {
		return nil, pgxerr.Classify(err, "get invitation", ErrInvitationNotFound, nil)
	}
	return rowToInvitation(row)
}

// AcceptInvitation spends a pending, unexpired Invitation and adds the
// user to its org with its role, in one transaction. The conditional
// update is the claim: of two concurrent accepts one matches the row
// and the other waits on it and then matches nothing, so it adds no
// member. A membership the user already holds rolls the claim back,
// leaving the invitation unspent.
//
// An unknown token yields ErrInvitationNotFound, an accepted one
// ErrInvitationAccepted, and an expired one ErrInvitationExpired.
func (s *PostgresStore) AcceptInvitation(ctx context.Context, token string, userGRN grn.GRN) (*Membership, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	now := time.Now().UTC()
	row, err := q.AcceptInvitation(ctx, db.AcceptInvitationParams{
		Token:      token,
		AcceptedBy: pgtype.Text{String: userGRN.String(), Valid: true},
		AcceptedAt: stamp(now),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, unclaimable(ctx, q, "accept invitation", token, now)
	}
	if err != nil {
		return nil, pgxerr.Classify(err, "accept invitation", nil, nil)
	}
	inv, err := rowToInvitation(row)
	if err != nil {
		return nil, err
	}
	m := NewMembership(inv.orgGRN, userGRN, inv.role)
	if err := q.AddOrgMember(ctx, db.AddOrgMemberParams{
		OrgGrn:    m.orgGRN.String(),
		UserGrn:   m.userGRN.String(),
		Role:      string(m.role),
		CreatedAt: stamp(m.createdAt),
	}); err != nil {
		return nil, pgxerr.Classify(err, "add member", nil, ErrMemberAlreadyExists)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errorx.Wrap(err, "commit")
	}
	return m, nil
}

// ClaimInvitation spends a pending, unexpired Invitation for an
// account that does not exist yet. The returned Invitation's
// AcceptedAt is the claim's mark, which CompleteInvitationClaim and
// ReleaseInvitationClaim match on. Errors as AcceptInvitation's.
func (s *PostgresStore) ClaimInvitation(ctx context.Context, token string) (*Invitation, error) {
	now := time.Now().UTC()
	row, err := s.q.ClaimInvitation(ctx, db.ClaimInvitationParams{
		ClaimedAt: stamp(now),
		Token:     token,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, unclaimable(ctx, s.q, "claim invitation", token, now)
	}
	if err != nil {
		return nil, pgxerr.Classify(err, "claim invitation", nil, nil)
	}
	return rowToInvitation(row)
}

// CompleteInvitationClaim records userGRN as the claimed Invitation's
// acceptor, once that user exists.
func (s *PostgresStore) CompleteInvitationClaim(ctx context.Context, inv *Invitation, userGRN grn.GRN) error {
	n, err := s.q.CompleteInvitationClaim(ctx, db.CompleteInvitationClaimParams{
		AcceptedBy: pgtype.Text{String: userGRN.String(), Valid: true},
		Token:      inv.token,
		ClaimedAt:  stamp(inv.acceptedAt),
	})
	return claimRows(n, err, "complete invitation claim")
}

// ReleaseInvitationClaim returns a claimed Invitation to pending.
func (s *PostgresStore) ReleaseInvitationClaim(ctx context.Context, inv *Invitation) error {
	n, err := s.q.ReleaseInvitationClaim(ctx, db.ReleaseInvitationClaimParams{
		Token:     inv.token,
		ClaimedAt: stamp(inv.acceptedAt),
	})
	return claimRows(n, err, "release invitation claim")
}

// claimRows refuses a claim update that matched no row: the claim it
// names is not held.
func claimRows(n int64, err error, op string) error {
	if err != nil {
		return pgxerr.Classify(err, op, nil, nil)
	}
	if n == 0 {
		return errorx.New(errorx.KindInternal, op+": the claim is not held")
	}
	return nil
}

// unclaimable names why the claim op took on token matched no row: the
// token is unknown, already spent, or expired, in that order. A row
// that is live again by the time it is read was spent when the claim
// ran and released since, by a holder whose own request failed: the
// claim lost that race and is refused as spent, which is what it met.
func unclaimable(ctx context.Context, q *db.Queries, op, token string, now time.Time) error {
	row, err := q.GetInvitation(ctx, token)
	if err != nil {
		return pgxerr.Classify(err, op, ErrInvitationNotFound, nil)
	}
	inv, err := rowToInvitation(row)
	if err != nil {
		return err
	}
	if inv.Expired(now) && !inv.Accepted() {
		return ErrInvitationExpired
	}
	return ErrInvitationAccepted
}

func rowToInvitation(row db.Invitation) (*Invitation, error) {
	role, err := ParseRole(row.Role)
	if err != nil {
		return nil, errorx.Wrap(err, "invitation row role", errorx.KindInternal)
	}
	og, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "invitation row org grn", errorx.KindInternal)
	}
	cb, err := grn.Parse(row.CreatedBy)
	if err != nil {
		return nil, errorx.Wrap(err, "invitation row creator grn", errorx.KindInternal)
	}
	inv := &Invitation{
		token:     row.Token,
		orgGRN:    og,
		role:      role,
		createdBy: cb,
		createdAt: row.CreatedAt.Time,
		expiresAt: row.ExpiresAt.Time,
	}
	if row.AcceptedBy.Valid {
		ab, err := grn.Parse(row.AcceptedBy.String)
		if err != nil {
			return nil, errorx.Wrap(err, "invitation row acceptor grn", errorx.KindInternal)
		}
		inv.acceptedBy = ab
	}
	if row.AcceptedAt.Valid {
		inv.acceptedAt = row.AcceptedAt.Time
	}
	return inv, nil
}
