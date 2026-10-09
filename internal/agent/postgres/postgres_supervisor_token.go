package postgres

import (
	"context"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/agent/postgres/db"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Compile-time check that Store satisfies agent.SupervisorTokenStore.
var _ agent.SupervisorTokenStore = (*Store)(nil)

func (s *Store) SupervisorCredentialHolder(ctx context.Context, credentialHash string) (agent.SupervisorCredentialHolder, error) {
	row, err := s.q.GetSupervisorCredentialHolder(ctx, credentialHash)
	if err != nil {
		return agent.SupervisorCredentialHolder{}, storageError(err, "read supervisor credential", agent.ErrSupervisorCredentialInvalid, nil)
	}
	g, err := grn.Parse(row.SupervisorGrn)
	if err != nil {
		return agent.SupervisorCredentialHolder{}, errorx.Wrap(err, "stored supervisor grn", errorx.KindInternal)
	}
	return agent.SupervisorCredentialHolder{ID: row.ID, Supervisor: g}, nil
}

// RecordSupervisorToken holds the credential issuance's row while it
// records and trims, so two exchanges at once stay inside the bound;
// finding no row is a re-issue or a revocation committed meanwhile.
func (s *Store) RecordSupervisorToken(ctx context.Context, holder agent.SupervisorCredentialHolder, t *agent.SupervisorToken) error {
	return s.inTx(ctx, "record supervisor token", func(q *db.Queries) error {
		if _, err := q.LockSupervisorCredential(ctx, holder.ID); err != nil {
			return storageError(err, "hold supervisor credential", agent.ErrSupervisorCredentialInvalid, nil)
		}
		if err := q.RecordSupervisorToken(ctx, db.RecordSupervisorTokenParams{
			TokenHash: t.Hash(), CredentialID: holder.ID, IssuedAt: stamp(t.IssuedAt), ExpiresAt: stamp(t.ExpiresAt),
		}); err != nil {
			return storageError(err, "record supervisor token", nil, nil)
		}
		return storageError(q.TrimSupervisorTokens(ctx, db.TrimSupervisorTokensParams{
			CredentialID: holder.ID, Keep: agent.LiveSupervisorTokensPerCredential,
		}), "trim supervisor tokens", nil, nil)
	})
}

// ResolveSupervisorToken answers what a live token names. The statement
// joins a registration that has not ended and filters on expiry, so a
// revocation, a replacement, a re-issue and an expiry are one answer:
// none.
func (s *Store) ResolveSupervisorToken(ctx context.Context, tokenHash string) (agent.SupervisorTokenHolder, error) {
	row, err := s.q.ResolveSupervisorToken(ctx, tokenHash)
	if err != nil {
		return agent.SupervisorTokenHolder{}, storageError(err, "resolve supervisor token", agent.ErrSupervisorTokenInvalid, nil)
	}
	g, err := grn.Parse(row.SupervisorGrn)
	if err != nil {
		return agent.SupervisorTokenHolder{}, errorx.Wrap(err, "stored supervisor grn", errorx.KindInternal)
	}
	return agent.SupervisorTokenHolder{Supervisor: g, IssuedAt: row.IssuedAt.Time, ExpiresAt: row.ExpiresAt.Time}, nil
}
