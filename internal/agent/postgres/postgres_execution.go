package postgres

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/agent/postgres/db"
	"github.com/garamsh/garam/internal/grn"
)

func (s *Store) Activate(ctx context.Context, req agent.ActivationRequest) (*agent.Activation, bool, error) {
	var (
		out     *agent.Activation
		created bool
	)
	err := s.inTx(ctx, "activate", func(q *db.Queries) error {
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
		current, err := q.CredentialCurrent(ctx, credentialParams(req.Agent, req.Credential))
		if err != nil {
			return storageError(err, "credential current", nil, nil)
		}
		if !current {
			return agent.ErrCredentialFenced
		}
		prior, err := q.GetActivationByRequest(ctx, db.GetActivationByRequestParams{
			AgentGrn: req.Agent.String(), RequestID: string(req.RequestID),
		})
		switch {
		case err == nil:
			out, err = remint(ctx, q, req, prior, now)
			return err
		case !errors.Is(err, pgx.ErrNoRows):
			return storageError(err, "read activation", nil, nil)
		}
		if exec.LatestActivationID != optionalUUID((*uuid.UUID)(req.Replaces)) {
			return agent.ErrActivationStale
		}
		row, err := q.InsertActivation(ctx, db.InsertActivationParams{
			ID:                   pgUUID(uuid.New()),
			AgentGrn:             req.Agent.String(),
			RequestID:            string(req.RequestID),
			Generation:           string(req.Generation),
			Epoch:                int32(epoch),
			Lineage:              exec.Lineage,
			AudienceGrn:          req.Audience.String(),
			GrantID:              req.GrantID,
			OperationRef:         optionalText(req.OperationRef),
			ReplacesActivationID: optionalUUID((*uuid.UUID)(req.Replaces)),
			CreatedAt:            stamp(now),
		})
		if err != nil {
			return storageError(err, "insert activation", nil, agent.ErrGenerationUsed)
		}
		if exec.ActivationID.Valid {
			if err := end(ctx, q, exec.ActivationID, agent.EndSuperseded, now); err != nil {
				return err
			}
		}
		if err := q.InstallActivation(ctx, db.InstallActivationParams{
			AgentGrn:     req.Agent.String(),
			ActivationID: row.ID,
			TokenHash:    req.TokenHash[:],
			UpdatedAt:    stamp(now),
		}); err != nil {
			return storageError(err, "install activation", nil, nil)
		}
		out, created = activationOf(req.Agent, row), true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return out, created, nil
}

// remint answers a retried activation. The same request while its
// activation is current replaces the token and raises its version;
// anything else under that request identifier is refused, so an ended
// activation is never revived.
func remint(ctx context.Context, q *db.Queries, req agent.ActivationRequest, prior db.AgentActivation, now time.Time) (*agent.Activation, error) {
	if !sameRequest(prior, req) {
		return nil, agent.ErrActivationConflict
	}
	if prior.EndedReason.Valid {
		return nil, agent.ErrActivationEnded
	}
	row, err := q.RemintActivation(ctx, prior.ID)
	if err != nil {
		return nil, storageError(err, "remint activation", nil, nil)
	}
	if err := q.ReplaceToken(ctx, db.ReplaceTokenParams{
		AgentGrn: req.Agent.String(), TokenHash: req.TokenHash[:], UpdatedAt: stamp(now),
	}); err != nil {
		return nil, storageError(err, "replace token", nil, nil)
	}
	return activationOf(req.Agent, row), nil
}

// sameRequest compares everything a request binds. The certificate is
// not part of it: a retry may present a renewed one, which the fence
// checks again regardless.
func sameRequest(prior db.AgentActivation, req agent.ActivationRequest) bool {
	return prior.Generation == string(req.Generation) &&
		int(prior.Epoch) == req.Epoch &&
		prior.ReplacesActivationID == optionalUUID((*uuid.UUID)(req.Replaces)) &&
		prior.AudienceGrn == req.Audience.String() &&
		prior.GrantID == req.GrantID &&
		prior.OperationRef == optionalText(req.OperationRef)
}

func end(ctx context.Context, q *db.Queries, id pgtype.UUID, reason agent.EndReason, now time.Time) error {
	err := q.EndActivation(ctx, db.EndActivationParams{
		ID:          id,
		EndedReason: pgtype.Text{String: string(reason), Valid: true},
		EndedAt:     stamp(now),
	})
	return storageError(err, "end activation", nil, nil)
}

func (s *Store) Deactivate(ctx context.Context, agentGRN grn.GRN, id agent.ActivationID, authority agent.Lifecycle) (bool, error) {
	var ended bool
	err := s.inTx(ctx, "deactivate", func(q *db.Queries) error {
		exec, err := lockExecution(ctx, q, agentGRN)
		if err != nil {
			return err
		}
		_, now, err := recheck(ctx, q, agentGRN, authority)
		if err != nil {
			return err
		}
		target := pgUUID(uuid.UUID(id))
		if _, err := q.GetActivation(ctx, db.GetActivationParams{AgentGrn: agentGRN.String(), ID: target}); err != nil {
			return storageError(err, "read activation", agent.ErrActivationNotFound, nil)
		}
		if exec.ActivationID != target {
			return nil
		}
		if err := end(ctx, q, target, agent.EndDeactivated, now); err != nil {
			return err
		}
		if err := q.ClearActivation(ctx, db.ClearActivationParams{AgentGrn: agentGRN.String(), UpdatedAt: stamp(now)}); err != nil {
			return storageError(err, "clear activation", nil, nil)
		}
		ended = true
		return nil
	})
	return ended, err
}

func (s *Store) Recover(ctx context.Context, req agent.RecoveryRequest) (*agent.Recovery, error) {
	var out *agent.Recovery
	err := s.inTx(ctx, "recover", func(q *db.Queries) error {
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
		if !now.Before(req.TokenExpiresAt) {
			return agent.ErrAuthorityExpired
		}
		prior, err := q.GetRecovery(ctx, db.GetRecoveryParams{AgentGrn: req.Agent.String(), RequestID: string(req.RequestID)})
		switch {
		case err == nil:
			if prior.OperationRef != req.OperationRef || !bytes.Equal(prior.BodySha256, req.BodySHA256[:]) {
				return agent.ErrRecoveryConflict
			}
			out = &agent.Recovery{Lineage: agent.Lineage(prior.Lineage.Bytes), CertificatePEM: prior.CertificatePem}
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return storageError(err, "read recovery", nil, nil)
		}
		lineage := pgUUID(uuid.UUID(req.Lineage))
		if err := q.InsertRecovery(ctx, db.InsertRecoveryParams{
			AgentGrn:       req.Agent.String(),
			RequestID:      string(req.RequestID),
			OperationRef:   req.OperationRef,
			BodySha256:     req.BodySHA256[:],
			Lineage:        lineage,
			CertificatePem: req.CertificatePEM,
			CreatedAt:      stamp(now),
		}); err != nil {
			return storageError(err, "insert recovery", nil, agent.ErrRecoveryConflict)
		}
		if exec.ActivationID.Valid {
			if err := end(ctx, q, exec.ActivationID, agent.EndRecovered, now); err != nil {
				return err
			}
		}
		if err := q.MoveLineage(ctx, db.MoveLineageParams{AgentGrn: req.Agent.String(), Lineage: lineage, UpdatedAt: stamp(now)}); err != nil {
			return storageError(err, "move lineage", nil, nil)
		}
		if err := q.InsertCredential(ctx, db.InsertCredentialParams{
			AgentGrn: req.Agent.String(), Serial: req.Credential.Serial, Lineage: lineage, NotAfter: stamp(req.Credential.NotAfter),
		}); err != nil {
			return storageError(err, "record credential", nil, nil)
		}
		out = &agent.Recovery{Lineage: req.Lineage, CertificatePEM: req.CertificatePEM}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GetRecovery(ctx context.Context, agentGRN grn.GRN, requestID agent.RequestID) (*agent.Recovery, error) {
	row, err := s.q.GetRecovery(ctx, db.GetRecoveryParams{AgentGrn: agentGRN.String(), RequestID: string(requestID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, storageError(err, "read recovery", nil, nil)
	}
	return &agent.Recovery{Lineage: agent.Lineage(row.Lineage.Bytes), CertificatePEM: row.CertificatePem}, nil
}

func (s *Store) RecordRenewal(ctx context.Context, agentGRN grn.GRN, token *agent.TokenHash, presented, successor agent.Credential) error {
	return s.inTx(ctx, "record renewal", func(q *db.Queries) error {
		var hash []byte
		if token != nil {
			hash = token[:]
		}
		answer, err := q.Admit(ctx, db.AdmitParams{
			AgentGrn:  agentGRN.String(),
			TokenHash: hash,
			Serial:    presented.Serial,
			NotBefore: stamp(presented.NotBefore),
			NotAfter:  stamp(presented.NotAfter),
		})
		if err != nil {
			return storageError(err, "admit", nil, nil)
		}
		if err := agent.Admission(answer).Err(); err != nil {
			return err
		}
		// The admit above holds the row FOR SHARE, so the lineage read
		// here is the one presented was admitted in.
		exec, err := q.GetExecution(ctx, agentGRN.String())
		if err != nil {
			return storageError(err, "get execution", agent.ErrAgentNotFound, nil)
		}
		if !exec.Lineage.Valid {
			return nil
		}
		if err := q.PruneCredentials(ctx, agentGRN.String()); err != nil {
			return storageError(err, "prune credentials", nil, nil)
		}
		return storageError(q.InsertCredential(ctx, db.InsertCredentialParams{
			AgentGrn: agentGRN.String(), Serial: successor.Serial, Lineage: exec.Lineage, NotAfter: stamp(successor.NotAfter),
		}), "record credential", nil, nil)
	})
}

func activationOf(agentGRN grn.GRN, row db.AgentActivation) *agent.Activation {
	a := &agent.Activation{
		ID:           agent.ActivationID(row.ID.Bytes),
		Agent:        agentGRN,
		Generation:   agent.Generation(row.Generation),
		Epoch:        int(row.Epoch),
		Lineage:      lineageOf(row.Lineage),
		TokenVersion: row.TokenVersion,
	}
	if row.EndedReason.Valid {
		reason := agent.EndReason(row.EndedReason.String)
		a.Ended = &reason
	}
	return a
}
