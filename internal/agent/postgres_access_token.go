package agent

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/agent/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxerr"
)

// Compile-time check that PostgresStore satisfies AccessTokenStore.
var _ AccessTokenStore = (*PostgresStore)(nil)

// MintAccessToken holds the agent's execution row FOR SHARE, reads the
// assignment and the cutover after it, decides, and records the token
// with the epoch and lineage it read, in one transaction. A
// reassignment or recovery committing after the read waits for the
// mint; one committing before it is what the mint is decided on.
func (s *PostgresStore) MintAccessToken(ctx context.Context, caller grn.GRN, t *AccessToken) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	execution, err := q.ShareAgentLineage(ctx, t.Agent.String())
	if err != nil {
		return pgxerr.Classify(err, "agent execution", ErrNotAssigned, nil)
	}
	assignment, err := q.GetAgentAssignment(ctx, t.Agent.String())
	if err != nil {
		return pgxerr.Classify(err, "agent assignment", ErrNotAssigned, nil)
	}
	if assignment.AssigneeGrn != caller.String() {
		return ErrNotAssigned
	}
	if !execution.Lineage.Valid {
		mode, err := ParseMode(execution.Mode)
		if err != nil {
			return errorx.Wrap(err, "stored execution mode", errorx.KindInternal)
		}
		if mode == ModeFenced {
			return ErrIssuedByLifecycle
		}
		cutOver, err := q.IsAgentCutOver(ctx, t.Agent.String())
		if err != nil {
			return pgxerr.Classify(err, "cutover", nil, nil)
		}
		if cutOver {
			return ErrCutOver
		}
	}

	t.Assignee = caller
	t.Epoch = int(assignment.Epoch)
	t.Lineage = nil
	if execution.Lineage.Valid {
		lineage := Lineage(uuid.UUID(execution.Lineage.Bytes))
		t.Lineage = &lineage
	}
	audience := pgtype.Text{}
	if t.Audience != nil {
		audience = pgtype.Text{String: t.Audience.String(), Valid: true}
	}
	if err := q.RecordAgentToken(ctx, db.RecordAgentTokenParams{
		TokenHash:   t.Hash(),
		Serial:      t.Serial(),
		AgentGrn:    t.Agent.String(),
		AssigneeGrn: caller.String(),
		Epoch:       assignment.Epoch,
		Lineage:     execution.Lineage,
		AudienceGrn: audience,
		IssuedAt:    pgtype.Timestamptz{Time: t.IssuedAt, Valid: true},
		ExpiresAt:   pgtype.Timestamptz{Time: t.ExpiresAt, Valid: true},
	}); err != nil {
		// The execution row is held, so the one key a missing row can
		// refuse here is the audience's: a hosted operator deleted
		// since the service read its registration.
		return pgxerr.Classify(err, "record agent token", ErrAccessTokenAudience, nil)
	}
	if err := q.TrimAgentTokens(ctx, db.TrimAgentTokensParams{
		AgentGrn: t.Agent.String(), AudienceGrn: audience, Keep: LiveAccessTokensPerAudience,
	}); err != nil {
		return pgxerr.Classify(err, "trim agent tokens", nil, nil)
	}
	if err := q.TrimAgentTokensTotal(ctx, db.TrimAgentTokensTotalParams{
		AgentGrn: t.Agent.String(), Keep: LiveAccessTokensPerAgent,
	}); err != nil {
		return pgxerr.Classify(err, "trim agent tokens", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "commit")
	}
	return nil
}

// ResolveAccessToken answers what a live token names. The statement
// joins the current assignment and execution, so a reassignment, a
// recovery, a deleted agent and an expiry are one answer: none.
func (s *PostgresStore) ResolveAccessToken(ctx context.Context, tokenHash string) (AccessTokenHolder, error) {
	row, err := s.q.ResolveAgentToken(ctx, tokenHash)
	if err != nil {
		return AccessTokenHolder{}, pgxerr.Classify(err, "resolve agent token", ErrAccessTokenInvalid, nil)
	}
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return AccessTokenHolder{}, errorx.Wrap(err, "stored agent grn", errorx.KindInternal)
	}
	holder := AccessTokenHolder{Agent: agentGRN, Serial: row.Serial,
		IssuedAt: row.IssuedAt.Time, ExpiresAt: row.ExpiresAt.Time}
	if row.AudienceGrn.Valid {
		audience, err := grn.Parse(row.AudienceGrn.String)
		if err != nil {
			return AccessTokenHolder{}, errorx.Wrap(err, "stored audience grn", errorx.KindInternal)
		}
		holder.Audience = &audience
	}
	return holder, nil
}
