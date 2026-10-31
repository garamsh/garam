package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/cowork/internal/artifact"
)

// transaction stands in for the pool and the transaction it opens.
type transaction struct {
	pgx.Tx
	beginErr, commitErr error
}

func (x *transaction) Begin(context.Context) (pgx.Tx, error) {
	if x.beginErr != nil {
		return nil, x.beginErr
	}
	return x, nil
}

func (x *transaction) Commit(context.Context) error { return x.commitErr }

func (x *transaction) Rollback(context.Context) error { return pgx.ErrTxClosed }

// TestRecord_OnlyACommitIsInDoubt: nothing sent before COMMIT can
// stand, so only a failed commit leaves the record in doubt.
func TestRecord_OnlyACommitIsInDoubt(t *testing.T) {
	failure := errors.New("connection lost")
	tests := []struct {
		name    string
		tx      transaction
		fnErr   error
		inDoubt bool
	}{
		{name: "the commit fails", tx: transaction{commitErr: failure}, inDoubt: true},
		{name: "the transaction never opens", tx: transaction{beginErr: failure}},
		{name: "a statement fails", tx: transaction{commitErr: failure}, fnErr: failure},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := record(context.Background(), &tc.tx, func(pgx.Tx) error { return tc.fnErr })
			assert.Error(t, err)
			assert.Equal(t, tc.inDoubt, errors.Is(err, artifact.ErrRecordInDoubt), "%v", err)
		})
	}

	t.Run("the commit succeeds", func(t *testing.T) {
		assert.NoError(t, record(context.Background(), &transaction{}, func(pgx.Tx) error { return nil }))
	})
}
