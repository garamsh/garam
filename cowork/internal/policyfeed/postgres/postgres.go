// Package postgres implements the policy feed's store and switch
// against cowork's database. The outbox, the triggers that fill it and
// policy_entity_state are migration 000006's.
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/cowork/internal/policyfeed"
	"github.com/garamsh/garam/lib/errorx"
)

// fail wraps a database error as the service being unavailable.
func fail(err error, op string) error {
	if err == nil {
		return nil
	}
	if errorx.KindOf(err) != errorx.KindUnknown {
		return errorx.Wrap(err, "policy feed: "+op)
	}
	return errorx.Wrap(err, "policy feed: "+op, errorx.KindUnavailable)
}

// Store implements policyfeed.Store.
type Store struct {
	pool *pgxpool.Pool
}

var _ policyfeed.Store = (*Store)(nil)

// NewStore constructs a store over pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// mark is one outbox row as a pass read it.
type mark struct {
	entity     policyfeed.Entity
	generation int64
}

// holdFeed locks the feed's row, and skips it when another replica
// holds the lock, so one replica delivers at a time.
const holdFeed = `SELECT enabled FROM policy_feed FOR UPDATE SKIP LOCKED`

const readMarks = `
SELECT org, entity_type, entity_id, generation FROM policy_outbox
ORDER BY org, entity_type, entity_id LIMIT $1`

const readState = `SELECT org FROM policy_entity_state($1, $2, $3)`

const nextSequence = `SELECT nextval('policy_feed_sequence')`

// dropMarks deletes each mark still at the generation the pass read.
const dropMarks = `
DELETE FROM policy_outbox o
USING unnest($1::text[], $2::text[], $3::text[], $4::bigint[]) AS d(o, t, i, g)
WHERE o.org = d.o AND o.entity_type = d.t AND o.entity_id = d.i AND o.generation = d.g`

// Deliver runs one delivery pass.
func (s *Store) Deliver(
	ctx context.Context, limit int, sender policyfeed.Sender,
) (policyfeed.Result, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return policyfeed.Result{}, fail(err, "begin pass")
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	result, err := deliver(ctx, tx, limit, sender)
	if err != nil {
		return policyfeed.Result{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return policyfeed.Result{}, fail(err, "commit pass")
	}
	return result, nil
}

func deliver(
	ctx context.Context, tx pgx.Tx, limit int, sender policyfeed.Sender,
) (policyfeed.Result, error) {
	var result policyfeed.Result
	var enabled bool
	switch err := tx.QueryRow(ctx, holdFeed).Scan(&enabled); {
	case errors.Is(err, pgx.ErrNoRows):
		return result, nil
	case err != nil:
		return result, fail(err, "hold feed")
	}
	if !enabled {
		return result, nil
	}

	marks, err := readMarkRows(ctx, tx, limit)
	if err != nil || len(marks) == 0 {
		return result, err
	}
	result.Marks = len(marks)

	entries, err := readEntries(ctx, tx, marks)
	if err != nil {
		return result, err
	}
	// The marks come ordered by organization, so each organization's
	// entries are one run, sent under that organization's own token.
	for start := 0; start < len(entries); {
		end := start + 1
		for end < len(entries) && entries[end].Entity.Org == entries[start].Entity.Org {
			end++
		}
		refused, err := send(ctx, tx, sender, entries[start].Entity.Org, entries[start:end])
		if err != nil {
			return result, err
		}
		result.Refused = append(result.Refused, refused...)
		start = end
	}

	orgs := make([]string, len(marks))
	types := make([]string, len(marks))
	ids := make([]string, len(marks))
	generations := make([]int64, len(marks))
	for i, m := range marks {
		orgs[i], types[i], ids[i], generations[i] = m.entity.Org, m.entity.Type, m.entity.ID, m.generation
	}
	if _, err := tx.Exec(ctx, dropMarks, orgs, types, ids, generations); err != nil {
		return result, fail(err, "delete marks")
	}
	return result, nil
}

func readMarkRows(ctx context.Context, tx pgx.Tx, limit int) ([]mark, error) {
	rows, err := tx.Query(ctx, readMarks, limit)
	if err != nil {
		return nil, fail(err, "read marks")
	}
	marks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (mark, error) {
		var m mark
		err := row.Scan(&m.entity.Org, &m.entity.Type, &m.entity.ID, &m.generation)
		return m, err
	})
	return marks, fail(err, "read marks")
}

// readEntries reads each marked entity's state, in the marks' order.
func readEntries(ctx context.Context, tx pgx.Tx, marks []mark) ([]policyfeed.Entry, error) {
	batch := &pgx.Batch{}
	for _, m := range marks {
		batch.Queue(readState, m.entity.Type, m.entity.Org, m.entity.ID)
	}
	results := tx.SendBatch(ctx, batch)
	entries := make([]policyfeed.Entry, 0, len(marks))
	for _, m := range marks {
		var org string
		exists := true
		switch err := results.QueryRow().Scan(&org); {
		case errors.Is(err, pgx.ErrNoRows):
			exists = false
		case err != nil:
			_ = results.Close()
			return nil, fail(err, "read entity state")
		}
		entries = append(entries, policyfeed.Entry{Entity: m.entity, Exists: exists})
	}
	return entries, fail(results.Close(), "read entity state")
}

// send delivers one organization's entries under a fresh sequence. An
// entity the service refuses is sent again as a delete under a higher
// one (ADR-0140 item 11), so a bad entity denies rather than decides on
// what it last said.
func send(
	ctx context.Context, tx pgx.Tx, sender policyfeed.Sender, org string, entries []policyfeed.Entry,
) ([]policyfeed.Refusal, error) {
	sequence, err := takeSequence(ctx, tx)
	if err != nil {
		return nil, err
	}
	refused, err := sender.Send(ctx, org, sequence, entries)
	if err != nil || len(refused) == 0 {
		return nil, err
	}
	deletes := make([]policyfeed.Entry, len(refused))
	for i, r := range refused {
		deletes[i] = policyfeed.Entry{Entity: r.Entity}
	}
	if sequence, err = takeSequence(ctx, tx); err != nil {
		return nil, err
	}
	again, err := sender.Send(ctx, org, sequence, deletes)
	if err != nil {
		return nil, err
	}
	if len(again) > 0 {
		return nil, errorx.New(errorx.KindInternal, "policy service refused an entity's delete")
	}
	return refused, nil
}

func takeSequence(ctx context.Context, tx pgx.Tx) (int64, error) {
	var sequence int64
	err := tx.QueryRow(ctx, nextSequence).Scan(&sequence)
	return sequence, fail(err, "take sequence")
}

// Switch implements policyfeed.Switch.
type Switch struct {
	pool *pgxpool.Pool
}

var _ policyfeed.Switch = (*Switch)(nil)

// NewSwitch constructs a switch over pool.
func NewSwitch(pool *pgxpool.Pool) *Switch {
	return &Switch{pool: pool}
}

// lockSources waits out every writer of the four tables the triggers
// watch, and holds off new ones until the transaction ends, so no write
// falls between the backfill and the first trigger that sees the feed
// on. SHARE blocks writers and lets readers through.
const lockSources = `LOCK TABLE channels, pages, tasks, artifacts IN SHARE MODE`

const backfill = `
INSERT INTO policy_outbox (entity_type, org, entity_id)
SELECT 'channel', org, id FROM channels
UNION ALL SELECT 'page', org, id FROM pages
UNION ALL SELECT 'task', org, id FROM tasks
UNION ALL SELECT 'artifact', org, id FROM artifacts
ON CONFLICT (org, entity_type, entity_id)
DO UPDATE SET generation = policy_outbox.generation + 1`

// Start enables the feed and marks every live entity.
func (s *Switch) Start(ctx context.Context) (int64, error) {
	var marked int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockSources); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE policy_feed SET enabled = true`); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, backfill); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT count(*) FROM policy_outbox`).Scan(&marked)
	})
	return marked, fail(err, "start")
}

// Stop disables the feed and empties the outbox.
func (s *Switch) Stop(ctx context.Context) (int64, error) {
	var removed int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockSources); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE policy_feed SET enabled = false`); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM policy_outbox`)
		removed = tag.RowsAffected()
		return err
	})
	return removed, fail(err, "stop")
}

// Enabled reports whether the feed is on.
func (s *Switch) Enabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.pool.QueryRow(ctx, `SELECT enabled FROM policy_feed`).Scan(&enabled)
	return enabled, fail(err, "read feed")
}
