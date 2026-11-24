package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/cluster/postgres/db"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// inTx runs fn in one transaction that holds g's row first and hands fn
// the database's time, and commits what fn wrote. A cluster no row
// holds is missing.
func (s *Store) inTx(ctx context.Context, g grn.GRN, missing error, fn func(q *db.Queries, now time.Time) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	decided, err := q.HoldCluster(ctx, g.String())
	if err != nil {
		return pgxerr.Classify(err, "hold cluster", missing, nil)
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
	err := s.inTx(ctx, g, cluster.ErrClusterNotFound, func(q *db.Queries, _ time.Time) error {
		if err := q.SweepRegistrationTokens(ctx); err != nil {
			return pgxerr.Classify(err, "sweep registration tokens", nil, nil)
		}
		at, err := q.IssueRegistrationToken(ctx, db.IssueRegistrationTokenParams{
			ClusterGrn: g.String(), TokenHash: hash, TtlSeconds: int64(ttl / time.Second),
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
		return grn.GRN{}, cluster.ErrRegistrationRefused
	}
	if err != nil {
		return grn.GRN{}, pgxerr.Classify(err, "spend registration token", nil, nil)
	}
	if err := s.q.SweepRegistrationTokens(ctx); err != nil {
		return grn.GRN{}, pgxerr.Classify(err, "sweep registration tokens", nil, nil)
	}
	if !row.Live {
		return grn.GRN{}, cluster.ErrRegistrationRefused
	}
	return storedGRN(row.ClusterGrn)
}

// BindKey withdraws g's keys and tokens and binds key.
func (s *Store) BindKey(ctx context.Context, g grn.GRN, key cluster.PublicKey) error {
	jwk, err := key.JWK()
	if err != nil {
		return err
	}
	return s.inTx(ctx, g, cluster.ErrClusterNotFound, func(q *db.Queries, _ time.Time) error {
		if err := q.RevokeAllClusterKeys(ctx, g.String()); err != nil {
			return pgxerr.Classify(err, "revoke cluster keys", nil, nil)
		}
		if err := q.DeleteClusterTokens(ctx, g.String()); err != nil {
			return pgxerr.Classify(err, "delete cluster tokens", nil, nil)
		}
		if err := q.BindClusterKey(ctx, db.BindClusterKeyParams{
			ClusterGrn: g.String(), Kid: key.KID(), PublicJwk: jwk,
		}); err != nil {
			return pgxerr.Classify(err, "bind cluster key", nil, nil)
		}
		return nil
	})
}

// VerificationKey reads g's live key kid.
func (s *Store) VerificationKey(ctx context.Context, g grn.GRN, kid string) (cluster.Key, error) {
	row, err := s.q.GetLiveClusterKey(ctx, db.GetLiveClusterKeyParams{ClusterGrn: g.String(), Kid: kid})
	if err != nil {
		return cluster.Key{}, pgxerr.Classify(err, "get cluster key", operator.ErrClientInvalid, nil)
	}
	return storedKey(row.ID, row.ClusterGrn, row.Kid, row.PublicJwk, row.CreatedAt, row.RetiresAt, row.RevokedAt, true)
}

// RecordAssertionToken admits the assertion and stores t's hash.
func (s *Store) RecordAssertionToken(ctx context.Context, a operator.VerifiedAssertion, t *cluster.Token) error {
	g := a.Operator
	return s.inTx(ctx, g, operator.ErrClientInvalid, func(q *db.Queries, now time.Time) error {
		if err := holdLiveKey(ctx, q, g, a.KeyID, operator.ErrClientInvalid); err != nil {
			return err
		}
		if err := spend(ctx, q, g, now, []operator.VerifiedAssertion{a}, operator.ErrClientInvalid); err != nil {
			return err
		}
		if err := q.SweepClusterTokens(ctx, g.String()); err != nil {
			return pgxerr.Classify(err, "sweep cluster tokens", nil, nil)
		}
		t.IssuedAt, t.ExpiresAt = now, now.Add(cluster.TokenTTL)
		if err := q.RecordClusterToken(ctx, db.RecordClusterTokenParams{
			TokenHash: cluster.HashSecret(t.Value), ClusterGrn: g.String(), KeyID: a.KeyID,
			IssuedAt: pgxtype.Timestamptz(t.IssuedAt), ExpiresAt: pgxtype.Timestamptz(t.ExpiresAt),
		}); err != nil {
			return pgxerr.Classify(err, "record cluster token", nil, nil)
		}
		return nil
	})
}

// ResolveToken reads the live token hash names.
func (s *Store) ResolveToken(ctx context.Context, hash string) (cluster.TokenHolder, error) {
	row, err := s.q.ResolveClusterToken(ctx, hash)
	if err != nil {
		return cluster.TokenHolder{}, pgxerr.Classify(err, "resolve cluster token", cluster.ErrTokenInvalid, nil)
	}
	g, err := storedGRN(row.ClusterGrn)
	if err != nil {
		return cluster.TokenHolder{}, err
	}
	return cluster.TokenHolder{Cluster: g, ExpiresAt: row.ExpiresAt.Time.UTC()}, nil
}

// Rotate adds successor beside g's live keys.
func (s *Store) Rotate(ctx context.Context, g grn.GRN, authorizedBy int64, successor cluster.PublicKey, signed []operator.VerifiedAssertion) (cluster.Rotation, error) {
	jwk, err := successor.JWK()
	if err != nil {
		return cluster.Rotation{}, err
	}
	var rotation cluster.Rotation
	err = s.inTx(ctx, g, cluster.ErrInvalidProof, func(q *db.Queries, now time.Time) error {
		if err := holdLiveKey(ctx, q, g, authorizedBy, cluster.ErrInvalidProof); err != nil {
			return err
		}
		if err := spend(ctx, q, g, now, signed, cluster.ErrInvalidProof); err != nil {
			return err
		}
		keys, err := q.ListClusterKeys(ctx, g.String())
		if err != nil {
			return pgxerr.Classify(err, "list cluster keys", nil, nil)
		}
		var live []db.ListClusterKeysRow
		for _, k := range keys {
			if k.Kid == successor.KID() {
				return repeated(keys, k, &rotation)
			}
			if k.Live.Bool {
				live = append(live, k)
			}
		}
		// Two live keys refuse a third; one is the predecessor.
		if len(live) >= cluster.LiveKeysPerCluster {
			return cluster.ErrTooManyKeys
		}
		predecessor := live[0]
		retires, err := q.RetireClusterKey(ctx, db.RetireClusterKeyParams{
			WindowSeconds: int64(cluster.RolloverWindow / time.Second), ID: predecessor.ID,
		})
		if err != nil {
			return pgxerr.Classify(err, "retire cluster key", nil, nil)
		}
		rotation.PredecessorRetiresAt = retires.Time.UTC()
		if err := q.AddClusterKey(ctx, db.AddClusterKeyParams{ClusterGrn: g.String(), Kid: successor.KID(), PublicJwk: jwk,
			PredecessorID: pgtype.Int8{Int64: predecessor.ID, Valid: true}}); err != nil {
			return pgxerr.Classify(err, "add cluster key", nil, nil)
		}
		rotation.KID = successor.KID()
		return nil
	})
	return rotation, err
}

// repeated answers a rotation to a key the cluster already holds: as
// the rotation that made it live answered, while it is live and a
// rotation made it so, and ErrKeyHeld otherwise (ROT-4).
func repeated(keys []db.ListClusterKeysRow, held db.ListClusterKeysRow, r *cluster.Rotation) error {
	if !held.Live.Bool || !held.PredecessorID.Valid {
		return cluster.ErrKeyHeld
	}
	for _, k := range keys {
		if k.ID == held.PredecessorID.Int64 {
			*r = cluster.Rotation{KID: held.Kid, PredecessorRetiresAt: k.RetiresAt.Time.UTC()}
			return nil
		}
	}
	return errorx.New(errorx.KindInternal, "cluster: a rotated key's predecessor is not held")
}

// ListKeys reads every key g held.
func (s *Store) ListKeys(ctx context.Context, g grn.GRN) ([]cluster.Key, error) {
	if _, err := s.Get(ctx, g); err != nil {
		return nil, err
	}
	rows, err := s.q.ListClusterKeys(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list cluster keys", nil, nil)
	}
	keys := make([]cluster.Key, 0, len(rows))
	for _, row := range rows {
		k, err := storedKey(row.ID, row.ClusterGrn, row.Kid, row.PublicJwk, row.CreatedAt, row.RetiresAt, row.RevokedAt, row.Live.Bool)
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// RevokeKey withdraws g's key kid and its tokens.
func (s *Store) RevokeKey(ctx context.Context, g grn.GRN, kid string) error {
	return s.inTx(ctx, g, cluster.ErrClusterNotFound, func(q *db.Queries, _ time.Time) error {
		id, err := q.RevokeClusterKey(ctx, db.RevokeClusterKeyParams{ClusterGrn: g.String(), Kid: kid})
		if errors.Is(err, pgx.ErrNoRows) {
			held, err := q.ClusterKeyExists(ctx, db.ClusterKeyExistsParams{ClusterGrn: g.String(), Kid: kid})
			if err != nil {
				return pgxerr.Classify(err, "read cluster key", nil, nil)
			}
			if !held {
				return cluster.ErrKeyNotFound
			}
			return nil
		}
		if err != nil {
			return pgxerr.Classify(err, "revoke cluster key", nil, nil)
		}
		if err := q.DeleteClusterKeyTokens(ctx, id); err != nil {
			return pgxerr.Classify(err, "delete cluster key tokens", nil, nil)
		}
		return nil
	})
}

// holdLiveKey holds the key row id while it is g's and live, or answers
// refused.
func holdLiveKey(ctx context.Context, q *db.Queries, g grn.GRN, id int64, refused error) error {
	if _, err := q.HoldLiveClusterKey(ctx, db.HoldLiveClusterKeyParams{ID: id, ClusterGrn: g.String()}); err != nil {
		return pgxerr.Classify(err, "hold cluster key", refused, nil)
	}
	return nil
}

// spend checks each signed request's dates at now and spends its jti,
// answering refused for an untimely one or a jti spent before.
func spend(ctx context.Context, q *db.Queries, g grn.GRN, now time.Time, signed []operator.VerifiedAssertion, refused error) error {
	if err := q.SweepClusterJTIs(ctx, g.String()); err != nil {
		return pgxerr.Classify(err, "sweep cluster jtis", nil, nil)
	}
	for _, a := range signed {
		if !a.Timely(now) {
			return refused
		}
		if _, err := q.SpendClusterJTI(ctx, db.SpendClusterJTIParams{
			ClusterGrn: g.String(), Jti: a.JTI, ExpiresAt: pgxtype.Timestamptz(a.ReplayUntil()),
		}); err != nil {
			return pgxerr.Classify(err, "spend cluster jti", refused, nil)
		}
	}
	return nil
}

func storedGRN(s string) (grn.GRN, error) {
	g, err := grn.Parse(s)
	if err != nil || g.ServiceType != cluster.ServiceType {
		return grn.GRN{}, errorx.New(errorx.KindInternal, "stored cluster grn "+s+" is not a cluster's")
	}
	return g, nil
}

// storedKey parses a key row, which binding already constrained, so a
// failure is the database's fault.
func storedKey(id int64, clusterGRN, kid, jwk string, createdAt, retiresAt, revokedAt pgtype.Timestamptz, live bool) (cluster.Key, error) {
	g, err := storedGRN(clusterGRN)
	if err != nil {
		return cluster.Key{}, err
	}
	public, err := operator.ParsePublicJWK(jwk, kid)
	if err != nil {
		return cluster.Key{}, errorx.Wrap(err, "stored cluster key", errorx.KindInternal)
	}
	k := cluster.Key{ID: id, Cluster: g, KID: kid, Public: public, CreatedAt: createdAt.Time.UTC(),
		RetiresAt: pgxtype.Time(retiresAt), RevokedAt: pgxtype.Time(revokedAt)}
	switch {
	case !live:
		k.State = cluster.KeyStateRevoked
	case retiresAt.Valid:
		k.State = cluster.KeyStateRetiring
	default:
		k.State = cluster.KeyStateActive
	}
	return k, nil
}
