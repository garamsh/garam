package postgres

import (
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

// Compile-time check that Store satisfies agent.AdmissionStore.
var _ agent.AdmissionStore = (*Store)(nil)

// admit writes a and answers the epoch the agent is assigned at: the
// one write every enrollment contract admits an identity through
// (ADR-0102 §Common agent enrollment). A new agent is written with its
// execution fenced in a.Lineage and its first assignment. A
// replacement moves the agent from exactly the assignment it expects,
// ends its activation and moves it to a.Lineage; exec is its execution
// row, which the caller holds, and nil where none was found. Either
// way no generation is active and no certificate of the agent admits
// anything until its lineage records one.
func admit(ctx context.Context, q *db.Queries, a agent.IdentityAdmission, exec *db.AgentExecution, now pgtype.Timestamptz) (int32, error) {
	lineage := pgUUID(uuid.UUID(a.Lineage))
	if a.Replaces == nil {
		identity, err := agent.New(a.Agent, a.Org)
		if err != nil {
			return 0, err
		}
		if err := q.CreateManagedAgent(ctx, db.CreateManagedAgentParams{
			Grn: a.Agent.String(), OrgGrn: a.Org.String(), ProvisioningState: string(identity.ProvisioningState()),
			RuntimeState: string(identity.RuntimeState()), CreatedAt: now,
		}); err != nil {
			return 0, storageError(err, "create agent", nil, agent.ErrAgentAlreadyExists)
		}
		if err := q.CreateFencedExecution(ctx, db.CreateFencedExecutionParams{
			AgentGrn: a.Agent.String(), Lineage: lineage, UpdatedAt: now,
		}); err != nil {
			return 0, storageError(err, "create execution", nil, nil)
		}
		epoch, err := q.AssignFirst(ctx, db.AssignFirstParams{AgentGrn: a.Agent.String(), AssigneeGrn: a.Assignee.String(), CreatedAt: now})
		return epoch, storageError(err, "assign", nil, nil)
	}
	if exec == nil {
		return 0, agent.ErrEnrollmentSuperseded
	}
	epoch, err := q.ReassignSupervisor(ctx, db.ReassignSupervisorParams{
		AgentGrn: a.Agent.String(), AssigneeGrn: a.Assignee.String(), UpdatedAt: now,
		ExpectedAssigneeGrn: a.Replaces.Assignee.String(), ExpectedEpoch: int32(a.Replaces.Epoch),
	})
	if err != nil {
		return 0, storageError(err, "reassign", agent.ErrEnrollmentSuperseded, nil)
	}
	if exec.ActivationID.Valid {
		if err := end(ctx, q, exec.ActivationID, agent.EndReassigned, now.Time); err != nil {
			return 0, err
		}
	}
	if err := q.MoveLineage(ctx, db.MoveLineageParams{AgentGrn: a.Agent.String(), Lineage: lineage, UpdatedAt: now}); err != nil {
		return 0, storageError(err, "move lineage", nil, nil)
	}
	return epoch, nil
}

// createManaged admits c's agent onto its controller and records c
// with the epoch admission assigned, in the caller's transaction.
func createManaged(ctx context.Context, q *db.Queries, c *agent.ManagedCreation, now pgtype.Timestamptz) error {
	epoch, err := admit(ctx, q, agent.IdentityAdmission{
		Agent: c.Agent, Org: grn.Org(c.Agent.Organization), Assignee: c.Controller, Lineage: c.Lineage,
	}, nil, now)
	if err != nil {
		return err
	}
	c.Epoch = int(epoch)
	return storageError(q.InsertManagedCreation(ctx, db.InsertManagedCreationParams{
		AgentGrn:      c.Agent.String(),
		AudienceGrn:   c.Audience.String(),
		ControllerGrn: c.Controller.String(),
		RequestID:     string(c.RequestID),
		OperationRef:  c.OperationRef,
		Epoch:         epoch,
		Lineage:       pgUUID(uuid.UUID(c.Lineage)),
		CreatedAt:     now,
	}), "record creation", nil, agent.ErrCreationConflict)
}

func (s *Store) PrepareApproval(ctx context.Context, a agent.EnrollmentApproval) (*agent.EnrollmentApproval, bool, error) {
	inserted, err := s.q.InsertEnrollmentApproval(ctx, db.InsertEnrollmentApprovalParams{
		ID:           pgUUID(uuid.UUID(a.ID)),
		OrgGrn:       a.Org.String(),
		RequestID:    string(a.Binding.RequestID),
		Placement:    string(a.Binding.Placement),
		AudienceGrn:  a.Audience.String(),
		AssigneeGrn:  a.Binding.Assignee.String(),
		OperationRef: a.Binding.OperationRef,
		CreatedAt:    pgxtype.Timestamptz(a.CreatedAt),
	})
	if err != nil {
		return nil, false, storageError(err, "insert approval", nil, agent.ErrApprovalBindingConflict)
	}
	if inserted == 1 {
		return &a, true, nil
	}
	row, err := s.q.GetEnrollmentApprovalByRequest(ctx, db.GetEnrollmentApprovalByRequestParams{
		OrgGrn: a.Org.String(), RequestID: string(a.Binding.RequestID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The reference holds an approval under another request
		// identifier.
		return nil, false, agent.ErrApprovalBindingConflict
	}
	if err != nil {
		return nil, false, storageError(err, "read approval", nil, nil)
	}
	stored, err := approvalOf(ctx, s.q, row)
	return stored, false, err
}

func (s *Store) GetApproval(ctx context.Context, org grn.GRN, id agent.ApprovalID) (*agent.EnrollmentApproval, error) {
	row, err := s.q.GetEnrollmentApproval(ctx, db.GetEnrollmentApprovalParams{OrgGrn: org.String(), ID: pgUUID(uuid.UUID(id))})
	if err != nil {
		return nil, storageError(err, "read approval", agent.ErrApprovalNotFound, nil)
	}
	return approvalOf(ctx, s.q, row)
}

func (s *Store) Admit(ctx context.Context, id agent.ApprovalID, c agent.ManagedCreation) (*agent.ManagedCreation, bool, error) {
	var (
		out     *agent.ManagedCreation
		created bool
	)
	err := s.inTx(ctx, "admit agent", func(q *db.Queries) error {
		row, err := q.LockEnrollmentApproval(ctx, pgUUID(uuid.UUID(id)))
		if err != nil {
			return storageError(err, "lock approval", agent.ErrApprovalNotFound, nil)
		}
		if row.AgentGrn.Valid {
			approval, err := approvalOf(ctx, q, row)
			if err != nil {
				return err
			}
			out = approval.Admitted
			return nil
		}
		now, err := q.Now(ctx)
		if err != nil {
			return storageError(err, "read clock", nil, nil)
		}
		if err := createManaged(ctx, q, &c, now); err != nil {
			if errors.Is(err, agent.ErrCreationConflict) {
				return agent.ErrApprovalSpent
			}
			return err
		}
		if err := q.SpendEnrollmentApproval(ctx, db.SpendEnrollmentApprovalParams{
			ID: row.ID, AgentGrn: pgtype.Text{String: c.Agent.String(), Valid: true}, AdmittedAt: now,
		}); err != nil {
			return storageError(err, "spend approval", nil, nil)
		}
		out, created = &c, true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// approvalOf reads a stored approval back, with the creation it
// admitted once it is spent. A value that no longer parses is data
// corruption, hence internal.
func approvalOf(ctx context.Context, q *db.Queries, row db.AgentEnrollmentApproval) (*agent.EnrollmentApproval, error) {
	org, err := grn.Parse(row.OrgGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "stored approval org", errorx.KindInternal)
	}
	audience, err := grn.Parse(row.AudienceGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "stored approval audience", errorx.KindInternal)
	}
	assignee, err := grn.Parse(row.AssigneeGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "stored approval assignee", errorx.KindInternal)
	}
	placement, err := agent.ParsePlacement(row.Placement)
	if err != nil {
		return nil, errorx.Wrap(err, "stored approval placement", errorx.KindInternal)
	}
	a := &agent.EnrollmentApproval{
		ID: agent.ApprovalID(row.ID.Bytes), Org: org, Audience: audience,
		Binding: agent.EnrollmentBinding{
			RequestID: agent.RequestID(row.RequestID), Placement: placement, Assignee: assignee,
			OperationRef: row.OperationRef,
		},
		CreatedAt: row.CreatedAt.Time,
	}
	if row.AgentGrn.Valid {
		creation, err := q.GetManagedCreation(ctx, row.AgentGrn.String)
		if err != nil {
			return nil, storageError(err, "read admitted creation", nil, nil)
		}
		if a.Admitted, err = creationOf(creation); err != nil {
			return nil, err
		}
	}
	return a, nil
}
