package postgres

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/agent/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// inSnapshot runs fn in one read-only repeatable-read transaction, so
// every statement it issues reads the same snapshot.
func (s *Store) inSnapshot(ctx context.Context, op string, fn func(q *db.Queries) error) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return errorx.Wrap(err, op+": begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return fn(s.q.WithTx(tx))
}

// source is an agent's cutover source as stored: the operator its
// digest covers, the definition if one parses, and the digest.
type source struct {
	operator grn.GRN
	parsed   bool
	def      *agent.CutoverSource
	digest   *agent.Digest
}

// sourceOf reads the agent's source from its definition row, or from
// none: an agent with no definition has empty values under its
// assignee (ADR-0086). A payload that does not parse is preserved and
// has no digest.
func sourceOf(agentGRN, assignee grn.GRN, row *db.AgentDefinition) (source, error) {
	if row == nil {
		d := agent.SourceDigest(agentGRN, assignee, agent.Values{})
		return source{operator: assignee, parsed: true, digest: &d}, nil
	}
	operator, err := grn.Parse(row.OperatorGrn)
	if err != nil {
		return source{}, errorx.Wrap(err, "stored definition operator", errorx.KindInternal)
	}
	stored := agent.ReadStoredValues(row.AgentValues)
	if stored.Values == nil {
		return source{operator: operator}, nil
	}
	definedBy, err := grn.Parse(row.DefinedBy)
	if err != nil {
		return source{}, errorx.Wrap(err, "stored definition author", errorx.KindInternal)
	}
	d := agent.SourceDigest(agentGRN, operator, stored.Values)
	return source{operator: operator, parsed: true, digest: &d, def: &agent.CutoverSource{
		Operator: operator, Values: stored.Values, DefinedBy: definedBy, CreatedAt: row.CreatedAt.Time,
	}}, nil
}

// definitionOf answers a read of the agent's definition row, nil where
// there is none.
func definitionOf(row db.AgentDefinition, err error) (*db.AgentDefinition, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, storageError(err, "read definition", nil, nil)
	}
	return &row, nil
}

func (s *Store) GetCutover(ctx context.Context, agentGRN grn.GRN) (*agent.Cutover, error) {
	var out *agent.Cutover
	err := s.inSnapshot(ctx, "get cutover", func(q *db.Queries) error {
		row, err := q.GetCutoverAgent(ctx, agentGRN.String())
		if err != nil {
			return storageError(err, "read cutover agent", agent.ErrNotAssigned, nil)
		}
		mode, err := agent.ParseMode(row.Mode)
		if err != nil {
			return errorx.Wrap(err, "stored execution mode", errorx.KindInternal)
		}
		assignee, err := grn.Parse(row.AssigneeGrn)
		if err != nil {
			return errorx.Wrap(err, "stored assignee", errorx.KindInternal)
		}
		def, err := definitionOf(q.GetCutoverSource(ctx, agentGRN.String()))
		if err != nil {
			return err
		}
		src, err := sourceOf(agentGRN, assignee, def)
		if err != nil {
			return err
		}
		out = &agent.Cutover{
			Agent: agentGRN, Assignee: assignee, Epoch: int(row.Epoch), Mode: mode,
			Class: agent.ClassOf(mode, src.parsed), Source: src.def, SourceDigest: src.digest,
		}
		latest, err := q.GetLatestCutover(ctx, agentGRN.String())
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return nil
		case err != nil:
			return storageError(err, "read cutover attempt", nil, nil)
		}
		out.Attempt, err = attemptOf(latest)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// lockCutover takes the locks every cutover stage takes, in the order
// they are taken: the execution row FOR UPDATE, then the definition
// row, the lock a claim serializes on. It rechecks the authority under
// them and answers the execution, the definition (nil where the agent
// has none), the epoch and the instant.
func lockCutover(ctx context.Context, q *db.Queries, req agent.CutoverRequest) (db.AgentExecution, *db.AgentDefinition, int, time.Time, error) {
	exec, err := lockExecution(ctx, q, req.Agent)
	if err != nil {
		return db.AgentExecution{}, nil, 0, time.Time{}, err
	}
	def, err := definitionOf(q.LockDefinition(ctx, req.Agent.String()))
	if err != nil {
		return db.AgentExecution{}, nil, 0, time.Time{}, err
	}
	epoch, now, err := recheck(ctx, q, req.Agent, req.Authority)
	if err != nil {
		return db.AgentExecution{}, nil, 0, time.Time{}, err
	}
	return exec, def, epoch, now, nil
}

func (s *Store) Freeze(ctx context.Context, req agent.FreezeRequest) (*agent.CutoverAttempt, bool, error) {
	var (
		out     *agent.CutoverAttempt
		created bool
	)
	err := s.inTx(ctx, "freeze cutover", func(q *db.Queries) error {
		exec, def, epoch, now, err := lockCutover(ctx, q, req.CutoverRequest)
		if err != nil {
			return err
		}
		if epoch != req.Epoch {
			return agent.ErrStaleEpoch
		}
		prior, err := attemptNamed(ctx, q, req.CutoverRequest)
		if err != nil {
			return err
		}
		if prior != nil {
			if prior.Stage == string(agent.CutoverRolledBack) {
				return agent.ErrCutoverAttemptEnded
			}
			if !bytes.Equal(prior.FrozenDigest, req.SourceDigest[:]) {
				return agent.ErrCutoverDigestMismatch
			}
			out, err = attemptOf(*prior)
			return err
		}
		if _, err := q.GetOpenCutover(ctx, req.Agent.String()); err == nil {
			return agent.ErrCutoverAttemptOpen
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return storageError(err, "read open cutover", nil, nil)
		}
		if exec.Mode != string(agent.ModeLegacy) {
			return agent.ErrCutoverNotEligible
		}
		src, err := sourceOf(req.Agent, req.Authority.Assignee, def)
		if err != nil {
			return err
		}
		if !src.parsed {
			return agent.ErrCutoverNotEligible
		}
		// The digest is of the rows under the lock, so nothing the
		// Operator imported can differ from what is frozen.
		if *src.digest != req.SourceDigest {
			return agent.ErrCutoverDigestMismatch
		}
		row, err := q.InsertCutover(ctx, db.InsertCutoverParams{
			AgentGrn:     req.Agent.String(),
			ImportID:     string(req.ImportID),
			FrozenDigest: req.SourceDigest[:],
			AudienceGrn:  req.Audience.String(),
			GrantID:      req.GrantID,
			OperationRef: req.OperationRef,
			CreatedAt:    stamp(now),
		})
		if err != nil {
			return storageError(err, "insert cutover", nil, agent.ErrCutoverAttemptOpen)
		}
		created = true
		out, err = attemptOf(row)
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return out, created, nil
}

func (s *Store) Switch(ctx context.Context, req agent.CutoverRequest) (*agent.CutoverAttempt, error) {
	var out *agent.CutoverAttempt
	err := s.inTx(ctx, "switch cutover", func(q *db.Queries) error {
		_, _, _, now, err := lockCutover(ctx, q, req)
		if err != nil {
			return err
		}
		prior, err := knownAttempt(ctx, q, req)
		if err != nil {
			return err
		}
		switch agent.CutoverStage(prior.Stage) {
		case agent.CutoverSwitched:
			out, err = attemptOf(*prior)
			return err
		case agent.CutoverRolledBack:
			return agent.ErrCutoverAttemptEnded
		}
		row, err := q.SwitchCutover(ctx, db.SwitchCutoverParams{
			AgentGrn: req.Agent.String(), ImportID: string(req.ImportID), SwitchedAt: stamp(now),
		})
		if err != nil {
			return storageError(err, "switch cutover", nil, nil)
		}
		out, err = attemptOf(row)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) RollBack(ctx context.Context, req agent.CutoverRequest) (*agent.CutoverAttempt, error) {
	var out *agent.CutoverAttempt
	err := s.inTx(ctx, "roll back cutover", func(q *db.Queries) error {
		_, _, _, now, err := lockCutover(ctx, q, req)
		if err != nil {
			return err
		}
		prior, err := knownAttempt(ctx, q, req)
		if err != nil {
			return err
		}
		switch agent.CutoverStage(prior.Stage) {
		case agent.CutoverRolledBack:
			return agent.ErrCutoverAttemptEnded
		case agent.CutoverSwitched:
			return agent.ErrReverseMigrationRequired
		}
		row, err := q.RollBackCutover(ctx, db.RollBackCutoverParams{
			AgentGrn: req.Agent.String(), ImportID: string(req.ImportID), EndedAt: stamp(now),
		})
		if err != nil {
			return storageError(err, "roll back cutover", nil, nil)
		}
		out, err = attemptOf(row)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// attemptNamed answers the agent's attempt under req's import, nil
// where it never froze one.
func attemptNamed(ctx context.Context, q *db.Queries, req agent.CutoverRequest) (*db.AgentCutover, error) {
	row, err := q.GetCutoverAttempt(ctx, db.GetCutoverAttemptParams{
		AgentGrn: req.Agent.String(), ImportID: string(req.ImportID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, storageError(err, "read cutover attempt", nil, nil)
	}
	return &row, nil
}

// knownAttempt answers the attempt a switch or a rollback names. One
// the agent never froze is not found, unless another attempt is open:
// then the request names the wrong one.
func knownAttempt(ctx context.Context, q *db.Queries, req agent.CutoverRequest) (*db.AgentCutover, error) {
	prior, err := attemptNamed(ctx, q, req)
	if err != nil || prior != nil {
		return prior, err
	}
	if _, err := q.GetOpenCutover(ctx, req.Agent.String()); err == nil {
		return nil, agent.ErrCutoverAttemptOpen
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, storageError(err, "read open cutover", nil, nil)
	}
	return nil, agent.ErrCutoverAttemptNotFound
}

func attemptOf(row db.AgentCutover) (*agent.CutoverAttempt, error) {
	stage, err := agent.ParseCutoverStage(row.Stage)
	if err != nil {
		return nil, errorx.Wrap(err, "stored cutover stage", errorx.KindInternal)
	}
	if len(row.FrozenDigest) != len(agent.Digest{}) {
		return nil, errorx.New(errorx.KindInternal, "stored frozen digest is not 32 bytes")
	}
	a := &agent.CutoverAttempt{
		ImportID:   agent.ImportID(row.ImportID),
		Stage:      stage,
		CreatedAt:  row.CreatedAt.Time,
		SwitchedAt: optionalTime(row.SwitchedAt),
		EndedAt:    optionalTime(row.EndedAt),
	}
	copy(a.FrozenDigest[:], row.FrozenDigest)
	return a, nil
}

func optionalTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}
