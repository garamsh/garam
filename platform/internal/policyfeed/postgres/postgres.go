// Package postgres implements the policy feed's switch against
// PostgreSQL via jackc/pgx (ADR-0140 §10).
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// Switch implements policyfeed.Switch against PostgreSQL.
type Switch struct {
	pool *pgxpool.Pool
}

// Compile-time check that Switch satisfies policyfeed.Switch.
var _ policyfeed.Switch = (*Switch)(nil)

// New constructs a Postgres-backed switch.
func New(pool *pgxpool.Pool) *Switch {
	return &Switch{pool: pool}
}

// lockSources takes SHARE on every table a trigger watches. A writer
// holds ROW EXCLUSIVE on its table from its statement's start until it
// commits, so this waits out any writer that read the feed as it was
// before the switch, and holds off the next until the switch commits.
const lockSources = `LOCK TABLE agents, group_members, groups, operators,
	message_consents, operator_delegations, org_members, orgs,
	project_members, projects, users, agent_supervisors,
	agent_assignments IN SHARE MODE`

// backfill marks what policy_outbox_track marks for each insert into
// the six tables that own an entity. A member's row marks its user or
// agent, which those tables already name, each organization
// membership marks its role link, each group and project marks its
// scoped link, and each registration that stands marks its supervisor.
const backfill = `
INSERT INTO policy_outbox (entity_type, entity_id)
SELECT t, id FROM (
	SELECT 'Iam::Org' AS t, grn AS id FROM orgs
	UNION ALL SELECT 'Iam::OrgMembers', grn FROM orgs
	UNION ALL SELECT 'Iam::Team', grn FROM groups
	UNION ALL SELECT 'Iam::Project', grn FROM projects
	UNION ALL SELECT 'Iam::User', grn FROM users
	UNION ALL SELECT 'Iam::Agent', grn FROM agents
	UNION ALL SELECT 'Iam::Operator', grn FROM operators
	UNION ALL SELECT 'Iam::Supervisor', grn FROM agent_supervisors
		WHERE ended_at IS NULL
	UNION ALL SELECT 'Iam::RoleLink', org_grn || ' ' || user_grn FROM org_members
	UNION ALL SELECT 'Iam::ScopedRoleLink', grn FROM groups
	UNION ALL SELECT 'Iam::ScopedRoleLink', grn FROM projects
) live
ON CONFLICT (entity_type, entity_id)
DO UPDATE SET generation = policy_outbox.generation + 1,
	set_aside_code = NULL, set_aside_reason = NULL, set_aside_at = NULL`

// Start enables the feed and marks every live entity.
func (s *Switch) Start(ctx context.Context) (int64, error) {
	var marked int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockSources); err != nil {
			return pgxerr.Classify(err, "lock source tables", nil, nil)
		}
		if _, err := tx.Exec(ctx, `UPDATE policy_feed SET enabled = true`); err != nil {
			return pgxerr.Classify(err, "enable feed", nil, nil)
		}
		if _, err := tx.Exec(ctx, backfill); err != nil {
			return pgxerr.Classify(err, "mark live entities", nil, nil)
		}
		err := tx.QueryRow(ctx, `SELECT count(*) FROM policy_outbox`).Scan(&marked)
		return pgxerr.Classify(err, "count marks", nil, nil)
	})
	return marked, errorx.Wrap(err, "start policy feed")
}

// Stop disables the feed and empties the outbox. It takes the same
// locks as Start: a writer that read the feed as on would otherwise
// commit its mark after the delete, into a feed that is off.
func (s *Switch) Stop(ctx context.Context) (int64, error) {
	var removed int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockSources); err != nil {
			return pgxerr.Classify(err, "lock source tables", nil, nil)
		}
		if _, err := tx.Exec(ctx, `UPDATE policy_feed SET enabled = false`); err != nil {
			return pgxerr.Classify(err, "disable feed", nil, nil)
		}
		tag, err := tx.Exec(ctx, `DELETE FROM policy_outbox`)
		if err != nil {
			return pgxerr.Classify(err, "empty outbox", nil, nil)
		}
		removed = tag.RowsAffected()
		return nil
	})
	return removed, errorx.Wrap(err, "stop policy feed")
}
