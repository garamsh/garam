package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// raisedMarks reads the marks the open transaction wrote: a mark's row
// is written by the transaction whose id it carries as xmin, and a
// write that raised a mark already there writes the row again. The
// transaction id is the top-level one: a write made under a savepoint
// or an EXCEPTION block of a trigger carries a subtransaction id
// instead, and this read would miss it with no error, so a delete or a
// trigger that gains one must be read again against this predicate
// (ADR-0165 §Signs the decision was wrong).
const raisedMarks = `
SELECT entity_type, entity_id FROM policy_outbox
WHERE xmin = pg_current_xact_id()::xid`

// Record adds to the collection ctx carries the marks tx has raised so
// far, read in tx itself. It reads nothing where ctx carries none, so
// a store calls it before commit on every path at no cost to a caller
// that did not ask (ADR-0165 item 3).
func Record(ctx context.Context, tx pgx.Tx) error {
	raised := policyfeed.RaisedIn(ctx)
	if raised == nil {
		return nil
	}
	rows, err := tx.Query(ctx, raisedMarks)
	if err != nil {
		return errorx.Wrap(pgxerr.Classify(err, "read raised marks", nil, nil), "record policy marks")
	}
	defer rows.Close()
	var found []policyfeed.Entity
	for rows.Next() {
		var e policyfeed.Entity
		if err := rows.Scan(&e.Type, &e.ID); err != nil {
			return errorx.Wrap(pgxerr.Classify(err, "scan raised mark", nil, nil), "record policy marks")
		}
		found = append(found, e)
	}
	if err := rows.Err(); err != nil {
		return errorx.Wrap(pgxerr.Classify(err, "read raised marks", nil, nil), "record policy marks")
	}
	raised.Add(found...)
	return nil
}
