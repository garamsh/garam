package postgres

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/agent/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/pgxtype"
)

func (s *Store) CreateManaged(ctx context.Context, a *agent.Agent, c agent.ManagedCreation) (*agent.ManagedCreation, bool, error) {
	err := s.inTx(ctx, "create managed agent", func(q *db.Queries) error {
		at := pgxtype.Timestamptz(a.CreatedAt())
		if err := q.CreateManagedAgent(ctx, db.CreateManagedAgentParams{
			Grn:               a.GRN().String(),
			OrgGrn:            a.Organization().String(),
			ProvisioningState: string(a.ProvisioningState()),
			RuntimeState:      string(a.RuntimeState()),
			CreatedAt:         at,
		}); err != nil {
			return storageError(err, "create agent", nil, agent.ErrAgentAlreadyExists)
		}
		lineage := pgUUID(uuid.UUID(c.Lineage))
		if err := q.CreateFencedExecution(ctx, db.CreateFencedExecutionParams{
			AgentGrn: a.GRN().String(), Lineage: lineage, UpdatedAt: at,
		}); err != nil {
			return storageError(err, "create execution", nil, nil)
		}
		epoch, err := q.AssignFirst(ctx, db.AssignFirstParams{
			AgentGrn: a.GRN().String(), AssigneeGrn: c.Controller.String(), CreatedAt: at,
		})
		if err != nil {
			return storageError(err, "assign", nil, nil)
		}
		c.Epoch = int(epoch)
		return storageError(q.InsertManagedCreation(ctx, db.InsertManagedCreationParams{
			AgentGrn:      a.GRN().String(),
			AudienceGrn:   c.Audience.String(),
			ControllerGrn: c.Controller.String(),
			RequestID:     string(c.RequestID),
			OperationRef:  c.OperationRef,
			Epoch:         epoch,
			Lineage:       lineage,
			CreatedAt:     at,
		}), "record creation", nil, agent.ErrCreationConflict)
	})
	if errorx.IsKind(err, errorx.KindAlreadyExists) {
		// A unique key refused the write, so this one wrote nothing:
		// a concurrent creation committed first. Under the same
		// reference, audience, request and controller it is the same
		// creation; anything else conflicts.
		prior, err := s.GetManagedCreationByReference(ctx, c.OperationRef)
		if err != nil {
			return nil, false, err
		}
		if prior == nil || prior.Audience != c.Audience || prior.RequestID != c.RequestID || prior.Controller != c.Controller {
			return nil, false, agent.ErrCreationConflict
		}
		return prior, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &c, true, nil
}

func (s *Store) GetManagedCreation(ctx context.Context, agentGRN grn.GRN) (*agent.ManagedCreation, error) {
	row, err := s.q.GetManagedCreation(ctx, agentGRN.String())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, storageError(err, "read managed creation", nil, nil)
	}
	return creationOf(row)
}

func (s *Store) GetManagedCreationByReference(ctx context.Context, ref string) (*agent.ManagedCreation, error) {
	row, err := s.q.GetManagedCreationByReference(ctx, ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, storageError(err, "read managed creation", nil, nil)
	}
	return creationOf(row)
}

func (s *Store) InstallInitialCertificate(ctx context.Context, req agent.InitialCertificateRequest) (*agent.InitialCertificate, bool, error) {
	var (
		out     *agent.InitialCertificate
		created bool
	)
	err := s.inTx(ctx, "install initial certificate", func(q *db.Queries) error {
		exec, err := lockExecution(ctx, q, req.Agent)
		if err != nil {
			return err
		}
		epoch, now, err := recheck(ctx, q, req.Agent, req.Authority)
		if err != nil {
			return err
		}
		if epoch != req.Epoch {
			return agent.ErrStaleEpoch
		}
		// A recovery moves the agent to a lineage of its own, and a
		// retry of the initial issuance never moves it back.
		lineage := pgUUID(uuid.UUID(req.Lineage))
		if exec.Lineage != lineage {
			return agent.ErrLineageSuperseded
		}
		prior, err := q.GetInitialCertificate(ctx, req.Agent.String())
		switch {
		case err == nil:
			if prior.RequestID != string(req.RequestID) || prior.OperationRef != req.OperationRef ||
				int(prior.Epoch) != req.Epoch || !bytes.Equal(prior.RequestSha256, req.RequestSHA256[:]) {
				return agent.ErrInitialCertificateConflict
			}
			out = initialCertificateOf(prior)
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return storageError(err, "read initial certificate", nil, nil)
		}
		if req.Signed == nil {
			return errorx.New(errorx.KindInternal, "initial certificate: nothing recorded and nothing signed")
		}
		signed := req.Signed
		if err := q.InsertCredential(ctx, db.InsertCredentialParams{
			AgentGrn: req.Agent.String(), Serial: signed.Credential.Serial, Lineage: lineage,
			NotAfter: pgxtype.Timestamptz(signed.Credential.NotAfter),
		}); err != nil {
			return storageError(err, "record credential", nil, nil)
		}
		row := db.AgentInitialCertificate{
			AgentGrn:       req.Agent.String(),
			RequestID:      string(req.RequestID),
			OperationRef:   req.OperationRef,
			Epoch:          int32(req.Epoch),
			RequestSha256:  req.RequestSHA256[:],
			Lineage:        lineage,
			CertificatePem: signed.CertificatePEM,
			IssuerPem:      signed.IssuerPEM,
			ServerRootPem:  signed.ServerRootPEM,
			NotAfter:       pgxtype.Timestamptz(signed.Credential.NotAfter),
			CreatedAt:      pgxtype.Timestamptz(now),
		}
		if err := q.InsertInitialCertificate(ctx, db.InsertInitialCertificateParams(row)); err != nil {
			return storageError(err, "record initial certificate", nil, agent.ErrInitialCertificateConflict)
		}
		out, created = initialCertificateOf(row), true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, created, nil
}

func (s *Store) GetInitialCertificate(ctx context.Context, agentGRN grn.GRN) (*agent.InitialCertificate, error) {
	row, err := s.q.GetInitialCertificate(ctx, agentGRN.String())
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, storageError(err, "read initial certificate", nil, nil)
	}
	return initialCertificateOf(row), nil
}

// creationOf reads a stored creation back. A value that no longer
// parses is data corruption, hence internal.
func creationOf(row db.AgentManagedCreation) (*agent.ManagedCreation, error) {
	agentGRN, err := grn.Parse(row.AgentGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "stored managed agent", errorx.KindInternal)
	}
	audience, err := grn.Parse(row.AudienceGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "stored managed creation audience", errorx.KindInternal)
	}
	controller, err := grn.Parse(row.ControllerGrn)
	if err != nil {
		return nil, errorx.Wrap(err, "stored managed creation controller", errorx.KindInternal)
	}
	return &agent.ManagedCreation{
		Agent:        agentGRN,
		Audience:     audience,
		Controller:   controller,
		RequestID:    agent.RequestID(row.RequestID),
		OperationRef: row.OperationRef,
		Epoch:        int(row.Epoch),
		Lineage:      agent.Lineage(row.Lineage.Bytes),
	}, nil
}

func initialCertificateOf(row db.AgentInitialCertificate) *agent.InitialCertificate {
	return &agent.InitialCertificate{
		CertificatePEM: row.CertificatePem,
		IssuerPEM:      row.IssuerPem,
		ServerRootPEM:  row.ServerRootPem,
		NotAfter:       row.NotAfter.Time,
	}
}
