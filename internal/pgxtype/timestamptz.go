// Package pgxtype converts between Go values and the pgtype values the
// postgres-backed stores bind and scan. It holds a wire format the
// stores already share and chooses no behaviour, which is what places
// it below them (docs/architecture/structure.md §When a new boundary
// is earned).
package pgxtype

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// Timestamptz returns t as a present timestamptz.
func Timestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// Time returns the time t holds as stored, or nil where t is NULL.
func Time(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	at := t.Time
	return &at
}
