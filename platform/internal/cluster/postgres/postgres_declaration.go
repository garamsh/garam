package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/cluster/postgres/db"
	"github.com/garamsh/garam/platform/internal/pgxerr"
	"github.com/garamsh/garam/platform/internal/pgxtype"
)

// DeclarationChannel is the LISTEN/NOTIFY channel a declaration write
// announces its record on, when its transaction commits; queries.sql's
// NotifyDeclarations names it too.
const DeclarationChannel = "garam_cluster_declarations"

// Compile-time check that Store keeps declarations.
var _ cluster.DeclarationStore = (*Store)(nil)

// Declare places object on g under id in one transaction holding g's
// row.
func (s *Store) Declare(ctx context.Context, g grn.GRN, id string, object cluster.DeclaredObject) (cluster.Declaration, bool, error) {
	var (
		out     cluster.Declaration
		created bool
	)
	err := s.inTx(ctx, g, cluster.ErrClusterNotFound, func(q *db.Queries, now time.Time) error {
		generation := int64(1)
		current, err := q.GetDeclarationForWrite(ctx, id)
		switch {
		case err == nil && current.ClusterGrn != g.String():
			return cluster.ErrDeclarationTaken
		case err == nil && current.Tombstoned:
			return cluster.ErrDeclarationTombstoned
		case err == nil:
			generation = current.Generation + 1
		case errors.Is(err, pgx.ErrNoRows):
			// An identifier no declaration holds was placed on a record
			// since deleted, or is new.
			reserved, err := q.ReserveDeclarationID(ctx, id)
			if err != nil {
				return pgxerr.Classify(err, "reserve declaration id", nil, nil)
			}
			if reserved == 0 {
				return cluster.ErrDeclarationTaken
			}
			created = true
		default:
			return pgxerr.Classify(err, "get declaration", nil, nil)
		}
		held, err := q.ClusterReportHolds(ctx, db.ClusterReportHoldsParams{ClusterGrn: g.String(),
			Kind: object.Kind(), Namespace: object.Namespace(), Name: object.Name()})
		if err != nil {
			return pgxerr.Classify(err, "read cluster report", nil, nil)
		}
		if held {
			return cluster.ErrDeclaredObjectHeld
		}
		row, err := s.write(ctx, q, g, db.WriteDeclarationParams{
			ID: id, ClusterGrn: g.String(), Generation: generation,
			Kind: object.Kind(), Namespace: object.Namespace(), Name: object.Name(),
			Object: object.JSON(), WrittenAt: pgxtype.Timestamptz(now),
		}, cluster.OpApply, now)
		if err != nil {
			return err
		}
		out, err = storedDeclaration(row)
		return err
	})
	return out, created, err
}

// Tombstone deletes g's live declaration id by a change at its next
// generation, in one transaction holding g's row.
func (s *Store) Tombstone(ctx context.Context, g grn.GRN, id string) (cluster.Declaration, error) {
	var out cluster.Declaration
	err := s.inTx(ctx, g, cluster.ErrDeclarationNotFound, func(q *db.Queries, now time.Time) error {
		current, err := q.GetDeclarationForWrite(ctx, id)
		if err != nil {
			return pgxerr.Classify(err, "get declaration", cluster.ErrDeclarationNotFound, nil)
		}
		if current.ClusterGrn != g.String() || current.Tombstoned {
			return cluster.ErrDeclarationNotFound
		}
		row, err := s.write(ctx, q, g, db.WriteDeclarationParams{
			ID: id, ClusterGrn: g.String(), Generation: current.Generation + 1,
			Kind: current.Kind, Namespace: current.Namespace, Name: current.Name,
			Tombstoned: true, WrittenAt: pgxtype.Timestamptz(now),
		}, cluster.OpDelete, now)
		if err != nil {
			return err
		}
		out, err = storedDeclaration(row)
		return err
	})
	return out, err
}

// write draws g's next position, stores the declaration at it, appends
// its change, purges what is past retention and announces g, all in
// q's transaction.
func (s *Store) write(ctx context.Context, q *db.Queries, g grn.GRN, d db.WriteDeclarationParams, op string, now time.Time) (db.ClusterDeclaration, error) {
	seq, err := q.AdvanceDeclarationHead(ctx, g.String())
	if err != nil {
		return db.ClusterDeclaration{}, pgxerr.Classify(err, "advance declaration head", nil, nil)
	}
	d.LastSeq = seq
	row, err := q.WriteDeclaration(ctx, d)
	if err != nil {
		return db.ClusterDeclaration{}, pgxerr.Classify(err, "write declaration", nil, cluster.ErrDeclaredObjectTaken)
	}
	if err := q.AppendDeclarationChange(ctx, db.AppendDeclarationChangeParams{
		ClusterGrn: g.String(), Seq: seq, Op: op, DeclarationID: d.ID,
		Generation: d.Generation, Object: d.Object, WrittenAt: d.WrittenAt,
	}); err != nil {
		return db.ClusterDeclaration{}, pgxerr.Classify(err, "append declaration change", nil, nil)
	}
	if err := q.PurgeDeclarationChanges(ctx, db.PurgeDeclarationChangesParams{
		ClusterGrn: g.String(), Before: pgxtype.Timestamptz(now.Add(-cluster.ChangeRetention)),
	}); err != nil {
		return db.ClusterDeclaration{}, pgxerr.Classify(err, "purge declaration changes", nil, nil)
	}
	if err := q.NotifyDeclarations(ctx, g.String()); err != nil {
		return db.ClusterDeclaration{}, pgxerr.Classify(err, "announce declarations", nil, nil)
	}
	return row, nil
}

// GetDeclaration reads g's live declaration id.
func (s *Store) GetDeclaration(ctx context.Context, g grn.GRN, id string) (cluster.Declaration, error) {
	row, err := s.q.GetLiveDeclaration(ctx, db.GetLiveDeclarationParams{ClusterGrn: g.String(), ID: id})
	if err != nil {
		return cluster.Declaration{}, pgxerr.Classify(err, "get declaration", cluster.ErrDeclarationNotFound, nil)
	}
	return storedDeclaration(row)
}

// ListDeclarations reads g's live declarations, or ErrClusterNotFound.
func (s *Store) ListDeclarations(ctx context.Context, g grn.GRN) ([]cluster.Declaration, error) {
	if _, err := s.q.GetCluster(ctx, g.String()); err != nil {
		return nil, pgxerr.Classify(err, "get cluster", cluster.ErrClusterNotFound, nil)
	}
	rows, err := s.q.ListLiveDeclarations(ctx, g.String())
	if err != nil {
		return nil, pgxerr.Classify(err, "list declarations", nil, nil)
	}
	out := make([]cluster.Declaration, 0, len(rows))
	for _, row := range rows {
		d, err := storedDeclaration(row)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// DeclarationPage reads g's position and the page after after in one
// repeatable-read snapshot, so the page and its cursor agree.
func (s *Store) DeclarationPage(ctx context.Context, g grn.GRN, after *string, size int) (cluster.Page, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return cluster.Page{}, errorx.Wrap(err, "begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	position, err := q.GetDeclarationPosition(ctx, g.String())
	if err != nil {
		return cluster.Page{}, pgxerr.Classify(err, "get declaration position", cluster.ErrClusterNotFound, nil)
	}
	at, err := cluster.ParseCursor(after, cluster.Position{Head: position.Head, PurgedThrough: position.PurgedThrough})
	if err != nil {
		return cluster.Page{}, err
	}
	// One row past the page tells whether more remain.
	maxRows := int32(size + 1)
	var page cluster.Page
	if at.Full {
		rows, err := q.ListLiveDeclarationsAfter(ctx, db.ListLiveDeclarationsAfterParams{
			ClusterGrn: g.String(), After: at.LastSent, MaxRows: maxRows})
		if err != nil {
			return cluster.Page{}, pgxerr.Classify(err, "list declarations", nil, nil)
		}
		live := make([]cluster.Entry, 0, len(rows))
		for _, r := range rows {
			live = append(live, cluster.Entry{Seq: r.LastSeq, Change: cluster.Change{
				Op: cluster.OpApply, ID: r.ID, Generation: r.Generation, Object: r.Object}})
		}
		page = cluster.FullPage(at, live, size)
	} else {
		rows, err := q.ListDeclarationChangesAfter(ctx, db.ListDeclarationChangesAfterParams{
			ClusterGrn: g.String(), After: at.LastSent, MaxRows: maxRows})
		if err != nil {
			return cluster.Page{}, pgxerr.Classify(err, "list declaration changes", nil, nil)
		}
		entries := make([]cluster.Entry, 0, len(rows))
		for _, r := range rows {
			entries = append(entries, cluster.Entry{Seq: r.Seq, Change: cluster.Change{
				Op: r.Op, ID: r.DeclarationID, Generation: r.Generation, Object: r.Object}})
		}
		page = cluster.LogPage(at, entries, size)
	}
	if err := tx.Commit(ctx); err != nil {
		return cluster.Page{}, errorx.Wrap(err, "commit", errorx.KindUnavailable)
	}
	return page, nil
}

// storedDeclaration parses a declaration row. Its object was admitted
// when written, so one that no longer parses is the database's fault.
func storedDeclaration(row db.ClusterDeclaration) (cluster.Declaration, error) {
	g, err := storedGRN(row.ClusterGrn)
	if err != nil {
		return cluster.Declaration{}, err
	}
	d := cluster.Declaration{ID: row.ID, Cluster: g, Generation: row.Generation, Tombstoned: row.Tombstoned,
		CreatedAt: row.CreatedAt.Time.UTC(), UpdatedAt: row.UpdatedAt.Time.UTC()}
	if !row.Tombstoned {
		d.Object, err = cluster.ParseDeclaredObject(row.Object)
		if err != nil {
			return cluster.Declaration{}, errorx.Wrap(err, "stored declaration "+row.ID, errorx.KindInternal)
		}
	}
	return d, nil
}

// ListenDeclarations wakes wakes' polls on each record a declaration
// write announces, from any process, until ctx is done. It holds one
// connection, and answers every failure by reconnecting after a delay
// doubling up to relayReconnectMax; a write announced while none was
// open is read at the poll's next recheck.
func ListenDeclarations(ctx context.Context, s *Store, wakes *cluster.Wakes, warn func(error, time.Duration)) {
	delay := relayReconnectMin
	for {
		connected, err := s.listen(ctx, wakes)
		if ctx.Err() != nil {
			return
		}
		if connected {
			delay = relayReconnectMin
		}
		warn(err, delay)
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, relayReconnectMax)
	}
}

// The listener's reconnect delays, as the watch relay's.
const (
	relayReconnectMin = 100 * time.Millisecond
	relayReconnectMax = 30 * time.Second
)

func (s *Store) listen(ctx context.Context, wakes *cluster.Wakes) (bool, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return false, errorx.Wrap(err, "declarations listener: acquire", errorx.KindUnavailable)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+DeclarationChannel); err != nil {
		return false, errorx.Wrap(err, "declarations listener: listen", errorx.KindUnavailable)
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return true, errorx.Wrap(err, "declarations listener: wait", errorx.KindUnavailable)
		}
		g, err := storedGRN(n.Payload)
		if err != nil {
			return true, err
		}
		wakes.Wake(g)
	}
}
