package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/agent/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

func (s *Store) CreateManaged(ctx context.Context, c agent.ManagedCreation) (*agent.ManagedCreation, bool, error) {
	err := s.inTx(ctx, "create managed agent", func(q *db.Queries) error {
		now, err := q.Now(ctx)
		if err != nil {
			return storageError(err, "read clock", nil, nil)
		}
		return createManaged(ctx, q, &c, now)
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
		Name:         storedName(row.Name),
	}, nil
}
