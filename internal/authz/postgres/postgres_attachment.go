package postgres

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/authz"
	"github.com/garamsh/garam/internal/authz/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/pgxtype"
	"github.com/garamsh/garam/internal/wire"
)

// AttachPolicy binds the policy to a subject. The schema rejects an
// attachment that leaves the policy's org.
func (s *Store) AttachPolicy(ctx context.Context, policyGRN, subjectGRN grn.GRN) (*authz.PolicyAttachment, error) {
	row, err := s.q.AttachPolicy(ctx, db.AttachPolicyParams{
		PolicyGrn:  policyGRN.String(),
		SubjectGrn: subjectGRN.String(),
		CreatedAt:  pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "attach policy", authz.ErrPolicyNotFound, authz.ErrAttachmentAlreadyExists)
	}
	return rowToAttachment(row)
}

// DetachPolicy removes one attachment. The delete runs in a
// transaction holding the policy's row lock, so two concurrent
// detaches of a policy's last two attachments serialize: the second
// counts what the first removed and is the one refused. Removing an
// active policy's last attachment is refused rather than widening the
// policy org-wide; the rollback undoes the delete, and its change
// notification with it.
func (s *Store) DetachPolicy(ctx context.Context, policyGRN, subjectGRN grn.GRN) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	policy, err := q.LockPolicy(ctx, policyGRN.String())
	if err != nil {
		return pgxerr.Classify(err, "detach policy", authz.ErrPolicyNotFound, nil)
	}
	rows, err := q.DetachPolicy(ctx, db.DetachPolicyParams{
		PolicyGrn:  policyGRN.String(),
		SubjectGrn: subjectGRN.String(),
	})
	if err != nil {
		return pgxerr.Classify(err, "detach policy", nil, nil)
	}
	if rows == 0 {
		return authz.ErrAttachmentNotFound
	}
	if policy.Status == string(wire.PolicyStatusActive) {
		remaining, err := q.CountPolicyAttachments(ctx, policyGRN.String())
		if err != nil {
			return pgxerr.Classify(err, "detach policy", nil, nil)
		}
		if remaining == 0 {
			return authz.ErrLastAttachment
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "commit")
	}
	return nil
}

// ListPolicyAttachments returns a policy's attachments ordered by
// subject GRN.
func (s *Store) ListPolicyAttachments(ctx context.Context, policyGRN grn.GRN) ([]*authz.PolicyAttachment, error) {
	rows, err := s.q.ListPolicyAttachments(ctx, policyGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list policy attachments", nil, nil)
	}
	attachments := make([]*authz.PolicyAttachment, 0, len(rows))
	for _, row := range rows {
		a, err := rowToAttachment(row)
		if err != nil {
			return nil, err
		}
		attachments = append(attachments, a)
	}
	return attachments, nil
}

// ActivePolicies returns what each active policy of the org evaluates
// at its default version, with the subjects it is attached to.
func (s *Store) ActivePolicies(ctx context.Context, orgGRN grn.GRN) ([]authz.ActivePolicy, error) {
	rows, err := s.q.ActivePolicies(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list active policies", nil, nil)
	}
	active := make([]authz.ActivePolicy, 0, len(rows))
	for _, row := range rows {
		p, err := rowToActivePolicy(row)
		if err != nil {
			return nil, err
		}
		active = append(active, p)
	}
	return active, nil
}

func rowToActivePolicy(row db.ActivePoliciesRow) (authz.ActivePolicy, error) {
	policyGRN, err := grn.Parse(row.Grn)
	if err != nil {
		return authz.ActivePolicy{}, errorx.Wrap(err, "policy grn", errorx.KindInternal)
	}
	doc, err := authz.ParsePolicyDocument(row.Document)
	if err != nil {
		return authz.ActivePolicy{}, errorx.Wrap(err, "stored document", errorx.KindInternal)
	}
	subjects := make([]grn.GRN, 0, len(row.SubjectGrns))
	for _, raw := range row.SubjectGrns {
		g, err := grn.Parse(raw)
		if err != nil {
			return authz.ActivePolicy{}, errorx.Wrap(err, "attachment subject grn", errorx.KindInternal)
		}
		subjects = append(subjects, g)
	}
	return authz.ActivePolicy{GRN: policyGRN, Document: doc, Subjects: subjects}, nil
}

func rowToAttachment(row db.PolicyAttachment) (*authz.PolicyAttachment, error) {
	policyGRN, err := grn.Parse(row.PolicyGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "policy grn", errorx.KindInternal)
	}
	subjectGRN, err := grn.Parse(row.SubjectGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "attachment subject grn", errorx.KindInternal)
	}
	return &authz.PolicyAttachment{Policy: policyGRN, Subject: subjectGRN, CreatedAt: row.CreatedAt.Time}, nil
}
