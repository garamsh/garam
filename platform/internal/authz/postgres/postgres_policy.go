package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/authz"
	"github.com/garamsh/garam/platform/internal/authz/postgres/db"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
	"github.com/garamsh/garam/platform/internal/wire"
)

// CreatePolicy stores the policy and its first version in one
// transaction, so a policy never exists without the version it points
// at.
func (s *Store) CreatePolicy(ctx context.Context, p *authz.Policy, doc authz.PolicyDocument, createdBy grn.GRN) (*authz.Policy, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, errorx.Wrap(err, "encode policy document", errorx.KindInvalidArgument)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	row, err := q.CreatePolicy(ctx, db.CreatePolicyParams{
		Grn:            p.GRN.String(),
		OrgGrn:         p.Organization.String(),
		Name:           p.Name,
		Status:         string(p.Status),
		DefaultVersion: int32(p.DefaultVersion),
		CreatedAt:      pgxtype.Timestamptz(p.CreatedAt),
		UpdatedAt:      pgxtype.Timestamptz(p.UpdatedAt),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "create policy", nil, authz.ErrPolicyAlreadyExists)
	}
	if _, err := q.CreateFirstPolicyVersion(ctx, db.CreateFirstPolicyVersionParams{
		PolicyGrn: p.GRN.String(),
		Document:  raw,
		CreatedBy: createdBy.String(),
		CreatedAt: pgxtype.Timestamptz(p.CreatedAt),
	}); err != nil {
		return nil, pgxerr.Classify(err, "create policy version", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errorx.Wrap(err, "commit")
	}
	return rowToPolicy(row)
}

// GetPolicy returns the policy with the given GRN, or
// ErrPolicyNotFound.
func (s *Store) GetPolicy(ctx context.Context, g grn.GRN) (*authz.Policy, error) {
	row, err := s.q.GetPolicy(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get policy", authz.ErrPolicyNotFound, nil)
	}
	return rowToPolicy(row)
}

// ListPolicies returns the org's policies ordered by GRN.
func (s *Store) ListPolicies(ctx context.Context, orgGRN grn.GRN) ([]*authz.Policy, error) {
	rows, err := s.q.ListPolicies(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list policies", nil, nil)
	}
	policies := make([]*authz.Policy, 0, len(rows))
	for _, row := range rows {
		p, err := rowToPolicy(row)
		if err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}
	return policies, nil
}

// SearchPolicies returns the org's policies whose name or identifier
// holds n, ordered by GRN.
func (s *Store) SearchPolicies(ctx context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]*authz.Policy, error) {
	rows, err := s.q.SearchPolicies(ctx, db.SearchPoliciesParams{OrgGrn: orgGRN.String(), Pattern: pgxtype.Contains(n.Text())})
	if err != nil {
		return nil, pgxerr.Classify(err, "search policies", nil, nil)
	}
	policies := make([]*authz.Policy, 0, len(rows))
	for _, row := range rows {
		p, err := rowToPolicy(row)
		if err != nil {
			return nil, err
		}
		policies = append(policies, p)
	}
	return policies, nil
}

// AddPolicyVersion appends a version to an existing policy.
func (s *Store) AddPolicyVersion(ctx context.Context, g grn.GRN, doc authz.PolicyDocument, createdBy grn.GRN) (*authz.PolicyVersion, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, errorx.Wrap(err, "encode policy document", errorx.KindInvalidArgument)
	}
	row, err := s.q.AddPolicyVersion(ctx, db.AddPolicyVersionParams{
		PolicyGrn: g.String(),
		Document:  raw,
		CreatedBy: createdBy.String(),
		CreatedAt: pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "add policy version", authz.ErrPolicyNotFound, nil)
	}
	return rowToPolicyVersion(row)
}

// ListPolicyVersions returns a policy's versions, oldest first.
func (s *Store) ListPolicyVersions(ctx context.Context, g grn.GRN) ([]*authz.PolicyVersion, error) {
	rows, err := s.q.ListPolicyVersions(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list policy versions", nil, nil)
	}
	versions := make([]*authz.PolicyVersion, 0, len(rows))
	for _, row := range rows {
		v, err := rowToPolicyVersion(row)
		if err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, nil
}

// DeletePolicy removes the policy in one statement, which holds the
// policy's row lock as DetachPolicy's does, so the two serialize. Its
// versions and attachments cascade with it, which the append-only
// trigger admits only once the policy row is gone (migration 000002).
func (s *Store) DeletePolicy(ctx context.Context, g grn.GRN) (*authz.Policy, error) {
	row, err := s.q.DeletePolicy(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "delete policy", authz.ErrPolicyNotFound, nil)
	}
	return rowToPolicy(row)
}

// SetPolicyStatus moves the policy through its lifecycle.
func (s *Store) SetPolicyStatus(ctx context.Context, g grn.GRN, status wire.PolicyStatus) (*authz.Policy, error) {
	row, err := s.q.SetPolicyStatus(ctx, db.SetPolicyStatusParams{
		Grn:       g.String(),
		Status:    string(status),
		UpdatedAt: pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "set policy status", authz.ErrPolicyNotFound, nil)
	}
	return rowToPolicy(row)
}

// SetPolicyDefaultVersion points the policy at an already-stored
// version; rollback is exactly this pointer move (ADR-0002).
func (s *Store) SetPolicyDefaultVersion(ctx context.Context, g grn.GRN, version int) (*authz.Policy, error) {
	if _, err := s.q.GetPolicyVersion(ctx, db.GetPolicyVersionParams{
		PolicyGrn: g.String(),
		Version:   int32(version),
	}); err != nil {
		return nil, pgxerr.Classify(err, "get policy version", authz.ErrPolicyVersionNotFound, nil)
	}
	row, err := s.q.SetPolicyDefaultVersion(ctx, db.SetPolicyDefaultVersionParams{
		Grn:            g.String(),
		DefaultVersion: int32(version),
		UpdatedAt:      pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "set policy default version", authz.ErrPolicyNotFound, nil)
	}
	return rowToPolicy(row)
}

// rowToPolicy converts a sqlc row into the domain aggregate. A stored
// value that no longer parses is data corruption, hence internal: the
// parsers below answer the kind a caller's own input earns, and this
// value passed that boundary before it was stored.
func rowToPolicy(row db.Policy) (*authz.Policy, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "policy grn", errorx.KindInternal)
	}
	orgGRN, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "policy org grn", errorx.KindInternal)
	}
	status, err := authz.ParsePolicyStatus(row.Status)
	if err != nil {
		return nil, errorx.Wrap(err, "policy status", errorx.KindInternal)
	}
	return &authz.Policy{
		GRN:            g,
		Organization:   orgGRN,
		Name:           row.Name,
		Status:         status,
		DefaultVersion: int(row.DefaultVersion),
		CreatedAt:      row.CreatedAt.Time,
		UpdatedAt:      row.UpdatedAt.Time,
	}, nil
}

func rowToPolicyVersion(row db.PolicyVersion) (*authz.PolicyVersion, error) {
	g, err := grn.Parse(row.PolicyGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "policy grn", errorx.KindInternal)
	}
	createdBy, err := grn.Parse(row.CreatedBy)
	if err != nil {
		return nil, errorx.Wrap(err, "policy version author", errorx.KindInternal)
	}
	doc, err := authz.ParsePolicyDocument(row.Document)
	if err != nil {
		return nil, errorx.Wrap(err, "stored document", errorx.KindInternal)
	}
	return &authz.PolicyVersion{Policy: g, Version: int(row.Version), Document: doc, CreatedBy: createdBy, CreatedAt: row.CreatedAt.Time}, nil
}
