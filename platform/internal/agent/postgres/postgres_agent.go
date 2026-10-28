package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/agent/postgres/db"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// Create persists a new Agent and its execution row, in one
// transaction, so no agent exists without the row the execution fence
// locks (ADR-0080).
func (s *Store) Create(ctx context.Context, a *agent.Agent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if err := q.CreateAgent(ctx, db.CreateAgentParams{
		Grn:               a.GRN.String(),
		OrgGrn:            a.Organization.String(),
		ProvisioningState: string(a.ProvisioningState),
		RuntimeState:      string(a.RuntimeState),
		CreatedAt:         pgxtype.Timestamptz(a.CreatedAt),
		UpdatedAt:         pgxtype.Timestamptz(a.UpdatedAt),
	}); err != nil {
		return pgxerr.Classify(err, "create", nil, agent.ErrAgentAlreadyExists)
	}
	if err := q.CreateAgentExecution(ctx, db.CreateAgentExecutionParams{
		AgentGrn:  a.GRN.String(),
		UpdatedAt: pgxtype.Timestamptz(a.CreatedAt),
	}); err != nil {
		return pgxerr.Classify(err, "create", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "commit")
	}
	return nil
}

// Get returns the Agent with the given GRN, or ErrAgentNotFound.
func (s *Store) Get(ctx context.Context, g grn.GRN) (*agent.Agent, error) {
	row, err := s.q.GetAgent(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "get", agent.ErrAgentNotFound, nil)
	}
	return rowToAgent(row)
}

// ListByOrg returns the organization's agents ordered by GRN.
func (s *Store) ListByOrg(ctx context.Context, orgGRN grn.GRN, f listfilter.Filter) ([]*agent.Agent, error) {
	rows, err := s.q.ListAgentsByOrg(ctx, db.ListAgentsByOrgParams{
		OrgGrn:    orgGRN.String(),
		RowOffset: int32(f.Offset),
		RowLimit:  limit(f.Limit),
	})
	if err != nil {
		return nil, pgxerr.Classify(err, "list agents", nil, nil)
	}
	agents := make([]*agent.Agent, 0, len(rows))
	for _, row := range rows {
		a, err := rowToAgent(row)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// ReportProvisioningState records what the operator assigned at epoch
// reports about the agent's pod.
func (s *Store) ReportProvisioningState(ctx context.Context, agentGRN grn.GRN, epoch int, state agent.ProvisioningState) (*agent.Agent, error) {
	e, err := narrowEpoch(epoch)
	if err != nil {
		return nil, err
	}
	row, err := s.q.ReportAgentProvisioningState(ctx, db.ReportAgentProvisioningStateParams{
		Grn:               agentGRN.String(),
		ProvisioningState: string(state),
		Epoch:             e,
		UpdatedAt:         pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		return nil, s.fenced(ctx, s.q, err, "report provisioning state", agentGRN)
	}
	return rowToAgent(row)
}

// ReportRuntimeState records what the agent assigned at epoch reports
// about itself.
func (s *Store) ReportRuntimeState(ctx context.Context, agentGRN grn.GRN, epoch int, state agent.RuntimeState, fence agent.Fencing) (*agent.Agent, error) {
	e, err := narrowEpoch(epoch)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	answer, err := q.AdmitExecution(ctx, db.AdmitExecutionParams{
		AgentGrn:  fence.Agent.String(),
		TokenHash: fence.TokenBytes(),
		Serial:    fence.Credential.Serial,
		NotBefore: pgxtype.Timestamptz(fence.Credential.NotBefore),
		NotAfter:  pgxtype.Timestamptz(fence.Credential.NotAfter),
	})
	if err != nil {
		return nil, errorx.Wrap(err, "report runtime state: admit", errorx.KindUnavailable)
	}
	if err := agent.Admission(answer).Err(); err != nil {
		return nil, err
	}
	row, err := q.ReportAgentRuntimeState(ctx, db.ReportAgentRuntimeStateParams{
		Grn:          agentGRN.String(),
		RuntimeState: string(state),
		Epoch:        e,
		UpdatedAt:    pgxtype.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		// The answer is read in the same transaction: no second
		// connection is asked for while the fence is held.
		return nil, s.fenced(ctx, q, err, "report runtime state", agentGRN)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, errorx.Wrap(err, "commit", errorx.KindUnavailable)
	}
	return rowToAgent(row)
}

// narrowEpoch converts a reported epoch to the width of the `epoch`
// column. The module converts to a sqlc parameter without checking;
// here the value decides the fence, and one that truncates onto the
// live epoch would be accepted.
func narrowEpoch(epoch int) (int32, error) {
	e := int32(epoch)
	if int(e) != epoch {
		return 0, agent.ErrStaleEpoch
	}
	return e, nil
}

// fenced says why a state report matched no row: the agent is gone, or
// the epoch is not the one it is assigned at.
func (s *Store) fenced(ctx context.Context, q *db.Queries, err error, op string, agentGRN grn.GRN) error {
	if !errors.Is(err, pgx.ErrNoRows) {
		return pgxerr.Classify(err, op, nil, nil)
	}
	if _, getErr := q.GetAgent(ctx, agentGRN.String()); getErr != nil {
		return pgxerr.Classify(getErr, op, agent.ErrAgentNotFound, nil)
	}
	return agent.ErrStaleEpoch
}

// rowToAgent converts a sqlc row into the domain aggregate. A stored
// value that no longer parses is data corruption, hence internal.
func rowToAgent(row db.Agent) (*agent.Agent, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "agent grn", errorx.KindInternal)
	}
	orgGRN, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "agent org grn", errorx.KindInternal)
	}
	if err := agent.ValidateOwnership(g, orgGRN); err != nil {
		return nil, errorx.Wrap(err, "stored agent", errorx.KindInternal)
	}
	provisioning, err := agent.ParseProvisioningState(row.ProvisioningState)
	if err != nil {
		return nil, errorx.Wrap(err, "provisioning state", errorx.KindInternal)
	}
	runtime, err := agent.ParseRuntimeState(row.RuntimeState)
	if err != nil {
		return nil, errorx.Wrap(err, "runtime state", errorx.KindInternal)
	}
	return &agent.Agent{
		GRN:               g,
		Organization:      orgGRN,
		ProvisioningState: provisioning,
		RuntimeState:      runtime,
		CreatedAt:         row.CreatedAt.Time,
		UpdatedAt:         row.UpdatedAt.Time,
		Name:              agent.Name(row.Name.String),
	}, nil
}

// Rename sets or clears the agent's name.
func (s *Store) Rename(ctx context.Context, g grn.GRN, name *agent.Name) (*agent.Agent, error) {
	params := db.RenameAgentParams{Grn: g.String()}
	if name != nil {
		params.Name = pgtype.Text{String: string(*name), Valid: true}
	}
	row, err := s.q.RenameAgent(ctx, params)
	if err != nil {
		return nil, pgxerr.Classify(err, "rename agent", agent.ErrAgentNotFound, nil)
	}
	return rowToAgent(row)
}

// CountByOrg counts the organization's agents, which deleting the
// organization removes with it.
func (s *Store) CountByOrg(ctx context.Context, orgGRN grn.GRN) (agent.OrgCounts, error) {
	count, err := s.q.CountAgentsByOrg(ctx, orgGRN.String())
	if err != nil {
		return agent.OrgCounts{}, pgxerr.Classify(err, "count agents by org", nil, nil)
	}
	return agent.OrgCounts{Agents: int(count)}, nil
}

// Compile-time check that Store satisfies agent.Store.
var _ agent.Store = (*Store)(nil)

// SearchByOrg returns the organization's agents whose identifier or
// name holds n, ordered by GRN.
func (s *Store) SearchByOrg(ctx context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]*agent.Agent, error) {
	rows, err := s.q.SearchAgentsByOrg(ctx, db.SearchAgentsByOrgParams{OrgGrn: orgGRN.String(), Pattern: pgxtype.Contains(n.Text())})
	if err != nil {
		return nil, pgxerr.Classify(err, "search agents", nil, nil)
	}
	found := make([]*agent.Agent, 0, len(rows))
	for _, row := range rows {
		a, err := rowToAgent(row)
		if err != nil {
			return nil, err
		}
		found = append(found, a)
	}
	return found, nil
}

// limit converts a list filter's limit: 0 means "no limit" (SQL NULL).
func limit(n int) pgtype.Int4 {
	if n <= 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(n), Valid: true}
}
