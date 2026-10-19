package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/operator/postgres/db"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/pgxtype"
)

// IssueKey records the key under the registration's row lock, so two
// issuances at once read the same live keys one after the other and
// the bound holds. An immediate rotation withdraws the live keys and
// deletes their tokens before the insert, in the same transaction, so
// no moment exists where the old key and the new one both verify.
func (s *Store) IssueKey(ctx context.Context, k *operator.IssuedKey, rotation operator.KeyRotation) (operator.Key, error) {
	jwk, err := k.PublicJWK()
	if err != nil {
		return operator.Key{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return operator.Key{}, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	operatorGRN := k.Operator().String()
	if _, err := q.LockOperatorRegistration(ctx, operatorGRN); err != nil {
		return operator.Key{}, pgxerr.Classify(err, "hold operator", operator.ErrOperatorNotFound, nil)
	}
	live, err := q.ListLiveOperatorKeys(ctx, operatorGRN)
	if err != nil {
		return operator.Key{}, pgxerr.Classify(err, "read operator keys", nil, nil)
	}
	switch rotation {
	case operator.KeyRotationFirst:
		// Any row refuses, a withdrawn one included: re-keying after a
		// revocation is operator:reissue (ADR-0102).
		keyed, err := q.OperatorWasKeyed(ctx, operatorGRN)
		if err != nil {
			return operator.Key{}, pgxerr.Classify(err, "read operator keys", nil, nil)
		}
		if keyed {
			return operator.Key{}, operator.ErrOperatorWasKeyed
		}
	case operator.KeyRotationPlanned:
		if len(live) >= operator.LiveKeysPerOperator {
			return operator.Key{}, operator.ErrKeyLimit
		}
		for _, held := range live {
			if err := q.RetireOperatorKey(ctx, db.RetireOperatorKeyParams{
				WindowSeconds: int64(operator.RolloverWindow / time.Second), ID: held.ID,
			}); err != nil {
				return operator.Key{}, pgxerr.Classify(err, "retire operator key", nil, nil)
			}
		}
	case operator.KeyRotationImmediate:
		revoked, err := q.RevokeLiveOperatorKeys(ctx, operatorGRN)
		if err != nil {
			return operator.Key{}, pgxerr.Classify(err, "revoke operator keys", nil, nil)
		}
		if err := q.DeleteOperatorKeyTokens(ctx, revoked); err != nil {
			return operator.Key{}, pgxerr.Classify(err, "void operator key tokens", nil, nil)
		}
	default:
		return operator.Key{}, errorx.New(errorx.KindInvalidArgument, "operator key: unknown rotation "+string(rotation))
	}
	id, err := q.InsertOperatorKey(ctx, db.InsertOperatorKeyParams{
		OperatorGrn: operatorGRN,
		Kid:         k.KID(),
		Algorithm:   operator.KeyAlgorithm,
		PublicJwk:   jwk,
		IssuedBy:    k.IssuedBy().String(),
		IssuedAt:    pgxtype.Timestamptz(k.IssuedAt()),
	})
	if err != nil {
		return operator.Key{}, pgxerr.Classify(err, "record operator key", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return operator.Key{}, errorx.Wrap(err, "commit", errorx.KindUnavailable)
	}
	return operator.Key{
		ID: id, Operator: k.Operator(), KID: k.KID(), Algorithm: operator.KeyAlgorithm, Public: k.Public(),
		IssuedBy: k.IssuedBy(), IssuedAt: k.IssuedAt(), State: operator.KeyStateActive,
	}, nil
}

// ListKeys reads every key the operator was issued, with the state
// the database's time puts each in.
func (s *Store) ListKeys(ctx context.Context, g grn.GRN) ([]operator.Key, error) {
	if _, err := s.q.GetOperator(ctx, g.String()); err != nil {
		return nil, pgxerr.Classify(err, "read operator", operator.ErrOperatorNotFound, nil)
	}
	rows, err := s.q.ListOperatorKeys(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list operator keys", nil, nil)
	}
	keys := make([]operator.Key, 0, len(rows))
	for _, row := range rows {
		state, err := operator.ParseKeyState(row.State)
		if err != nil {
			return nil, err
		}
		key, err := storedKey(row.ID, row.OperatorGrn, row.Kid, row.Algorithm, row.PublicJwk, row.IssuedBy,
			row.IssuedAt, row.RetiresAt, row.RevokedAt, row.LastExchangedAt)
		if err != nil {
			return nil, err
		}
		key.State = state
		keys = append(keys, key)
	}
	return keys, nil
}

// RevokeKey withdraws the key and deletes its tokens in one
// transaction, under the registration's row lock, which every exchange
// of the operator holds first too. The two serialize on it: an
// exchange either commits first and its token is deleted here, or
// rereads the key withdrawn and records nothing (#1418).
func (s *Store) RevokeKey(ctx context.Context, g grn.GRN, kid string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	// An operator the organization holds no record of was never issued
	// the key.
	if _, err := q.LockOperatorRegistration(ctx, g.String()); err != nil {
		return pgxerr.Classify(err, "hold operator", operator.ErrKeyNotFound, nil)
	}
	id, err := q.RevokeOperatorKey(ctx, db.RevokeOperatorKeyParams{OperatorGrn: g.String(), Kid: kid})
	if err := pgxerr.Classify(err, "revoke operator key", errNoLiveKey, nil); err != nil {
		if !errors.Is(err, errNoLiveKey) {
			return err
		}
		exists, err := q.OperatorKeyExists(ctx, db.OperatorKeyExistsParams{OperatorGrn: g.String(), Kid: kid})
		if err != nil {
			return pgxerr.Classify(err, "read operator key", nil, nil)
		}
		if !exists {
			return operator.ErrKeyNotFound
		}
		return nil
	}
	if err := q.DeleteOperatorKeyTokens(ctx, []int64{id}); err != nil {
		return pgxerr.Classify(err, "void operator key tokens", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "commit", errorx.KindUnavailable)
	}
	return nil
}

// VerificationKey reads the live key kid names, within client's
// registration and joined to it, so a deleted operator answers no key.
func (s *Store) VerificationKey(ctx context.Context, client grn.GRN, kid string) (operator.Key, error) {
	row, err := s.q.GetLiveOperatorKey(ctx, db.GetLiveOperatorKeyParams{OperatorGrn: client.String(), Kid: kid})
	if err != nil {
		return operator.Key{}, pgxerr.Classify(err, "read operator key", operator.ErrClientInvalid, nil)
	}
	key, err := storedKey(row.ID, row.OperatorGrn, row.Kid, row.Algorithm, row.PublicJwk, row.IssuedBy,
		row.IssuedAt, row.RetiresAt, row.RevokedAt, row.LastExchangedAt)
	if err != nil {
		return operator.Key{}, err
	}
	key.State = operator.KeyStateActive
	if key.RetiresAt != nil {
		key.State = operator.KeyStateRetiring
	}
	return key, nil
}

// RecordAssertionToken admits the assertion and records the token in
// one transaction that holds the registrations and then the key's row
// from the reread to the commit, the order IssueKey takes them in, so
// an exchange racing a rotation waits rather than deadlocks. The
// (client, jti) primary key is what refuses a replay on any replica: a
// concurrent spend of the same pair waits on the first and conflicts
// once it commits.
func (s *Store) RecordAssertionToken(ctx context.Context, a operator.VerifiedAssertion, t *operator.Token) error {
	audience := pgtype.Text{}
	if t.Audience != nil {
		audience = pgtype.Text{String: t.Audience.String(), Valid: true}
	}
	client := a.Operator.String()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := s.q.WithTx(tx)
	if err := holdRegistrations(ctx, q, client, t.Audience, operator.ErrClientInvalid); err != nil {
		return err
	}
	decided, err := q.HoldLiveOperatorKey(ctx, db.HoldLiveOperatorKeyParams{ID: a.KeyID, OperatorGrn: client})
	if err != nil {
		return pgxerr.Classify(err, "hold operator key", operator.ErrClientInvalid, nil)
	}
	now := decided.Time.UTC()
	if !a.Timely(now) {
		return operator.ErrClientInvalid
	}
	if err := q.SweepOperatorAssertions(ctx, client); err != nil {
		return pgxerr.Classify(err, "sweep operator assertions", nil, nil)
	}
	if _, err := q.SpendOperatorAssertion(ctx, db.SpendOperatorAssertionParams{
		OperatorGrn: client, Jti: a.JTI, ExpiresAt: pgxtype.Timestamptz(a.ReplayUntil()),
	}); err != nil {
		return pgxerr.Classify(err, "spend operator assertion", operator.ErrClientInvalid, nil)
	}
	t.IssuedAt, t.ExpiresAt = now, now.Add(operator.TokenTTL)
	err = recordTokens(ctx, q, client,
		func() error {
			return q.RecordOperatorKeyToken(ctx, db.RecordOperatorKeyTokenParams{
				TokenHash:   t.Hash(),
				KeyID:       a.KeyID,
				OperatorGrn: client,
				AudienceGrn: audience,
				IssuedAt:    pgxtype.Timestamptz(t.IssuedAt),
				ExpiresAt:   pgxtype.Timestamptz(t.ExpiresAt),
			})
		},
		func() error {
			return q.TrimOperatorKeyTokens(ctx, db.TrimOperatorKeyTokensParams{
				KeyID: a.KeyID, Keep: operator.LiveTokensPerKey,
			})
		})
	if err != nil {
		return err
	}
	if err := q.MarkOperatorKeyExchanged(ctx, db.MarkOperatorKeyExchangedParams{At: decided, ID: a.KeyID}); err != nil {
		return pgxerr.Classify(err, "mark operator key exchanged", nil, nil)
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "commit", errorx.KindUnavailable)
	}
	return nil
}

// errNoLiveKey is RevokeKey's reading of a key no update withdrew:
// already withdrawn, or never issued, which a second read tells apart.
var errNoLiveKey = errors.New("no live operator key")

// storedKey parses a key row, which the schema and issuance already
// constrained, so a failure is the database's fault.
func storedKey(id int64, operatorGRN, kid, algorithm, jwk, issuedBy string,
	issuedAt, retiresAt, revokedAt, lastExchangedAt pgtype.Timestamptz) (operator.Key, error) {
	owner, err := storedOperatorGRN(operatorGRN)
	if err != nil {
		return operator.Key{}, err
	}
	by, err := grn.Parse(issuedBy)
	if err != nil {
		return operator.Key{}, errorx.Wrap(err, "stored operator key issuer", errorx.KindInternal)
	}
	if algorithm != operator.KeyAlgorithm {
		return operator.Key{}, operator.ErrKeyInvalid
	}
	public, err := operator.ParsePublicJWK(jwk, kid)
	if err != nil {
		return operator.Key{}, err
	}
	return operator.Key{
		ID: id, Operator: owner, KID: kid, Algorithm: algorithm, Public: public, IssuedBy: by,
		IssuedAt: issuedAt.Time.UTC(), RetiresAt: optionalTime(retiresAt), RevokedAt: optionalTime(revokedAt),
		LastExchangedAt: optionalTime(lastExchangedAt),
	}, nil
}

// optionalTime renders a nullable timestamp as a time or nil.
func optionalTime(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	at := t.Time.UTC()
	return &at
}
