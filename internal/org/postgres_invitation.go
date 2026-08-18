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
	return classify(err, "create invitation", nil, nil)
}

// GetInvitation returns the Invitation with the given token, or
// ErrInvitationNotFound.
func (s *PostgresStore) GetInvitation(ctx context.Context, token string) (*Invitation, error) {
	row, err := s.q.GetInvitation(ctx, token)
	if err != nil {
		return nil, classify(err, "get invitation", ErrInvitationNotFound, nil)
	}
	return rowToInvitation(row)
}

// AcceptInvitation marks a pending Invitation accepted by the user.
// An unknown token yields ErrInvitationNotFound; an already-accepted
// token yields ErrInvitationAccepted.
func (s *PostgresStore) AcceptInvitation(ctx context.Context, token string, userGRN grn.GRN) (*Invitation, error) {
	row, err := s.q.AcceptInvitation(ctx, db.AcceptInvitationParams{
		Token:      token,
		AcceptedBy: pgtype.Text{String: userGRN.String(), Valid: true},
		AcceptedAt: stamp(time.Now().UTC()),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The update only misses when the token is unknown or the row
		// is already accepted; look it up to tell the two apart.
		if _, getErr := s.q.GetInvitation(ctx, token); getErr != nil {
			return nil, classify(getErr, "accept invitation", ErrInvitationNotFound, nil)
		}
		return nil, ErrInvitationAccepted
	}
	if err != nil {
		return nil, classify(err, "accept invitation", nil, nil)
	}
	return rowToInvitation(row)
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
