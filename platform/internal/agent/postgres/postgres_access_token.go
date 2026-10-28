package postgres

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/agent/postgres/db"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// Compile-time checks that Store satisfies AccessTokenStore and
// ShownTokenReader.
var (
	_ agent.AccessTokenStore = (*Store)(nil)
	_ agent.ShownTokenReader = (*Store)(nil)
)

// MintAccessToken holds the agent's execution row FOR SHARE, reads the
// assignment after it, decides, and records the token
// with the epoch and lineage it read, in one transaction. A
// reassignment or recovery committing after the read waits for the
// mint; one committing before it is what the mint is decided on.
//
// It records the token before deleting any, so the audience's
// registration, which the insert's foreign key holds, is waited on
// holding no token another transaction could want; the trim after it
// is the one statement that waits on tokens (#1434).
func (s *Store) MintAccessToken(ctx context.Context, caller grn.GRN, t *agent.AccessToken) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	lineage, err := q.ShareAgentLineage(ctx, t.Agent.String())
	if err != nil {
		return pgxerr.Classify(err, "agent execution", agent.ErrNotAssigned, nil)
	}
	assignment, err := q.GetAgentAssignment(ctx, t.Agent.String())
	if err != nil {
		return pgxerr.Classify(err, "agent assignment", agent.ErrNotAssigned, nil)
	}
	if assignment.AssigneeGrn != caller.String() {
		return agent.ErrNotAssigned
	}
	if !lineage.Valid {
		return agent.ErrIssuedByLifecycle
	}

	t.Assignee = caller
	t.Epoch = int(assignment.Epoch)
	current := agent.Lineage(uuid.UUID(lineage.Bytes))
	t.Lineage = &current
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
		Lineage:     lineage,
		AudienceGrn: audience,
		IssuedAt:    pgxtype.Timestamptz(t.IssuedAt),
		ExpiresAt:   pgxtype.Timestamptz(t.ExpiresAt),
	}); err != nil {
		// The execution row is held, so the one key a missing row can
		// refuse here is the audience's: a hosted operator deleted
		// since the service read its registration.
		return pgxerr.Classify(err, "record agent token", agent.ErrAccessTokenAudience, nil)
	}
	if err := q.TrimAgentTokens(ctx, db.TrimAgentTokensParams{
		AgentGrn: t.Agent.String(), AudienceGrn: audience,
		KeepPerAudience: agent.LiveAccessTokensPerAudience, KeepInAll: agent.LiveAccessTokensPerAgent,
	}); err != nil {
		return pgxerr.Classify(err, "trim agent tokens", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "commit")
	}
	return nil
}

// AccessTokenShownTo reads the token's row alone, by its hash and the
// audience it was minted to be shown to.
func (s *Store) AccessTokenShownTo(ctx context.Context, tokenHash string, audience grn.GRN) (agent.AccessTokenHolder, error) {
	row, err := s.q.GetAgentTokenShownTo(ctx, db.GetAgentTokenShownToParams{
		TokenHash: tokenHash, CallerGrn: pgtype.Text{String: audience.String(), Valid: true},
	})
	if err != nil {
		return agent.AccessTokenHolder{}, pgxerr.Classify(err, "agent token shown to an audience", agent.ErrTokenNotProven, nil)
	}
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return agent.AccessTokenHolder{}, errorx.Wrap(err, "stored agent grn", errorx.KindInternal)
	}
	holder := agent.AccessTokenHolder{Agent: agentGRN, Serial: row.Serial, IssuedAt: row.IssuedAt.Time, ExpiresAt: row.ExpiresAt.Time}
	if row.AudienceGrn.Valid {
		holder.Audience = &audience
	}
	return holder, nil
}

// ResolveAccessToken answers what a live token names. The statement
// joins the current assignment and execution, so a reassignment, a
// recovery, a deleted agent and an expiry are one answer: none.
func (s *Store) ResolveAccessToken(ctx context.Context, tokenHash string) (agent.AccessTokenHolder, error) {
	row, err := s.q.ResolveAgentToken(ctx, tokenHash)
	if err != nil {
		return agent.AccessTokenHolder{}, pgxerr.Classify(err, "resolve agent token", agent.ErrAccessTokenInvalid, nil)
	}
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return agent.AccessTokenHolder{}, errorx.Wrap(err, "stored agent grn", errorx.KindInternal)
	}
	holder := agent.AccessTokenHolder{Agent: agentGRN, Serial: row.Serial,
		IssuedAt: row.IssuedAt.Time, ExpiresAt: row.ExpiresAt.Time}
	if row.AudienceGrn.Valid {
		audience, err := grn.Parse(row.AudienceGrn.String)
		if err != nil {
			return agent.AccessTokenHolder{}, errorx.Wrap(err, "stored audience grn", errorx.KindInternal)
		}
		holder.Audience = &audience
	}
	return holder, nil
}
