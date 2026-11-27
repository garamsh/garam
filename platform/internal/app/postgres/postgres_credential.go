package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/app/postgres/db"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// inTx runs fn in one transaction that holds g's row first and hands fn
// the database's time, and commits what fn wrote. An instance no row
// holds is missing.
func (s *Store) inTx(ctx context.Context, g grn.GRN, missing error, fn func(q *db.Queries, now time.Time) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	decided, err := q.HoldInstance(ctx, g.String())
	if err != nil {
		return pgxerr.Classify(err, "hold instance", missing, nil)
	}
	if err := fn(q, decided.Time.UTC()); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "commit", errorx.KindUnavailable)
	}
	return nil
}

// IssueRegistrationToken replaces g's token with hash.
func (s *Store) IssueRegistrationToken(ctx context.Context, g grn.GRN, hash string, ttl time.Duration) (time.Time, error) {
	var expiresAt time.Time
	err := s.inTx(ctx, g, app.ErrInstanceNotFound, func(q *db.Queries, _ time.Time) error {
		if err := q.SweepRegistrationTokens(ctx); err != nil {
			return pgxerr.Classify(err, "sweep registration tokens", nil, nil)
		}
		at, err := q.IssueRegistrationToken(ctx, db.IssueRegistrationTokenParams{
			InstanceGrn: g.String(), TokenHash: hash, TtlSeconds: int64(ttl / time.Second),
		})
		if err != nil {
			return pgxerr.Classify(err, "issue registration token", nil, nil)
		}
		expiresAt = at.Time.UTC()
		return nil
	})
	return expiresAt, err
}

// SpendRegistrationToken deletes the token's row in the statement that
// reads it, and sweeps every expired one after.
func (s *Store) SpendRegistrationToken(ctx context.Context, hash string) (grn.GRN, error) {
	row, err := s.q.SpendRegistrationToken(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return grn.GRN{}, app.ErrRegistrationRefused
	}
	if err != nil {
		return grn.GRN{}, pgxerr.Classify(err, "spend registration token", nil, nil)
	}
	if err := s.q.SweepRegistrationTokens(ctx); err != nil {
		return grn.GRN{}, pgxerr.Classify(err, "sweep registration tokens", nil, nil)
	}
	if !row.Live {
		return grn.GRN{}, app.ErrRegistrationRefused
	}
	return storedGRN(row.InstanceGrn)
}

// BindKey withdraws g's keys and its tokens, and binds key.
func (s *Store) BindKey(ctx context.Context, g grn.GRN, key app.PublicKey) error {
	jwk, err := key.JWK()
	if err != nil {
		return err
	}
	return s.inTx(ctx, g, app.ErrInstanceNotFound, func(q *db.Queries, _ time.Time) error {
		if err := q.RevokeAllInstanceKeys(ctx, g.String()); err != nil {
			return pgxerr.Classify(err, "revoke instance keys", nil, nil)
		}
		if err := q.DeleteInstanceTokens(ctx, g.String()); err != nil {
			return pgxerr.Classify(err, "delete instance tokens", nil, nil)
		}
		if err := q.BindInstanceKey(ctx, db.BindInstanceKeyParams{
			InstanceGrn: g.String(), Kid: key.KID(), PublicJwk: jwk,
		}); err != nil {
			return pgxerr.Classify(err, "bind instance key", nil, nil)
		}
		return nil
	})
}

// VerificationKey reads g's live key kid.
func (s *Store) VerificationKey(ctx context.Context, g grn.GRN, kid string) (app.Key, error) {
	row, err := s.q.GetLiveInstanceKey(ctx, db.GetLiveInstanceKeyParams{InstanceGrn: g.String(), Kid: kid})
	if err != nil {
		return app.Key{}, pgxerr.Classify(err, "get instance key", operator.ErrClientInvalid, nil)
	}
	return storedKey(row.ID, row.InstanceGrn, row.Kid, row.PublicJwk, row.CreatedAt, row.RetiresAt, row.RevokedAt, true)
}

// RecordAssertionToken admits the assertion and stores t's hash.
func (s *Store) RecordAssertionToken(ctx context.Context, a operator.VerifiedAssertion, t *app.Token) error {
	g := a.Operator
	return s.inTx(ctx, g, operator.ErrClientInvalid, func(q *db.Queries, now time.Time) error {
		if err := holdLiveKey(ctx, q, g, a.KeyID, operator.ErrClientInvalid); err != nil {
			return err
		}
		if err := spend(ctx, q, g, now, []operator.VerifiedAssertion{a}, operator.ErrClientInvalid); err != nil {
			return err
		}
		if err := q.SweepInstanceTokens(ctx, g.String()); err != nil {
			return pgxerr.Classify(err, "sweep instance tokens", nil, nil)
		}
		t.IssuedAt, t.ExpiresAt = now, now.Add(app.TokenTTL)
		if err := q.RecordInstanceToken(ctx, db.RecordInstanceTokenParams{
			TokenHash: app.HashSecret(t.Value), InstanceGrn: g.String(), KeyID: a.KeyID,
			IssuedAt: pgxtype.Timestamptz(t.IssuedAt), ExpiresAt: pgxtype.Timestamptz(t.ExpiresAt),
		}); err != nil {
			return pgxerr.Classify(err, "record instance token", nil, nil)
		}
		return nil
	})
}

// ResolveToken reads the live token hash names.
func (s *Store) ResolveToken(ctx context.Context, hash string) (app.TokenHolder, error) {
	row, err := s.q.ResolveInstanceToken(ctx, hash)
	if err != nil {
		return app.TokenHolder{}, pgxerr.Classify(err, "resolve instance token", app.ErrTokenInvalid, nil)
	}
	g, err := storedGRN(row.InstanceGrn)
	if err != nil {
		return app.TokenHolder{}, err
	}
	installation, err := storedGRNs("installation", row.InstallationGrn)
	if err != nil {
		return app.TokenHolder{}, err
	}
	return app.TokenHolder{Instance: g, Installation: installation[0], RequestsBotHosting: row.RequestsBotHosting,
		ExpiresAt:  row.ExpiresAt.Time.UTC(),
		Operations: row.MachineOperations}, nil
}

// Rotate adds successor beside g's live keys.
func (s *Store) Rotate(ctx context.Context, g grn.GRN, authorizedBy int64, successor app.PublicKey, signed []operator.VerifiedAssertion) (app.Rotation, error) {
	jwk, err := successor.JWK()
	if err != nil {
		return app.Rotation{}, err
	}
	var rotation app.Rotation
	err = s.inTx(ctx, g, app.ErrInvalidProof, func(q *db.Queries, now time.Time) error {
		if err := holdLiveKey(ctx, q, g, authorizedBy, app.ErrInvalidProof); err != nil {
			return err
		}
		if err := spend(ctx, q, g, now, signed, app.ErrInvalidProof); err != nil {
			return err
		}
		keys, err := q.ListInstanceKeys(ctx, g.String())
		if err != nil {
			return pgxerr.Classify(err, "list instance keys", nil, nil)
		}
		var live []db.ListInstanceKeysRow
		for _, k := range keys {
			if k.Kid == successor.KID() {
				return repeated(keys, k, &rotation)
			}
			if k.Live.Bool {
				live = append(live, k)
			}
		}
		// Two live keys refuse a third; one is the predecessor.
		if len(live) >= app.LiveKeysPerInstance {
			return app.ErrTooManyKeys
		}
		predecessor := live[0]
		retires, err := q.RetireInstanceKey(ctx, db.RetireInstanceKeyParams{
			WindowSeconds: int64(app.RolloverWindow / time.Second), ID: predecessor.ID,
		})
		if err != nil {
			return pgxerr.Classify(err, "retire instance key", nil, nil)
		}
		rotation.PredecessorRetiresAt = retires.Time.UTC()
		if err := q.AddInstanceKey(ctx, db.AddInstanceKeyParams{InstanceGrn: g.String(), Kid: successor.KID(), PublicJwk: jwk,
			PredecessorID: pgtype.Int8{Int64: predecessor.ID, Valid: true}}); err != nil {
			return pgxerr.Classify(err, "add instance key", nil, nil)
		}
		rotation.KID = successor.KID()
		return nil
	})
	return rotation, err
}

// repeated answers a rotation to a key the instance already holds: as
// the rotation that made it live answered, while it is live and a
// rotation made it so, and ErrKeyHeld otherwise (ROT-4).
func repeated(keys []db.ListInstanceKeysRow, held db.ListInstanceKeysRow, r *app.Rotation) error {
	if !held.Live.Bool || !held.PredecessorID.Valid {
		return app.ErrKeyHeld
	}
	for _, k := range keys {
		if k.ID == held.PredecessorID.Int64 {
			*r = app.Rotation{KID: held.Kid, PredecessorRetiresAt: k.RetiresAt.Time.UTC()}
			return nil
		}
	}
	return errorx.New(errorx.KindInternal, "instance: a rotated key's predecessor is not held")
}

// ListKeys reads every key g held.
func (s *Store) ListKeys(ctx context.Context, g grn.GRN) ([]app.Key, error) {
	if _, err := s.GetInstance(ctx, g); err != nil {
		return nil, err
	}
	rows, err := s.q.ListInstanceKeys(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list instance keys", nil, nil)
	}
	keys := make([]app.Key, 0, len(rows))
	for _, row := range rows {
		k, err := storedKey(row.ID, row.InstanceGrn, row.Kid, row.PublicJwk, row.CreatedAt, row.RetiresAt, row.RevokedAt, row.Live.Bool)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// RevokeKey withdraws g's key kid and its tokens.
func (s *Store) RevokeKey(ctx context.Context, g grn.GRN, kid string) error {
	return s.inTx(ctx, g, app.ErrInstanceNotFound, func(q *db.Queries, _ time.Time) error {
		id, err := q.RevokeInstanceKey(ctx, db.RevokeInstanceKeyParams{InstanceGrn: g.String(), Kid: kid})
		if errors.Is(err, pgx.ErrNoRows) {
			held, err := q.InstanceKeyExists(ctx, db.InstanceKeyExistsParams{InstanceGrn: g.String(), Kid: kid})
			if err != nil {
				return pgxerr.Classify(err, "read instance key", nil, nil)
			}
			if !held {
				return app.ErrKeyNotFound
			}
			return nil
		}
		if err != nil {
			return pgxerr.Classify(err, "revoke instance key", nil, nil)
		}
		if err := q.DeleteInstanceKeyTokens(ctx, id); err != nil {
			return pgxerr.Classify(err, "delete instance key tokens", nil, nil)
		}
		return nil
	})
}

// holdLiveKey holds the key row id while it is g's and live, or answers
// refused.
func holdLiveKey(ctx context.Context, q *db.Queries, g grn.GRN, id int64, refused error) error {
	if _, err := q.HoldLiveInstanceKey(ctx, db.HoldLiveInstanceKeyParams{ID: id, InstanceGrn: g.String()}); err != nil {
		return pgxerr.Classify(err, "hold instance key", refused, nil)
	}
	return nil
}

// spend checks each signed request's dates at now and spends its jti,
// answering refused for an untimely one or a jti spent before.
func spend(ctx context.Context, q *db.Queries, g grn.GRN, now time.Time, signed []operator.VerifiedAssertion, refused error) error {
	if err := q.SweepInstanceJTIs(ctx, g.String()); err != nil {
		return pgxerr.Classify(err, "sweep instance jtis", nil, nil)
	}
	for _, a := range signed {
		if !a.Timely(now) {
			return refused
		}
		if _, err := q.SpendInstanceJTI(ctx, db.SpendInstanceJTIParams{
			InstanceGrn: g.String(), Jti: a.JTI, ExpiresAt: pgxtype.Timestamptz(a.ReplayUntil()),
		}); err != nil {
			return pgxerr.Classify(err, "spend instance jti", refused, nil)
		}
	}
	return nil
}

func storedGRN(s string) (grn.GRN, error) {
	g, err := grn.Parse(s)
	if err != nil || !app.IsInstanceGRN(g) {
		return grn.GRN{}, errorx.New(errorx.KindInternal, "stored instance grn "+s+" is not an instance's")
	}
	return g, nil
}

// storedKey parses a key row, which binding already constrained, so a
// failure is the database's fault.
func storedKey(id int64, instanceGRN, kid, jwk string, createdAt, retiresAt, revokedAt pgtype.Timestamptz, live bool) (app.Key, error) {
	g, err := storedGRN(instanceGRN)
	if err != nil {
		return app.Key{}, err
	}
	public, err := operator.ParsePublicJWK(jwk, kid)
	if err != nil {
		return app.Key{}, errorx.Wrap(err, "stored instance key", errorx.KindInternal)
	}
	k := app.Key{ID: id, Instance: g, KID: kid, Public: public, CreatedAt: createdAt.Time.UTC(),
		RetiresAt: pgxtype.Time(retiresAt), RevokedAt: pgxtype.Time(revokedAt)}
	switch {
	case !live:
		k.State = app.KeyStateRevoked
	case retiresAt.Valid:
		k.State = app.KeyStateRetiring
	default:
		k.State = app.KeyStateActive
	}
	return k, nil
}
