package pgxtype_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/pgxtype"
)

func TestTimestamptz(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 30, 0, 0, time.FixedZone("KST", 9*60*60))
	assert.Equal(t, pgtype.Timestamptz{Time: at, Valid: true}, pgxtype.Timestamptz(at))
}

// TestTime reads a stored time back as stored, its zone included, and
// a NULL as nil rather than as the zero time.
func TestTime(t *testing.T) {
	at := time.Date(2026, 10, 16, 9, 30, 0, 0, time.FixedZone("KST", 9*60*60))
	got := pgxtype.Time(pgtype.Timestamptz{Time: at, Valid: true})
	require.NotNil(t, got)
	assert.Equal(t, at, *got)
	assert.Equal(t, "KST", got.Location().String())

	assert.Nil(t, pgxtype.Time(pgtype.Timestamptz{Time: at}))
}
