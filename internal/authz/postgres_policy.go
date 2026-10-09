package authz

import (
	"context"
	"encoding/json"
	"time"

	"github.com/garamsh/garam/internal/authz/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/wire"
)

// CreatePolicy stores the policy and its first version in one
// transaction, so a policy never exists without the version it points
// at.
func (s *PostgresStore) CreatePolicy(ctx context.Context, p *Policy, doc PolicyDocument, createdBy grn.GRN) (*Policy, error) {
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
		Grn:            p.grn.String(),
		OrgGrn:         p.orgGRN.String(),
		Name:           p.name,
		Status:         string(p.status),
		DefaultVersion: int32(p.defaultVersion),
		CreatedAt:      stamp(p.createdAt),
		UpdatedAt:      stamp(p.updatedAt),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "create policy", nil, ErrPolicyAlreadyExists)
	}
	if _, err := q.CreateFirstPolicyVersion(ctx, db.CreateFirstPolicyVersionParams{
		PolicyGrn: p.grn.String(),
		Document:  raw,
		CreatedBy: createdBy.String(),
		CreatedAt: stamp(p.createdAt),
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
func (s *PostgresStore) GetPolicy(ctx context.Context, g grn.GRN) (*Policy, error) {
	row, err := s.q.GetPolicy(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get policy", ErrPolicyNotFound, nil)
	}
	return rowToPolicy(row)
}

// ListPolicies returns the org's policies ordered by GRN.
func (s *PostgresStore) ListPolicies(ctx context.Context, orgGRN grn.GRN) ([]*Policy, error) {
	rows, err := s.q.ListPolicies(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list policies", nil, nil)
	}
	policies := make([]*Policy, 0, len(rows))
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
func (s *PostgresStore) AddPolicyVersion(ctx context.Context, g grn.GRN, doc PolicyDocument, createdBy grn.GRN) (*PolicyVersion, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, errorx.Wrap(err, "encode policy document", errorx.KindInvalidArgument)
	}
	row, err := s.q.AddPolicyVersion(ctx, db.AddPolicyVersionParams{
		PolicyGrn: g.String(),
		Document:  raw,
		CreatedBy: createdBy.String(),
		CreatedAt: stamp(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "add policy version", ErrPolicyNotFound, nil)
	}
	return rowToPolicyVersion(row)
}

// ListPolicyVersions returns a policy's versions, oldest first.
func (s *PostgresStore) ListPolicyVersions(ctx context.Context, g grn.GRN) ([]*PolicyVersion, error) {
	rows, err := s.q.ListPolicyVersions(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list policy versions", nil, nil)
	}
	versions := make([]*PolicyVersion, 0, len(rows))
	for _, row := range rows {
		v, err := rowToPolicyVersion(row)
		if err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, nil
}

// SetPolicyStatus moves the policy through its lifecycle.
func (s *PostgresStore) SetPolicyStatus(ctx context.Context, g grn.GRN, status wire.PolicyStatus) (*Policy, error) {
	row, err := s.q.SetPolicyStatus(ctx, db.SetPolicyStatusParams{
		Grn:       g.String(),
		Status:    string(status),
		UpdatedAt: stamp(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "set policy status", ErrPolicyNotFound, nil)
	}
	return rowToPolicy(row)
}

// SetPolicyDefaultVersion points the policy at an already-stored
// version; rollback is exactly this pointer move (ADR-0002).
func (s *PostgresStore) SetPolicyDefaultVersion(ctx context.Context, g grn.GRN, version int) (*Policy, error) {
	if _, err := s.q.GetPolicyVersion(ctx, db.GetPolicyVersionParams{
		PolicyGrn: g.String(),
		Version:   int32(version),
	}); err != nil {
		return nil, pgxerr.Classify(err, "get policy version", ErrPolicyVersionNotFound, nil)
	}
	row, err := s.q.SetPolicyDefaultVersion(ctx, db.SetPolicyDefaultVersionParams{
		Grn:            g.String(),
		DefaultVersion: int32(version),
		UpdatedAt:      stamp(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "set policy default version", ErrPolicyNotFound, nil)
	}
	return rowToPolicy(row)
}

// rowToPolicy converts a sqlc row into the domain aggregate. A stored
// value that no longer parses is data corruption, hence internal: the
// parsers below answer the kind a caller's own input earns, and this
// value passed that boundary before it was stored.
func rowToPolicy(row db.Policy) (*Policy, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "policy grn", errorx.KindInternal)
	}
	orgGRN, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "policy org grn", errorx.KindInternal)
	}
	status, err := ParsePolicyStatus(row.Status)
	if err != nil {
		return nil, errorx.Wrap(err, "policy status", errorx.KindInternal)
	}
	return &Policy{
		grn:            g,
		orgGRN:         orgGRN,
		name:           row.Name,
		status:         status,
		defaultVersion: int(row.DefaultVersion),
		createdAt:      row.CreatedAt.Time,
		updatedAt:      row.UpdatedAt.Time,
	}, nil
}

func rowToPolicyVersion(row db.PolicyVersion) (*PolicyVersion, error) {
	g, err := grn.Parse(row.PolicyGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "policy grn", errorx.KindInternal)
	}
	createdBy, err := grn.Parse(row.CreatedBy)
	if err != nil {
		return nil, errorx.Wrap(err, "policy version author", errorx.KindInternal)
	}
	doc, err := ParsePolicyDocument(row.Document)
	if err != nil {
		return nil, errorx.Wrap(err, "stored document", errorx.KindInternal)
	}
	return NewPolicyVersion(g, int(row.Version), doc, createdBy, row.CreatedAt.Time), nil
}
