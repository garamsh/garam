package authz

import (
	"context"
	"time"

	"github.com/garamsh/garam/internal/authz/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
)

// AttachPolicy binds the policy to a subject. The schema rejects an
// attachment that leaves the policy's org.
func (s *PostgresStore) AttachPolicy(ctx context.Context, policyGRN, subjectGRN grn.GRN) (*PolicyAttachment, error) {
	row, err := s.q.AttachPolicy(ctx, db.AttachPolicyParams{
		PolicyGrn:  policyGRN.String(),
		SubjectGrn: subjectGRN.String(),
		CreatedAt:  stamp(time.Now().UTC()),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "attach policy", ErrPolicyNotFound, ErrAttachmentAlreadyExists)
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
func (s *PostgresStore) DetachPolicy(ctx context.Context, policyGRN, subjectGRN grn.GRN) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "authz store: begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	policy, err := q.LockPolicy(ctx, policyGRN.String())
	if err != nil {
		return pgxerr.Classify(err, "detach policy", ErrPolicyNotFound, nil)
	}
	rows, err := q.DetachPolicy(ctx, db.DetachPolicyParams{
		PolicyGrn:  policyGRN.String(),
		SubjectGrn: subjectGRN.String(),
	})
	if err != nil {
		return pgxerr.Classify(err, "detach policy", nil, nil)
	}
	if rows == 0 {
		return ErrAttachmentNotFound
	}
	if policy.Status == string(PolicyStatusActive) {
		remaining, err := q.CountPolicyAttachments(ctx, policyGRN.String())
		if err != nil {
			return pgxerr.Classify(err, "detach policy", nil, nil)
		}
		if remaining == 0 {
			return ErrLastAttachment
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "authz store: commit")
	}
	return nil
}

// ListPolicyAttachments returns a policy's attachments ordered by
// subject GRN.
func (s *PostgresStore) ListPolicyAttachments(ctx context.Context, policyGRN grn.GRN) ([]*PolicyAttachment, error) {
	rows, err := s.q.ListPolicyAttachments(ctx, policyGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list policy attachments", nil, nil)
	}
	attachments := make([]*PolicyAttachment, 0, len(rows))
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
func (s *PostgresStore) ActivePolicies(ctx context.Context, orgGRN grn.GRN) ([]ActivePolicy, error) {
	rows, err := s.q.ActivePolicies(ctx, orgGRN.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list active policies", nil, nil)
	}
	active := make([]ActivePolicy, 0, len(rows))
	for _, row := range rows {
		doc, err := ParsePolicyDocument(row.Document)
		if err != nil {
			return nil, errorx.Wrap(err, "authz store: stored document")
		}
		subjects := make([]grn.GRN, 0, len(row.SubjectGrns))
		for _, raw := range row.SubjectGrns {
			g, err := grn.Parse(raw)
			if err != nil {
				return nil, errorx.Wrap(err, "authz store: attachment subject grn")
			}
			subjects = append(subjects, g)
		}
		active = append(active, ActivePolicy{Document: doc, Subjects: subjects})
	}
	return active, nil
}

func rowToAttachment(row db.PolicyAttachment) (*PolicyAttachment, error) {
	policyGRN, err := grn.Parse(row.PolicyGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "authz store: policy grn")
	}
	subjectGRN, err := grn.Parse(row.SubjectGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "authz store: attachment subject grn")
	}
	return NewPolicyAttachment(policyGRN, subjectGRN, row.CreatedAt.Time), nil
}
