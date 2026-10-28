package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/operator/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// Compile-time check that Store satisfies operator.GrantStore.
var _ operator.GrantStore = (*Store)(nil)

// WriteGrant applies a grant write in one transaction: the replay read,
// the lock on the current grant, the staleness comparison, ending it,
// and writing its successor. Two first writes racing past an empty view
// meet the unique index on the current grant, and the loser is stale.
func (s *Store) WriteGrant(ctx context.Context, w operator.GrantWrite, id operator.GrantID) (*operator.Grant, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	prior, err := q.GetDelegationByRequest(ctx, db.GetDelegationByRequestParams{
		OrgGrn: w.Org.String(), OperatorGrn: w.Operator.String(), RequestID: string(w.RequestID),
	})
	switch {
	case err == nil:
		return replay(ctx, q, prior, w)
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, false, storageError(err, "read delegation request", nil, nil)
	}

	current, err := q.LockCurrentDelegation(ctx, db.LockCurrentDelegationParams{
		OrgGrn: w.Org.String(), OperatorGrn: w.Operator.String(),
	})
	hasCurrent := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, storageError(err, "lock current delegation", nil, nil)
	}
	switch {
	case hasCurrent != (w.Expected != nil):
		return nil, false, operator.ErrGrantStale
	case hasCurrent && operator.GrantID(current.ID) != *w.Expected:
		return nil, false, operator.ErrGrantStale
	}
	if hasCurrent {
		if _, err := q.EndDelegation(ctx, db.EndDelegationParams{
			EndedAt: pgxtype.Timestamptz(w.Now), ID: current.ID, OrgGrn: w.Org.String(), OperatorGrn: w.Operator.String(),
		}); err != nil {
			return nil, false, storageError(err, "end delegation", nil, nil)
		}
	}

	operations := make([]string, len(w.Terms.Operations))
	for i, op := range w.Terms.Operations {
		operations[i] = string(op)
	}
	if err := q.CreateDelegation(ctx, db.CreateDelegationParams{
		ID: string(id), OrgGrn: w.Org.String(), OperatorGrn: w.Operator.String(),
		RequestID: string(w.RequestID), Replaces: optionalText((*string)(w.Expected)),
		Operations: operations, ExpiresAt: pgxtype.Timestamptz(w.Terms.ExpiresAt),
		GrantedBy: w.Actor.String(), GrantedAt: pgxtype.Timestamptz(w.Now),
	}); err != nil {
		return nil, false, storageError(err, "create delegation", operator.ErrOperatorNotFound, operator.ErrGrantStale)
	}
	if err := q.AddDelegationControllers(ctx, db.AddDelegationControllersParams{
		DelegationID: string(id), ControllerGrns: grnStrings(w.Terms.Controllers),
	}); err != nil {
		return nil, false, storageError(err, "add delegation controllers", operator.ErrOperatorNotFound, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, storageError(err, "commit delegation", nil, operator.ErrGrantStale)
	}
	return &operator.Grant{
		ID: id, Org: w.Org, Operator: w.Operator, Controllers: w.Terms.Controllers,
		Operations: w.Terms.Operations, ExpiresAt: w.Terms.ExpiresAt, RequestID: w.RequestID,
		Replaces: w.Expected, GrantedBy: w.Actor, GrantedAt: w.Now,
	}, true, nil
}

// replay answers a repeated request: the grant it wrote while that
// grant is current, and a refusal once it is ended or where the write
// differs.
func replay(ctx context.Context, q *db.Queries, row db.OperatorDelegation, w operator.GrantWrite) (*operator.Grant, bool, error) {
	g, err := grantFromRow(ctx, q, row)
	switch {
	case err != nil:
		return nil, false, err
	case !g.Records(w):
		return nil, false, operator.ErrGrantRequestConflict
	case g.EndedAt != nil:
		return nil, false, operator.ErrGrantEnded
	}
	return g, false, nil
}

// EndGrant ends the named grant, and answers nil for one already ended.
func (s *Store) EndGrant(ctx context.Context, org, hosted grn.GRN, id operator.GrantID, now time.Time) error {
	n, err := s.q.EndDelegation(ctx, db.EndDelegationParams{
		EndedAt: pgxtype.Timestamptz(now), ID: string(id), OrgGrn: org.String(), OperatorGrn: hosted.String(),
	})
	if err != nil {
		return storageError(err, "end delegation", nil, nil)
	}
	if n == 1 {
		return nil
	}
	row, err := s.q.GetDelegation(ctx, string(id))
	if err != nil {
		return storageError(err, "get delegation", operator.ErrGrantNotFound, nil)
	}
	if row.OrgGrn != org.String() || row.OperatorGrn != hosted.String() {
		return operator.ErrGrantNotFound
	}
	return nil
}

// CurrentGrant answers the hosted operator's current grant.
func (s *Store) CurrentGrant(ctx context.Context, org, hosted grn.GRN) (*operator.Grant, error) {
	row, err := s.q.GetCurrentDelegation(ctx, db.GetCurrentDelegationParams{
		OrgGrn: org.String(), OperatorGrn: hosted.String(),
	})
	if err != nil {
		return nil, storageError(err, "get current delegation", operator.ErrGrantNotFound, nil)
	}
	return grantFromRow(ctx, s.q, row)
}

// GetGrant answers one grant by identifier.
func (s *Store) GetGrant(ctx context.Context, id operator.GrantID) (*operator.Grant, error) {
	row, err := s.q.GetDelegation(ctx, string(id))
	if err != nil {
		return nil, storageError(err, "get delegation", operator.ErrGrantNotFound, nil)
	}
	return grantFromRow(ctx, s.q, row)
}

// grantFromRow reads a grant row's controllers and converts both. A
// stored value that no longer parses is data corruption, hence
// internal.
func grantFromRow(ctx context.Context, q *db.Queries, row db.OperatorDelegation) (*operator.Grant, error) {
	controllers, err := q.ListDelegationControllers(ctx, row.ID)
	if err != nil {
		return nil, storageError(err, "list delegation controllers", nil, nil)
	}
	g := &operator.Grant{
		ID: operator.GrantID(row.ID), RequestID: operator.RequestID(row.RequestID),
		ExpiresAt: row.ExpiresAt.Time.UTC(), GrantedAt: row.GrantedAt.Time.UTC(),
	}
	if row.EndedAt.Valid {
		ended := row.EndedAt.Time.UTC()
		g.EndedAt = &ended
	}
	if row.Replaces.Valid {
		replaced := operator.GrantID(row.Replaces.String)
		g.Replaces = &replaced
	}
	if g.Org, err = grn.Parse(row.OrgGrn); err != nil {
		return nil, errorx.Wrap(err, "stored delegation org", errorx.KindInternal)
	}
	if g.Operator, err = grn.Parse(row.OperatorGrn); err != nil {
		return nil, errorx.Wrap(err, "stored delegation operator", errorx.KindInternal)
	}
	if g.GrantedBy, err = grn.Parse(row.GrantedBy); err != nil {
		return nil, errorx.Wrap(err, "stored delegation granter", errorx.KindInternal)
	}
	for _, c := range controllers {
		parsed, err := grn.Parse(c)
		if err != nil {
			return nil, errorx.Wrap(err, "stored delegation controller", errorx.KindInternal)
		}
		g.Controllers = append(g.Controllers, parsed)
	}
	for _, o := range row.Operations {
		op, err := operator.ParseOperation(o)
		if err != nil {
			return nil, errorx.Wrap(err, "stored delegation operation", errorx.KindInternal)
		}
		g.Operations = append(g.Operations, op)
	}
	return g, nil
}

func grnStrings(gs []grn.GRN) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.String()
	}
	return out
}
