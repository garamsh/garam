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
WHERE set_aside_at IS NULL
ORDER BY entity_type, entity_id LIMIT $1`

const readState = `SELECT org, members, parent_types, parent_ids, consent_ids
	FROM policy_entity_state($1, $2)`

const nextSequence = `SELECT nextval('policy_feed_sequence')`

// dropMarks deletes each mark still at the generation the pass read.
const dropMarks = `
DELETE FROM policy_outbox o
USING unnest($1::text[], $2::text[], $3::bigint[]) AS d(t, i, g)
WHERE o.entity_type = d.t AND o.entity_id = d.i AND o.generation = d.g`

// setAsideMarks records the refusal of each link's mark still at the
// generation the pass read. A mark that was written again meanwhile is
// pending again, and the pass that reads it sends the link anew.
const setAsideMarks = `
UPDATE policy_outbox o
SET set_aside_code = d.c, set_aside_reason = d.r, set_aside_at = now()
FROM unnest($1::text[], $2::text[], $3::bigint[], $4::text[], $5::text[])
	AS d(t, i, g, c, r)
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
		if result.SetAside, err = sender.SendLinks(ctx, links); err != nil {
			return result, err
		}
	}
	return result, settleMarks(ctx, tx, marks, result.SetAside)
}

// settleMarks deletes the marks the pass delivered, and records the
// refusal on the mark of each link it set aside.
func settleMarks(
	ctx context.Context, tx pgx.Tx, marks []mark, setAside []policyfeed.LinkRefusal,
) error {
	refusals := make(map[string]policyfeed.LinkRefusal, len(setAside))
	for _, r := range setAside {
		refusals[r.Link.Org+" "+r.Link.User] = r
	}
	var drop, keep markColumns
	for _, m := range marks {
		r, refused := refusals[m.entity.ID]
		if refused && m.entity.Type == policyfeed.RoleLinkType {
			keep.add(m, r.Code, r.Reason)
		} else {
			drop.add(m, "", "")
		}
	}
	if len(keep.ids) > 0 {
		_, err := tx.Exec(ctx, setAsideMarks,
			keep.types, keep.ids, keep.generations, keep.codes, keep.reasons)
		if err != nil {
			return pgxerr.Classify(err, "set aside marks", nil, nil)
		}
	}
	if _, err := tx.Exec(ctx, dropMarks, drop.types, drop.ids, drop.generations); err != nil {
		return pgxerr.Classify(err, "delete marks", nil, nil)
	}
	return nil
}

// markColumns is marks as the columns a statement unnests.
type markColumns struct {
	types, ids, codes, reasons []string
	generations                []int64
}

func (c *markColumns) add(m mark, code, reason string) {
	c.types = append(c.types, m.entity.Type)
	c.ids = append(c.ids, m.entity.ID)
	c.generations = append(c.generations, m.generation)
	c.codes = append(c.codes, code)
	c.reasons = append(c.reasons, reason)
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
		consents     []string
	)
	switch err := row.Scan(&org, &members, &types, &ids, &consents); {
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
	for _, id := range consents {
		state.Consents = append(state.Consents, policyfeed.Entity{Type: "Iam::Org", ID: id})
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

const linkState = `
SELECT count(*) FILTER (WHERE set_aside_at IS NULL),
       count(*) FILTER (WHERE set_aside_at IS NOT NULL)
FROM policy_outbox
WHERE entity_type = 'Iam::RoleLink'
  AND starts_with(entity_id, $1::text || ' ')
  AND ($2::text = '' OR entity_id = $1::text || ' ' || $2::text)`

// LinkState reports where the marks of a role link of org, or of the
// one of user where it is named, stand in the outbox.
func (s *Store) LinkState(ctx context.Context, org, user string) (policyfeed.LinkState, error) {
	var pending, setAside int64
	err := s.pool.QueryRow(ctx, linkState, org, user).Scan(&pending, &setAside)
	if err != nil {
		return policyfeed.LinkApplied,
			errorx.Wrap(pgxerr.Classify(err, "read link marks", nil, nil), "await role link")
	}
	switch {
	case setAside > 0:
		return policyfeed.LinkSetAside, nil
	case pending > 0:
		return policyfeed.LinkPending, nil
	}
	return policyfeed.LinkApplied, nil
}

const entityPending = `
SELECT EXISTS (SELECT 1 FROM policy_outbox
	WHERE entity_type = $1 AND entity_id = $2)`

// EntityPending reports whether the outbox holds a mark of the entity.
func (s *Store) EntityPending(ctx context.Context, entity policyfeed.Entity) (bool, error) {
	var pending bool
	err := s.pool.QueryRow(ctx, entityPending, entity.Type, entity.ID).Scan(&pending)
	if err != nil {
		return false,
			errorx.Wrap(pgxerr.Classify(err, "read entity mark", nil, nil), "await entity")
	}
	return pending, nil
}

const readStanding = `
SELECT count(*) FILTER (WHERE set_aside_at IS NULL),
       count(*) FILTER (WHERE set_aside_at IS NOT NULL)
FROM policy_outbox`

// Standing counts the outbox's marks by whether a pass reads them.
func (s *Store) Standing(ctx context.Context) (policyfeed.Standing, error) {
	var standing policyfeed.Standing
	err := s.pool.QueryRow(ctx, readStanding).Scan(&standing.Pending, &standing.SetAside)
	return standing, errorx.Wrap(pgxerr.Classify(err, "read outbox", nil, nil), "read policy feed standing")
}

// Enabled reports whether the feed is on.
func (s *Switch) Enabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.pool.QueryRow(ctx, `SELECT enabled FROM policy_feed`).Scan(&enabled)
	return enabled, errorx.Wrap(pgxerr.Classify(err, "read feed", nil, nil), "read policy feed")
}
