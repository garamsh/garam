package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/org"
	"github.com/garamsh/garam/platform/internal/org/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// CreateInvitation persists a new pending Invitation, and deletes the
// pending Invitations that have expired. An accepted one is kept: it
// records where a membership came from.
func (s *Store) CreateInvitation(ctx context.Context, inv *org.Invitation) error {
	err := s.q.CreateInvitation(ctx, db.CreateInvitationParams{
		Token:     inv.Token,
		OrgGrn:    inv.Organization.String(),
		Role:      string(inv.Role),
		CreatedBy: inv.CreatedBy.String(),
		CreatedAt: pgxtype.Timestamptz(inv.CreatedAt),
		ExpiresAt: pgxtype.Timestamptz(inv.ExpiresAt),
	})
	return pgxerr.Classify(err, "create invitation", nil, nil)
}

// GetInvitation returns the Invitation with the given token, or
// ErrInvitationNotFound.
func (s *Store) GetInvitation(ctx context.Context, token string) (*org.Invitation, error) {
	row, err := s.q.GetInvitation(ctx, token)
	if err != nil {
		return nil, pgxerr.Classify(err, "get invitation", org.ErrInvitationNotFound, nil)
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
func (s *Store) AcceptInvitation(ctx context.Context, token string, userGRN grn.GRN) (*org.Membership, error) {
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
		AcceptedAt: pgxtype.Timestamptz(now),
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
	m := org.NewMembership(inv.Organization, userGRN, inv.Role)
	if err := q.AddOrgMember(ctx, db.AddOrgMemberParams{
		OrgGrn:    m.Organization.String(),
		UserGrn:   m.User.String(),
		Role:      string(m.Role),
		CreatedAt: pgxtype.Timestamptz(m.CreatedAt),
	}); err != nil {
		return nil, pgxerr.Classify(err, "add member", nil, org.ErrMemberAlreadyExists)
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
func (s *Store) ClaimInvitation(ctx context.Context, token string) (*org.Invitation, error) {
	now := time.Now().UTC()
	row, err := s.q.ClaimInvitation(ctx, db.ClaimInvitationParams{
		ClaimedAt: pgxtype.Timestamptz(now),
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
func (s *Store) CompleteInvitationClaim(ctx context.Context, inv *org.Invitation, userGRN grn.GRN) error {
	n, err := s.q.CompleteInvitationClaim(ctx, db.CompleteInvitationClaimParams{
		AcceptedBy: pgtype.Text{String: userGRN.String(), Valid: true},
		Token:      inv.Token,
		ClaimedAt:  pgxtype.Timestamptz(inv.AcceptedAt),
	})
	return claimRows(n, err, "complete invitation claim")
}

// ReleaseInvitationClaim returns a claimed Invitation to pending.
func (s *Store) ReleaseInvitationClaim(ctx context.Context, inv *org.Invitation) error {
	n, err := s.q.ReleaseInvitationClaim(ctx, db.ReleaseInvitationClaimParams{
		Token:     inv.Token,
		ClaimedAt: pgxtype.Timestamptz(inv.AcceptedAt),
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
		return pgxerr.Classify(err, op, org.ErrInvitationNotFound, nil)
	}
	inv, err := rowToInvitation(row)
	if err != nil {
		return err
	}
	if inv.Expired(now) && !inv.Accepted() {
		return org.ErrInvitationExpired
	}
	return org.ErrInvitationAccepted
}

func rowToInvitation(row db.Invitation) (*org.Invitation, error) {
	role, err := org.ParseRole(row.Role)
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
	inv := &org.Invitation{
		Token:        row.Token,
		Organization: og,
		Role:         role,
		CreatedBy:    cb,
		CreatedAt:    row.CreatedAt.Time,
		ExpiresAt:    row.ExpiresAt.Time,
	}
	if row.AcceptedBy.Valid {
		ab, err := grn.Parse(row.AcceptedBy.String)
		if err != nil {
			return nil, errorx.Wrap(err, "invitation row acceptor grn", errorx.KindInternal)
		}
		inv.AcceptedBy = ab
	}
	if row.AcceptedAt.Valid {
		inv.AcceptedAt = row.AcceptedAt.Time
	}
	return inv, nil
}
