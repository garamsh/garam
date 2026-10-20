package postgres

import (
	"context"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/operator/postgres/db"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/pgxtype"
)

// Compile-time check that Store satisfies operator.Store.
var _ operator.Store = (*Store)(nil)

// Register persists the record and reads the stored operator back in
// the same transaction.
func (s *Store) Register(ctx context.Context, o *operator.Operator) (*operator.Operator, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if err := q.CreateOperator(ctx, db.CreateOperatorParams{
		Grn:          o.GRN.String(),
		OrgGrn:       o.Organization.String(),
		RegisteredBy: o.RegisteredBy.String(),
		CreatedAt:    pgxtype.Timestamptz(o.CreatedAt),
		Name:         o.Name,
		Description:  o.Description,
	}); err != nil {
		return nil, pgxerr.Classify(err, "register operator", nil, operator.ErrOperatorAlreadyRegistered)
	}
	row, err := q.GetOperator(ctx, o.GRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "read registered operator", nil, nil)
	}
	registered, err := rowToOperator(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errorx.Wrap(err, "commit")
	}
	return registered, nil
}

// UpdateDetails writes the patch and reads the operator back in one
// transaction, so the answer is the row this write left rather than
// one a concurrent write replaced in between.
func (s *Store) UpdateDetails(ctx context.Context, g grn.GRN, patch operator.DetailsPatch) (*operator.Operator, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	n, err := q.UpdateOperatorDetails(ctx, db.UpdateOperatorDetailsParams{
		Grn:         g.String(),
		Name:        optionalText(patch.Name()),
		Description: optionalText(patch.Description()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "update operator details", nil, nil)
	}
	if n == 0 {
		return nil, operator.ErrOperatorNotFound
	}
	row, err := q.GetOperator(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "read updated operator", operator.ErrOperatorNotFound, nil)
	}
	updated, err := rowToOperator(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errorx.Wrap(err, "commit")
	}
	return updated, nil
}

// SetControlEndpoint writes the endpoint and reads the operator back in
// one transaction, as UpdateDetails does.
func (s *Store) SetControlEndpoint(ctx context.Context, g grn.GRN, endpoint *operator.ControlEndpoint) (*operator.Operator, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	n, err := q.SetOperatorControlEndpoint(ctx, db.SetOperatorControlEndpointParams{
		Grn:             g.String(),
		ControlEndpoint: optionalText((*string)(endpoint)),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "set operator control endpoint", nil, nil)
	}
	if n == 0 {
		return nil, operator.ErrOperatorNotFound
	}
	row, err := q.GetOperator(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "read operator", operator.ErrOperatorNotFound, nil)
	}
	updated, err := rowToOperator(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errorx.Wrap(err, "commit")
	}
	return updated, nil
}

// Delete removes the record, in one transaction with the tokens it
// holds.
//
// The transaction holds the registration first, as every writer of
// the operator's keys and tokens does, and then deletes
// every token the operator holds or is the audience of in token_hash
// order, the order every statement waiting on several tokens takes
// them in. The cascades would take them in their own orders, and the
// audience's reaches tokens of other operators whose registrations it
// does not hold (#1424).
//
// The statements are one transaction, so a failure part way deletes
// nothing. Their order is the one above: the registration, then the
// tokens, then the rest.
func (s *Store) Delete(ctx context.Context, g grn.GRN) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if _, err := q.LockOperatorRegistration(ctx, g.String()); err != nil {
		return pgxerr.Classify(err, "hold operator", operator.ErrOperatorNotFound, nil)
	}
	if err := q.DeleteOperatorTokens(ctx, g.String()); err != nil {
		return pgxerr.Classify(err, "delete operator tokens", nil, nil)
	}
	// The registration is held FOR UPDATE, so the row is there to
	// delete; an absent one was refused by the hold.
	if _, err := q.DeleteOperator(ctx, g.String()); err != nil {
		return pgxerr.Classify(err, "delete operator", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "commit")
	}
	return nil
}

// Get returns the organization's record of the operator.
func (s *Store) Get(ctx context.Context, g grn.GRN) (*operator.Operator, error) {
	row, err := s.q.GetOperator(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get operator", operator.ErrOperatorNotFound, nil)
	}
	return rowToOperator(row)
}

// ListByOrg returns the organization's operators.
func (s *Store) ListByOrg(ctx context.Context, orgGRN grn.GRN) ([]*operator.Operator, error) {
	rows, err := s.q.ListOperatorsByOrg(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list operators", nil, nil)
	}
	operators := make([]*operator.Operator, 0, len(rows))
	for _, row := range rows {
		o, err := rowToOperator(row)
		if err != nil {
			return nil, err
		}
		operators = append(operators, o)
	}
	return operators, nil
}

// CountByOrg counts the organization's registered operators, which
// deleting the organization removes with it.
func (s *Store) CountByOrg(ctx context.Context, orgGRN grn.GRN) (int, error) {
	n, err := s.q.CountOperatorsByOrg(ctx, orgGRN.String())
	if err != nil {
		return 0, pgxerr.Classify(err, "count operators by org", nil, nil)
	}
	return int(n), nil
}

// rowToOperator converts a sqlc row into the domain aggregate. A stored
// GRN that no longer parses is data corruption, hence internal.
func rowToOperator(row db.Operator) (*operator.Operator, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "grn", errorx.KindInternal)
	}
	orgGRN, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "org grn", errorx.KindInternal)
	}
	// GRNOf is the one derivation of an operator's GRN, so a stored one
	// it does not rebuild names no operator of that organization.
	if want, err := operator.GRNOf(orgGRN, g.Identifier); err != nil || want != g {
		return nil, errorx.New(errorx.KindInternal, "stored operator: grn "+g.String()+" is not an operator of "+orgGRN.String())
	}
	registeredBy, err := grn.Parse(row.RegisteredBy)
	if err != nil {
		return nil, errorx.Wrap(err, "registered by", errorx.KindInternal)
	}
	var endpoint *operator.ControlEndpoint
	if row.ControlEndpoint.Valid {
		e, err := operator.ParseControlEndpoint(row.ControlEndpoint.String)
		if err != nil {
			return nil, errorx.Wrap(err, "control endpoint", errorx.KindInternal)
		}
		endpoint = &e
	}
	return &operator.Operator{
		GRN:             g,
		Organization:    orgGRN,
		RegisteredBy:    registeredBy,
		CreatedAt:       row.CreatedAt.Time,
		Name:            row.Name,
		Description:     row.Description,
		ControlEndpoint: endpoint,
	}, nil
}
