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
		AgentGrn:      e.Agent.String(),
		SupervisorGrn: e.Supervisor.String(),
		HostName:      string(e.HostName),
		RequestID:     string(e.RequestID),
		ApprovedBy:    e.ApprovedBy.String(),
		SecretSha256:  e.SecretSHA256[:],
		ExpiresAt:     pgxtype.Timestamptz(e.ExpiresAt),
		CreatedAt:     pgxtype.Timestamptz(e.CreatedAt),
	}
	if k := e.Keys; k != nil {
		params.SupervisorKeySha256, params.AgentKeySha256 = k.Supervisor[:], k.Agent[:]
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
			// garam keeps only a v2 credential's hash, so its retry
			// re-issues it, under this approval's row lock: the earlier
			// credential and every token minted under it go.
			if keyless(row) {
				if err := reissue(ctx, q, row.SupervisorGrn, req.Credential, now); err != nil {
					return err
				}
			}
			out, err = resultOf(row)
			return err
		}
		signs := !keyless(row)
		if signs && (req.Supervisor == nil || req.Agent == nil) || !signs && req.Credential == nil {
			return errorx.New(errorx.KindInternal, "enroll: nothing recorded and nothing issued")
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
		if signs {
			if err := q.InsertCredential(ctx, db.InsertCredentialParams{
				AgentGrn: row.AgentGrn, Serial: req.Agent.Credential.Serial, Lineage: lineage,
				NotAfter: pgxtype.Timestamptz(req.Agent.Credential.NotAfter),
			}); err != nil {
				return storageError(err, "record credential", nil, nil)
			}
			text := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: true} }
			registration.Serial = text(req.Supervisor.Credential.Serial)
			registration.NotAfter = pgxtype.Timestamptz(req.Supervisor.Credential.NotAfter)
			spending.SupervisorCertificatePem = text(req.Supervisor.CertificatePEM)
			spending.AgentCertificatePem = text(req.Agent.CertificatePEM)
			spending.IssuerPem = text(req.Agent.IssuerPEM)
			spending.ServerRootPem = text(req.Agent.ServerRootPEM)
		}
		if err := q.InsertSupervisor(ctx, registration); err != nil {
			return storageError(err, "register supervisor", nil, agent.ErrEnrollmentConflict)
		}
		if !signs {
			if err := reissue(ctx, q, row.SupervisorGrn, req.Credential, now); err != nil {
				return err
			}
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

// keyless reports whether an approval binds no key, which is spent
// under standalone-enrollment.v2 and issues a credential.
func keyless(row db.AgentEnrollment) bool { return row.SupervisorKeySha256 == nil }

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

// place writes where an approval puts its agent and answers the epoch:
// a new agent with its fenced execution and first assignment, or a
// replacement's reassignment from exactly the assignment it expected,
// which ends the replaced registration and the activation its runtime
// held. Either way the agent moves to lineage, which holds no
// certificate until the caller records the enrolled one, so every
// certificate it held before is fenced.
func place(ctx context.Context, q *db.Queries, row db.AgentEnrollment, exec db.AgentExecution, held bool, lineage pgtype.UUID, now pgtype.Timestamptz) (int32, error) {
	if !row.ExpectedAssigneeGrn.Valid {
		if held {
			return 0, agent.ErrEnrollmentConflict
		}
		orgGRN, err := grn.Parse(row.OrgGrn)
		if err != nil {
			return 0, errorx.Wrap(err, "stored enrollment org", errorx.KindInternal)
		}
		agentGRN, err := grn.Parse(row.AgentGrn)
		if err != nil {
			return 0, errorx.Wrap(err, "stored enrollment agent", errorx.KindInternal)
		}
		a, err := agent.New(agentGRN, orgGRN)
		if err != nil {
			return 0, err
		}
		if err := q.CreateManagedAgent(ctx, db.CreateManagedAgentParams{
			Grn: row.AgentGrn, OrgGrn: row.OrgGrn, ProvisioningState: string(a.ProvisioningState()),
			RuntimeState: string(a.RuntimeState()), CreatedAt: now,
		}); err != nil {
			return 0, storageError(err, "create agent", nil, agent.ErrEnrollmentConflict)
		}
		// Born fenced, as a managed agent is: no certificate of it
		// admits anything until the lineage records its first.
		if err := q.CreateFencedExecution(ctx, db.CreateFencedExecutionParams{
			AgentGrn: row.AgentGrn, Lineage: lineage, UpdatedAt: now,
		}); err != nil {
			return 0, storageError(err, "create execution", nil, nil)
		}
		epoch, err := q.AssignFirst(ctx, db.AssignFirstParams{AgentGrn: row.AgentGrn, AssigneeGrn: row.SupervisorGrn, CreatedAt: now})
		return epoch, storageError(err, "assign", nil, nil)
	}
	if !held {
		return 0, agent.ErrEnrollmentSuperseded
	}
	epoch, err := q.ReassignSupervisor(ctx, db.ReassignSupervisorParams{
		AgentGrn: row.AgentGrn, AssigneeGrn: row.SupervisorGrn, UpdatedAt: now,
		ExpectedAssigneeGrn: row.ExpectedAssigneeGrn.String, ExpectedEpoch: row.ExpectedEpoch.Int32,
	})
	if err != nil {
		return 0, storageError(err, "reassign", agent.ErrEnrollmentSuperseded, nil)
	}
	if err := q.EndCurrentSupervisor(ctx, db.EndCurrentSupervisorParams{AgentGrn: row.AgentGrn, EndedAt: now}); err != nil {
		return 0, storageError(err, "end supervisor", nil, nil)
	}
	if exec.ActivationID.Valid {
		if err := end(ctx, q, exec.ActivationID, agent.EndReassigned, now.Time); err != nil {
			return 0, err
		}
	}
	if err := q.MoveLineage(ctx, db.MoveLineageParams{AgentGrn: row.AgentGrn, Lineage: lineage, UpdatedAt: now}); err != nil {
		return 0, storageError(err, "move lineage", nil, nil)
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

func (s *Store) GetRenewal(ctx context.Context, supervisor grn.GRN, predecessor string) (*agent.RecordedRenewal, error) {
	row, err := s.q.GetSupervisorRenewal(ctx, db.GetSupervisorRenewalParams{SupervisorGrn: supervisor.String(), PredecessorSerial: predecessor})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, storageError(err, "read renewal", nil, nil)
	}
	return &agent.RecordedRenewal{
		RequestID: agent.RequestID(row.RequestID), RequestSHA256: [32]byte(row.RequestSha256),
		Successor: row.Serial, Current: row.Current,
	}, nil
}

func (s *Store) Renew(ctx context.Context, req agent.SupervisorRenewal) (*agent.SupervisorCertificate, bool, error) {
	var (
		out     *agent.SupervisorCertificate
		created bool
	)
	err := s.inTx(ctx, "renew supervisor", func(q *db.Queries) error {
		registered, err := q.LockSupervisor(ctx, req.Supervisor.String())
		if err != nil {
			return storageError(err, "lock supervisor", agent.ErrSupervisorNotFound, nil)
		}
		if registered.EndedAt.Valid {
			return agent.ErrSupervisorNotFound
		}
		now, err := q.Now(ctx)
		if err != nil {
			return storageError(err, "read clock", nil, nil)
		}
		if !now.Time.Before(req.Presented.NotAfter) {
			return agent.ErrCredentialFenced
		}
		if !registered.Serial.Valid {
			// A supervisor enrolled under v2 holds no certificate to renew.
			return agent.ErrRenewalConflict
		}
		if req.Presented.Serial != registered.Serial.String {
			// Only the current certificate's immediate predecessor, with
			// the request that produced it, is answered again.
			prior, err := q.GetSupervisorRenewal(ctx, db.GetSupervisorRenewalParams{
				SupervisorGrn: registered.Grn, PredecessorSerial: req.Presented.Serial,
			})
			if err != nil {
				return storageError(err, "read renewal", agent.ErrRenewalConflict, nil)
			}
			if !prior.Current || prior.RequestID != string(req.RequestID) || !bytes.Equal(prior.RequestSha256, req.RequestSHA256[:]) {
				return agent.ErrRenewalConflict
			}
			out = &agent.SupervisorCertificate{
				CertificatePEM: prior.CertificatePem, IssuerPEM: prior.IssuerPem,
				ServerRootPEM: prior.ServerRootPem, NotAfter: prior.NotAfter.Time,
			}
			return nil
		}
		if req.Signed == nil {
			return errorx.New(errorx.KindInternal, "renew supervisor: nothing recorded and nothing signed")
		}
		signed := req.Signed
		if err := q.InsertSupervisorRenewal(ctx, db.InsertSupervisorRenewalParams{
			SupervisorGrn: registered.Grn, PredecessorSerial: req.Presented.Serial,
			RequestID: string(req.RequestID), RequestSha256: req.RequestSHA256[:],
			Serial: signed.Credential.Serial, CertificatePem: signed.CertificatePEM, IssuerPem: signed.IssuerPEM,
			ServerRootPem: signed.ServerRootPEM, NotAfter: pgxtype.Timestamptz(signed.Credential.NotAfter), CreatedAt: now,
		}); err != nil {
			return storageError(err, "record renewal", nil, agent.ErrRenewalConflict)
		}
		if err := q.MoveSupervisorCertificate(ctx, db.MoveSupervisorCertificateParams{
			Grn: registered.Grn, Serial: pgtype.Text{String: signed.Credential.Serial, Valid: true},
			NotAfter: pgxtype.Timestamptz(signed.Credential.NotAfter),
		}); err != nil {
			return storageError(err, "move supervisor certificate", nil, nil)
		}
		out = &agent.SupervisorCertificate{
			CertificatePEM: signed.CertificatePEM, IssuerPEM: signed.IssuerPEM,
			ServerRootPEM: signed.ServerRootPEM, NotAfter: signed.Credential.NotAfter,
		}
		created = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, created, nil
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
	e := &agent.Enrollment{
		ID: agent.EnrollmentID(row.ID.Bytes), Org: org, Agent: agentGRN, Supervisor: supervisor,
		HostName: agent.Name(row.HostName), RequestID: agent.RequestID(row.RequestID),
		SecretSHA256: [32]byte(row.SecretSha256), ApprovedBy: approver,
		ExpiresAt: row.ExpiresAt.Time, CreatedAt: row.CreatedAt.Time, Withdrawn: row.WithdrawnAt.Valid,
	}
	if !keyless(row) {
		e.Keys = &agent.HostKeys{
			Supervisor: agent.KeyFingerprint(row.SupervisorKeySha256), Agent: agent.KeyFingerprint(row.AgentKeySha256),
		}
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
	out := &agent.EnrollmentResult{
		Agent: agentGRN, Supervisor: supervisor, Epoch: int(row.Epoch.Int32), Lineage: agent.Lineage(row.Lineage.Bytes),
	}
	if row.SupervisorCertificatePem.Valid {
		out.Certificates = &agent.EnrollmentCertificates{
			SupervisorCertificatePEM: row.SupervisorCertificatePem.String, AgentCertificatePEM: row.AgentCertificatePem.String,
			IssuerPEM: row.IssuerPem.String,
		}
		out.ServerRootPEM = row.ServerRootPem.String
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
	out := &agent.Supervisor{
		GRN: g, Agent: agentGRN, Enrollment: agent.EnrollmentID(row.EnrollmentID.Bytes),
		HostName: agent.Name(row.HostName),
	}
	if row.Serial.Valid {
		out.Certificate = &agent.Credential{Serial: row.Serial.String, NotAfter: row.NotAfter.Time}
	}
	return out, nil
}
