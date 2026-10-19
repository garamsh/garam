package postgres

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/agent/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxtype"
)

// Compile-time check that Store satisfies agent.StandaloneStore.
var _ agent.StandaloneStore = (*Store)(nil)

func (s *Store) Approve(ctx context.Context, e agent.Enrollment) (*agent.Enrollment, bool, error) {
	params := db.InsertEnrollmentParams{
		ID:            pgUUID(uuid.UUID(e.ID)),
		OrgGrn:        e.Org.String(),
		Placement:     string(e.Placement),
		AgentGrn:      e.Agent.String(),
		SupervisorGrn: e.Supervisor.String(),
		HostName:      string(e.HostName),
		RequestID:     string(e.RequestID),
		ApprovedBy:    e.ApprovedBy.String(),
		SecretSha256:  e.SecretSHA256[:],
		ExpiresAt:     pgxtype.Timestamptz(e.ExpiresAt),
		CreatedAt:     pgxtype.Timestamptz(e.CreatedAt),
	}
	if r := e.Replaces; r != nil {
		params.ExpectedAssigneeGrn = pgtype.Text{String: r.Assignee.String(), Valid: true}
		params.ExpectedEpoch = pgtype.Int4{Int32: int32(r.Epoch), Valid: true}
	}
	inserted, err := s.q.InsertEnrollment(ctx, params)
	if err != nil {
		return nil, false, storageError(err, "insert enrollment", nil, agent.ErrEnrollmentConflict)
	}
	if inserted == 1 {
		return &e, true, nil
	}
	row, err := s.q.GetEnrollmentByRequest(ctx, db.GetEnrollmentByRequestParams{OrgGrn: e.Org.String(), RequestID: string(e.RequestID)})
	if err != nil {
		return nil, false, storageError(err, "read enrollment", nil, nil)
	}
	stored, err := enrollmentOf(row)
	return stored, false, err
}

func (s *Store) GetEnrollment(ctx context.Context, org grn.GRN, id agent.EnrollmentID) (*agent.Enrollment, error) {
	row, err := s.q.GetEnrollment(ctx, db.GetEnrollmentParams{OrgGrn: org.String(), ID: pgUUID(uuid.UUID(id))})
	if err != nil {
		return nil, storageError(err, "read enrollment", agent.ErrEnrollmentNotFound, nil)
	}
	return enrollmentOf(row)
}

func (s *Store) GetEnrollmentBySecret(ctx context.Context, secretSHA256 [32]byte) (*agent.Enrollment, error) {
	row, err := s.q.GetEnrollmentBySecret(ctx, secretSHA256[:])
	if err != nil {
		return nil, storageError(err, "read enrollment", agent.ErrBootstrapRefused, nil)
	}
	return enrollmentOf(row)
}

func (s *Store) Withdraw(ctx context.Context, org grn.GRN, id agent.EnrollmentID) error {
	return s.inTx(ctx, "withdraw enrollment", func(q *db.Queries) error {
		row, err := q.LockEnrollment(ctx, pgUUID(uuid.UUID(id)))
		if err != nil || row.OrgGrn != org.String() {
			return storageError(orNoRows(err), "lock enrollment", agent.ErrEnrollmentNotFound, nil)
		}
		if row.ConsumedAt.Valid {
			return agent.ErrEnrollmentConflict
		}
		now, err := q.Now(ctx)
		if err != nil {
			return storageError(err, "read clock", nil, nil)
		}
		return storageError(q.WithdrawEnrollment(ctx, db.WithdrawEnrollmentParams{ID: row.ID, WithdrawnAt: now}),
			"withdraw enrollment", nil, nil)
	})
}

func (s *Store) Enroll(ctx context.Context, req agent.EnrollmentRequest) (*agent.EnrollmentResult, bool, error) {
	out, created, err := s.enroll(ctx, req)
	if errors.Is(err, errAgentAppeared) {
		// The agent this transaction found absent was created while it
		// waited, so it runs once more holding that agent's execution
		// row first, in the lock order every other writer takes.
		return s.enroll(ctx, req)
	}
	return out, created, err
}

// errAgentAppeared reports an approval spent, while this spending
// waited for it, by a transaction that created the agent this one had
// not yet locked.
var errAgentAppeared = errorx.New(errorx.KindAborted, "enroll: the agent was created while waiting")

func (s *Store) enroll(ctx context.Context, req agent.EnrollmentRequest) (*agent.EnrollmentResult, bool, error) {
	var (
		out     *agent.EnrollmentResult
		created bool
	)
	err := s.inTx(ctx, "enroll", func(q *db.Queries) error {
		id := pgUUID(uuid.UUID(req.Enrollment))
		unlocked, err := q.GetEnrollmentByID(ctx, id)
		if err != nil {
			return storageError(err, "read enrollment", agent.ErrBootstrapRefused, nil)
		}
		// The execution row comes before the approval wherever the agent
		// already exists: the lock order a revocation follows too.
		exec, err := q.LockExecution(ctx, unlocked.AgentGrn)
		held := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return storageError(err, "lock execution", nil, nil)
		}
		row, err := q.LockEnrollment(ctx, id)
		if err != nil {
			return storageError(err, "lock enrollment", agent.ErrBootstrapRefused, nil)
		}
		now, err := q.Now(ctx)
		if err != nil {
			return storageError(err, "read clock", nil, nil)
		}
		if row.WithdrawnAt.Valid || !now.Time.Before(row.ExpiresAt.Time) {
			return agent.ErrBootstrapRefused
		}
		if row.ConsumedAt.Valid {
			if !bytes.Equal(row.RequestSha256, req.RequestSHA256[:]) {
				return agent.ErrEnrollmentConflict
			}
			if !held {
				return errAgentAppeared
			}
			if err := stillCurrent(ctx, q, row); err != nil {
				return err
			}
			// garam keeps only the credential's hash, so a retry
			// re-issues it, under this approval's row lock: the earlier
			// credential and every token minted under it go.
			if err := reissue(ctx, q, row.SupervisorGrn, req.Credential, now); err != nil {
				return err
			}
			out, err = resultOf(row)
			return err
		}
		lineage := pgUUID(uuid.UUID(req.Lineage))
		epoch, err := place(ctx, q, row, exec, held, lineage, now)
		if err != nil {
			return err
		}
		registration := db.InsertSupervisorParams{
			Grn: row.SupervisorGrn, AgentGrn: row.AgentGrn, EnrollmentID: id, HostName: row.HostName,
			RegisteredAt: now,
		}
		spending := db.SpendEnrollmentParams{
			ID: id, ConsumedAt: now, RequestSha256: req.RequestSHA256[:],
			Epoch: pgtype.Int4{Int32: epoch, Valid: true}, Lineage: lineage,
		}
		if row.ExpectedAssigneeGrn.Valid {
			// The anchor the new supervisor's first activation names,
			// read under the execution lock the move holds: ending the
			// activation does not move it.
			spending.ReplacedActivationID = exec.LatestActivationID
		}
		if err := q.InsertSupervisor(ctx, registration); err != nil {
			return storageError(err, "register supervisor", nil, agent.ErrEnrollmentConflict)
		}
		if err := reissue(ctx, q, row.SupervisorGrn, req.Credential, now); err != nil {
			return err
		}
		if err := q.SpendEnrollment(ctx, spending); err != nil {
			return storageError(err, "spend enrollment", nil, nil)
		}
		row, err = q.GetEnrollmentByID(ctx, id)
		if err != nil {
			return storageError(err, "read enrollment", nil, nil)
		}
		out, err = resultOf(row)
		created = err == nil
		return err
	})
	if err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// reissue replaces the supervisor's credential with c: the earlier one
// and every token minted under it go by the foreign key.
func reissue(ctx context.Context, q *db.Queries, supervisor string, c *agent.SupervisorCredential, now pgtype.Timestamptz) error {
	if c == nil {
		return errorx.New(errorx.KindInternal, "enroll: no credential to issue")
	}
	if err := q.DeleteSupervisorCredential(ctx, supervisor); err != nil {
		return storageError(err, "void supervisor credential", nil, nil)
	}
	if err := q.InsertSupervisorCredential(ctx, db.InsertSupervisorCredentialParams{
		SupervisorGrn: supervisor, CredentialHash: c.Hash(), IssuedAt: now,
	}); err != nil {
		return storageError(err, "issue supervisor credential", nil, nil)
	}
	return nil
}

// place admits the agent an approval binds onto its supervisor and
// answers the epoch: a new agent, or a replacement from exactly the
// assignment it expected, which also ends the replaced registration.
// Either way the agent moves to lineage, in which its supervisor mints
// its tokens.
func place(ctx context.Context, q *db.Queries, row db.AgentEnrollment, exec db.AgentExecution, held bool, lineage pgtype.UUID, now pgtype.Timestamptz) (int32, error) {
	orgGRN, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return 0, errorx.Wrap(err, "stored enrollment org", errorx.KindInternal)
	}
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return 0, errorx.Wrap(err, "stored enrollment agent", errorx.KindInternal)
	}
	supervisorGRN, err := grn.Parse(row.SupervisorGrn)
	if err != nil {
		return 0, errorx.Wrap(err, "stored enrollment supervisor", errorx.KindInternal)
	}
	a := agent.IdentityAdmission{Agent: agentGRN, Org: orgGRN, Assignee: supervisorGRN, Lineage: agent.Lineage(lineage.Bytes)}
	if !row.ExpectedAssigneeGrn.Valid {
		if held {
			return 0, agent.ErrEnrollmentConflict
		}
		epoch, err := admit(ctx, q, a, nil, now)
		if errors.Is(err, agent.ErrAgentAlreadyExists) {
			return 0, agent.ErrEnrollmentConflict
		}
		return epoch, err
	}
	if !held {
		return 0, agent.ErrEnrollmentSuperseded
	}
	expected, err := grn.Parse(row.ExpectedAssigneeGrn.String)
	if err != nil {
		return 0, errorx.Wrap(err, "stored enrollment expected assignee", errorx.KindInternal)
	}
	a.Replaces = &agent.Replacement{Agent: agentGRN, Assignee: expected, Epoch: int(row.ExpectedEpoch.Int32)}
	epoch, err := admit(ctx, q, a, &exec, now)
	if err != nil {
		return 0, err
	}
	if err := q.EndCurrentSupervisor(ctx, db.EndCurrentSupervisorParams{AgentGrn: row.AgentGrn, EndedAt: now}); err != nil {
		return 0, storageError(err, "end supervisor", nil, nil)
	}
	return epoch, nil
}

// stillCurrent refuses replaying a spent approval once its registration
// is no longer what holds the agent: ended, or the agent reassigned
// since. A replay never discloses a superseded result as current. The
// caller holds the agent's execution row.
func stillCurrent(ctx context.Context, q *db.Queries, row db.AgentEnrollment) error {
	registered, err := q.GetSupervisor(ctx, row.SupervisorGrn)
	if err != nil {
		return storageError(err, "read supervisor", agent.ErrEnrollmentSuperseded, nil)
	}
	if registered.EndedAt.Valid {
		return agent.ErrEnrollmentSuperseded
	}
	assignment, err := q.ShareAssignment(ctx, row.AgentGrn)
	if err != nil {
		return storageError(err, "read assignment", agent.ErrEnrollmentSuperseded, nil)
	}
	if assignment.AssigneeGrn != row.SupervisorGrn || assignment.Epoch != row.Epoch.Int32 {
		return agent.ErrEnrollmentSuperseded
	}
	return nil
}

func (s *Store) GetSupervisor(ctx context.Context, g grn.GRN) (*agent.Supervisor, error) {
	row, err := s.q.GetSupervisor(ctx, g.String())
	if err != nil {
		return nil, storageError(err, "read supervisor", agent.ErrSupervisorNotFound, nil)
	}
	if row.EndedAt.Valid {
		return nil, agent.ErrSupervisorNotFound
	}
	return supervisorOf(row)
}

func (s *Store) Revoke(ctx context.Context, org, g grn.GRN) error {
	return s.inTx(ctx, "revoke supervisor", func(q *db.Queries) error {
		unlocked, err := q.GetSupervisor(ctx, g.String())
		if err != nil {
			return storageError(err, "read supervisor", agent.ErrSupervisorNotFound, nil)
		}
		agentGRN, err := grn.Parse(unlocked.AgentGrn)
		if err != nil {
			return errorx.Wrap(err, "stored supervisor agent", errorx.KindInternal)
		}
		if agentGRN.Organization != org.Identifier {
			return agent.ErrSupervisorNotFound
		}
		exec, err := lockExecution(ctx, q, agentGRN)
		if err != nil {
			return err
		}
		row, err := q.LockSupervisor(ctx, g.String())
		if err != nil {
			return storageError(err, "lock supervisor", agent.ErrSupervisorNotFound, nil)
		}
		if row.EndedAt.Valid {
			return agent.ErrSupervisorNotFound
		}
		now, err := q.Now(ctx)
		if err != nil {
			return storageError(err, "read clock", nil, nil)
		}
		if err := q.EndSupervisor(ctx, db.EndSupervisorParams{Grn: row.Grn, EndedAt: now}); err != nil {
			return storageError(err, "end supervisor", nil, nil)
		}
		// Only the registration holding the agent ends its activation;
		// one a replacement already ended holds nothing to end.
		assignment, err := q.ShareAssignment(ctx, row.AgentGrn)
		if err != nil {
			return storageError(err, "read assignment", nil, nil)
		}
		if assignment.AssigneeGrn == row.Grn && exec.ActivationID.Valid {
			if err := end(ctx, q, exec.ActivationID, agent.EndDeactivated, now.Time); err != nil {
				return err
			}
			if err := q.ClearActivation(ctx, db.ClearActivationParams{AgentGrn: row.AgentGrn, UpdatedAt: now}); err != nil {
				return storageError(err, "clear activation", nil, nil)
			}
		}
		return storageError(q.WithdrawPendingEnrollments(ctx, db.WithdrawPendingEnrollmentsParams{AgentGrn: row.AgentGrn, WithdrawnAt: now}),
			"withdraw pending enrollments", nil, nil)
	})
}

// orNoRows reports a row read but not this caller's as absent.
func orNoRows(err error) error {
	if err == nil {
		return pgx.ErrNoRows
	}
	return err
}

// enrollmentOf reads a stored approval back. A value that no longer
// parses is data corruption, hence internal.
func enrollmentOf(row db.AgentEnrollment) (*agent.Enrollment, error) {
	corrupt := func(err error) error { return errorx.Wrap(err, "stored enrollment", errorx.KindInternal) }
	org, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, corrupt(err)
	}
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return nil, corrupt(err)
	}
	supervisor, err := grn.Parse(row.SupervisorGrn)
	if err != nil {
		return nil, corrupt(err)
	}
	approver, err := grn.Parse(row.ApprovedBy)
	if err != nil {
		return nil, corrupt(err)
	}
	placement, err := agent.ParsePlacement(row.Placement)
	if err != nil {
		return nil, corrupt(err)
	}
	e := &agent.Enrollment{
		ID: agent.EnrollmentID(row.ID.Bytes), Org: org, Placement: placement, Agent: agentGRN, Supervisor: supervisor,
		HostName: agent.Name(row.HostName), RequestID: agent.RequestID(row.RequestID),
		SecretSHA256: [32]byte(row.SecretSha256), ApprovedBy: approver,
		ExpiresAt: row.ExpiresAt.Time, CreatedAt: row.CreatedAt.Time, Withdrawn: row.WithdrawnAt.Valid,
	}
	if row.ExpectedAssigneeGrn.Valid {
		assignee, err := grn.Parse(row.ExpectedAssigneeGrn.String)
		if err != nil {
			return nil, corrupt(err)
		}
		e.Replaces = &agent.Replacement{Agent: agentGRN, Assignee: assignee, Epoch: int(row.ExpectedEpoch.Int32)}
	}
	if row.ConsumedAt.Valid {
		result, err := resultOf(row)
		if err != nil {
			return nil, err
		}
		e.Spent = &agent.SpentEnrollment{RequestSHA256: [32]byte(row.RequestSha256), Result: *result}
	}
	return e, nil
}

// resultOf reads the public answer a spent approval recorded.
func resultOf(row db.AgentEnrollment) (*agent.EnrollmentResult, error) {
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "stored enrollment agent", errorx.KindInternal)
	}
	supervisor, err := grn.Parse(row.SupervisorGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "stored enrollment supervisor", errorx.KindInternal)
	}
	placement, err := agent.ParsePlacement(row.Placement)
	if err != nil {
		return nil, errorx.Wrap(err, "stored enrollment placement", errorx.KindInternal)
	}
	out := &agent.EnrollmentResult{
		Placement: placement, Agent: agentGRN, Supervisor: supervisor, Epoch: int(row.Epoch.Int32),
		Lineage: agent.Lineage(row.Lineage.Bytes),
	}
	if row.ReplacedActivationID.Valid {
		replaced := agent.ActivationID(row.ReplacedActivationID.Bytes)
		out.ReplacesActivation = &replaced
	}
	return out, nil
}

func supervisorOf(row db.AgentSupervisor) (*agent.Supervisor, error) {
	g, err := grn.Parse(row.Grn)
	if err != nil {
		return nil, errorx.Wrap(err, "stored supervisor", errorx.KindInternal)
	}
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "stored supervisor agent", errorx.KindInternal)
	}
	return &agent.Supervisor{
		GRN: g, Agent: agentGRN, Enrollment: agent.EnrollmentID(row.EnrollmentID.Bytes),
		HostName: agent.Name(row.HostName),
	}, nil
}
