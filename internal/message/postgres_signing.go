package message

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/message/db"
	"github.com/garamsh/garam/internal/pgxerr"
	"github.com/garamsh/garam/internal/pgxtype"
)

// Compile-time check that PostgresStore holds the signing keys too.
var _ SigningKeyStore = (*PostgresStore)(nil)

// SigningKeys reads every key and the database's clock in one
// transaction, so the keys are judged at the moment they were read.
func (s *PostgresStore) SigningKeys(ctx context.Context) ([]*SigningKey, time.Time, error) {
	var keys []*SigningKey
	var now time.Time
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		var err error
		keys, now, err = readSigningKeys(ctx, s.q.WithTx(tx))
		return err
	})
	if err != nil {
		return nil, time.Time{}, errorx.Wrap(err, "signing keys")
	}
	return keys, now, nil
}

// RotateSigningKeys runs prepare under the rotation lock, so two
// replicas that both found a key due store one between them. The keys
// and the clock are read after the lock is held, and the key stored
// signs from that same instant plus its lead.
func (s *PostgresStore) RotateSigningKeys(ctx context.Context, prepare func(keys []*SigningKey, now time.Time) (*NewSigningKey, error)) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.q.WithTx(tx)
		if err := q.LockSigningKeys(ctx); err != nil {
			return pgxerr.Classify(err, "lock signing keys", nil, nil)
		}
		keys, now, err := readSigningKeys(ctx, q)
		if err != nil {
			return err
		}
		key, err := prepare(keys, now)
		if err != nil || key == nil {
			return err
		}
		params := db.InsertSigningKeyParams{
			ID:        string(key.ID),
			PublicKey: key.PublicKey,
			SignsFrom: pgxtype.Timestamptz(now.Add(key.Lead)),
		}
		switch held := key.Held.(type) {
		case SealedKey:
			params.SealedPrivateKey = held
		case TransitVersion:
			params.TransitMount = pgtype.Text{String: held.Mount, Valid: true}
			params.TransitKey = pgtype.Text{String: held.Key, Valid: true}
			params.TransitVersion = pgtype.Int4{Int32: int32(held.Version), Valid: true}
		default:
			return errorx.New(errorx.KindInternal, "insert signing key: no holding for its private half")
		}
		return pgxerr.Classify(q.InsertSigningKey(ctx, params), "insert signing key", nil, nil)
	})
	return errorx.Wrap(err, "rotate signing keys")
}

// WithdrawSigningKey stops a key signing and publishing at once.
func (s *PostgresStore) WithdrawSigningKey(ctx context.Context, id KeyID) error {
	_, err := s.q.WithdrawSigningKey(ctx, string(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoSuchSigningKey
	}
	return pgxerr.Classify(err, "withdraw signing key", nil, nil)
}

// readSigningKeys reads the keys and the clock through q, which the
// caller has put in one transaction.
func readSigningKeys(ctx context.Context, q *db.Queries) ([]*SigningKey, time.Time, error) {
	rows, err := q.SigningKeys(ctx)
	if err != nil {
		return nil, time.Time{}, pgxerr.Classify(err, "signing keys", nil, nil)
	}
	now, err := q.DatabaseNow(ctx)
	if err != nil {
		return nil, time.Time{}, pgxerr.Classify(err, "database now", nil, nil)
	}
	keys := make([]*SigningKey, 0, len(rows))
	for _, row := range rows {
		key := &SigningKey{
			ID:        KeyID(row.ID),
			PublicKey: row.PublicKey,
			Held:      heldIn(row),
			SignsFrom: row.SignsFrom.Time,
		}
		if row.WithdrawnAt.Valid {
			withdrawn := row.WithdrawnAt.Time
			key.WithdrawnAt = &withdrawn
		}
		keys = append(keys, key)
	}
	return keys, now.Time, nil
}

// heldIn reads where a row's private half is held. The table's check
// admits a row holding exactly one of the two, so a row naming no
// Transit version is a sealed one.
func heldIn(row db.MessageSigningKey) Holding {
	if row.TransitVersion.Valid {
		return TransitVersion{Mount: row.TransitMount.String, Key: row.TransitKey.String, Version: int(row.TransitVersion.Int32)}
	}
	return SealedKey(row.SealedPrivateKey)
}
