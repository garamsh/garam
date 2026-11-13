package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// Store implements policyfeed.Store against PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// Compile-time check that Store satisfies policyfeed.Store.
var _ policyfeed.Store = (*Store)(nil)

// NewStore constructs a Postgres-backed delivery store.
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
SELECT entity_type, entity_id, generation FROM policy_outbox
ORDER BY entity_type, entity_id LIMIT $1`

const readState = `SELECT org, members, parent_types, parent_ids
	FROM policy_entity_state($1, $2)`

const nextSequence = `SELECT nextval('policy_feed_sequence')`

// dropMarks deletes each mark still at the generation the pass read.
const dropMarks = `
DELETE FROM policy_outbox o
USING unnest($1::text[], $2::text[], $3::bigint[]) AS d(t, i, g)
WHERE o.entity_type = d.t AND o.entity_id = d.i AND o.generation = d.g`

// Deliver runs one delivery pass.
func (s *Store) Deliver(
	ctx context.Context, limit int, sender policyfeed.Sender,
) (policyfeed.Result, error) {
	var result policyfeed.Result
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, errorx.Wrap(pgxerr.Classify(err, "begin pass", nil, nil), "deliver policy feed")
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	result, err = deliver(ctx, tx, limit, sender)
	if err != nil {
		return policyfeed.Result{}, errorx.Wrap(err, "deliver policy feed")
	}
	if err := tx.Commit(ctx); err != nil {
		return policyfeed.Result{}, errorx.Wrap(pgxerr.Classify(err, "commit pass", nil, nil), "deliver policy feed")
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
		return result, pgxerr.Classify(err, "hold feed", nil, nil)
	}
	if !enabled {
		return result, nil
	}

	marks, err := readMarkRows(ctx, tx, limit)
	if err != nil || len(marks) == 0 {
		return result, err
	}
	result.Marks = len(marks)

	entityMarks, linkMarks := splitMarks(marks)
	entries, err := readEntries(ctx, tx, entityMarks)
	if err != nil {
		return result, err
	}
	refused, err := send(ctx, tx, sender, entries)
	if err != nil {
		return result, err
	}
	result.Refused = refused

	// Links go after the entities of the pass, and no sequence orders
	// them: a link is written by name, and written again by it.
	links, err := readLinks(ctx, tx, linkMarks)
	if err != nil {
		return result, err
	}
	if len(links) > 0 {
		if err := sender.SendLinks(ctx, links); err != nil {
			return result, err
		}
	}

	types := make([]string, len(marks))
	ids := make([]string, len(marks))
	generations := make([]int64, len(marks))
	for i, m := range marks {
		types[i], ids[i], generations[i] = m.entity.Type, m.entity.ID, m.generation
	}
	if _, err := tx.Exec(ctx, dropMarks, types, ids, generations); err != nil {
		return result, pgxerr.Classify(err, "delete marks", nil, nil)
	}
	return result, nil
}

func readMarkRows(ctx context.Context, tx pgx.Tx, limit int) ([]mark, error) {
	rows, err := tx.Query(ctx, readMarks, limit)
	if err != nil {
		return nil, pgxerr.Classify(err, "read marks", nil, nil)
	}
	marks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (mark, error) {
		var m mark
		err := row.Scan(&m.entity.Type, &m.entity.ID, &m.generation)
		return m, err
	})
	return marks, pgxerr.Classify(err, "read marks", nil, nil)
}

// splitMarks separates the role links' marks from the entities'.
func splitMarks(marks []mark) (entities, links []mark) {
	for _, m := range marks {
		if m.entity.Type == policyfeed.RoleLinkType {
			links = append(links, m)
		} else {
			entities = append(entities, m)
		}
	}
	return entities, links
}

const readLinkState = `SELECT role FROM policy_role_link_state($1)`

// readLinks reads the role each marked membership holds, in the marks'
// order. No row is a membership the table no longer holds, and a link
// with no role.
func readLinks(ctx context.Context, tx pgx.Tx, marks []mark) ([]policyfeed.Link, error) {
	batch := &pgx.Batch{}
	links := make([]policyfeed.Link, len(marks))
	for i, m := range marks {
		org, user, ok := strings.Cut(m.entity.ID, " ")
		if !ok {
			return nil, errorx.New(errorx.KindInternal, "role link mark names no member")
		}
		links[i] = policyfeed.Link{Org: org, User: user}
		batch.Queue(readLinkState, m.entity.ID)
	}
	results := tx.SendBatch(ctx, batch)
	for i := range marks {
		switch err := results.QueryRow().Scan(&links[i].Role); {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			_ = results.Close()
			return nil, pgxerr.Classify(err, "read role link state", nil, nil)
		}
	}
	return links, pgxerr.Classify(results.Close(), "read role link state", nil, nil)
}

// readEntries reads each marked entity's state, in the marks' order.
func readEntries(ctx context.Context, tx pgx.Tx, marks []mark) ([]policyfeed.Entry, error) {
	batch := &pgx.Batch{}
	for _, m := range marks {
		batch.Queue(readState, m.entity.Type, m.entity.ID)
	}
	results := tx.SendBatch(ctx, batch)
	entries := make([]policyfeed.Entry, 0, len(marks))
	for _, m := range marks {
		state, err := scanState(results.QueryRow())
		if err != nil {
			_ = results.Close()
			return nil, pgxerr.Classify(err, "read entity state", nil, nil)
		}
		entries = append(entries, policyfeed.Entry{Entity: m.entity, State: state})
	}
	return entries, pgxerr.Classify(results.Close(), "read entity state", nil, nil)
}

// scanState reads policy_entity_state's row. No row is an entity the
// owners no longer hold, and a nil state.
func scanState(row pgx.Row) (*policyfeed.State, error) {
	var (
		org, members *string
		types, ids   []string
	)
	switch err := row.Scan(&org, &members, &types, &ids); {
	case errors.Is(err, pgx.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, err
	}
	state := &policyfeed.State{Parents: make([]policyfeed.Entity, len(types))}
	if org != nil {
		state.Org = &policyfeed.Entity{Type: "Iam::Org", ID: *org}
	}
	if members != nil {
		state.Members = &policyfeed.Entity{Type: "Iam::OrgMembers", ID: *members}
	}
	for i := range types {
		state.Parents[i] = policyfeed.Entity{Type: types[i], ID: ids[i]}
	}
	return state, nil
}

// send delivers entries under a fresh sequence. An entity the service
// refuses is sent again as a delete under a higher one (ADR-0140 §11),
// so a bad entity denies rather than decides on what it last said.
func send(
	ctx context.Context, tx pgx.Tx, sender policyfeed.Sender, entries []policyfeed.Entry,
) ([]policyfeed.Refusal, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	sequence, err := takeSequence(ctx, tx)
	if err != nil {
		return nil, err
	}
	refused, err := sender.Send(ctx, sequence, entries)
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
	again, err := sender.Send(ctx, sequence, deletes)
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
	return sequence, pgxerr.Classify(err, "take sequence", nil, nil)
}

const linkPending = `
SELECT EXISTS (
	SELECT 1 FROM policy_outbox
	WHERE entity_type = 'Iam::RoleLink'
	  AND starts_with(entity_id, $1::text || ' ')
	  AND ($2::text = '' OR entity_id = $1::text || ' ' || $2::text))`

// LinkPending reports whether a mark of a role link of org, or of the
// one of user where it is named, is still in the outbox.
func (s *Store) LinkPending(ctx context.Context, org, user string) (bool, error) {
	var pending bool
	err := s.pool.QueryRow(ctx, linkPending, org, user).Scan(&pending)
	return pending, errorx.Wrap(pgxerr.Classify(err, "read link marks", nil, nil), "await role link")
}

// Enabled reports whether the feed is on.
func (s *Switch) Enabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.pool.QueryRow(ctx, `SELECT enabled FROM policy_feed`).Scan(&enabled)
	return enabled, errorx.Wrap(pgxerr.Classify(err, "read feed", nil, nil), "read policy feed")
}
