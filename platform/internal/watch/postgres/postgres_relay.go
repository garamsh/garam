package postgres

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/pgxtype"
	"github.com/garamsh/garam/platform/internal/watch"
	"github.com/garamsh/garam/platform/internal/watch/postgres/db"
)

// TurnEventChannel is the Postgres LISTEN/NOTIFY channel forwarded turn
// events travel on.
const TurnEventChannel = "garam_turn_event"

// maxAnnouncementBytes is what one announcement carries. Postgres
// refuses a notification payload of 8000 bytes or more, and the margin
// covers the difference between a body's characters and the bytes its
// JSON encoding takes.
const maxAnnouncementBytes = 7500

// How long the relay waits before reopening a connection: the first
// attempt after a failure waits relayReconnectMin, doubling up to
// relayReconnectMax while attempts keep failing, and returning to the
// minimum once one connects. The minimum is short because every open
// watch is silent until it does, and the maximum is what keeps a
// database that is down from being retried into an outage of its own.
const (
	relayReconnectMin = 100 * time.Millisecond
	relayReconnectMax = 30 * time.Second
)

// Relay carries events between garam's processes over
// LISTEN/NOTIFY: no row, no acknowledgement, and nothing kept for a
// process that is not listening when one arrives — which is what makes
// it the shape ADR-0022 asks for rather than a log with a short memory.
type Relay struct {
	pool *pgxpool.Pool
}

// Compile-time check that Relay satisfies watch.Relay.
var _ watch.Relay = (*Relay)(nil)

// NewRelay constructs a Postgres-backed watch.Relay.
func NewRelay(pool *pgxpool.Pool) *Relay {
	return &Relay{pool: pool}
}

// announcement is one event on the wire, with the agent it belongs to.
type announcement struct {
	Agent string `json:"agent"`
	Kind  string `json:"kind"`
	Body  string `json:"body"`
}

// Announce publishes the event to every listening process, or refuses
// one larger than a notification carries. It is refused rather than
// truncated: a shortened turn would put words in the agent's mouth,
// where a refused event is a gap, and a gap is what this stream is
// already permitted (ADR-0022).
func (r *Relay) Announce(ctx context.Context, agent grn.GRN, e watch.Event, fence *watch.Fence) error {
	payload, err := json.Marshal(announcement{Agent: agent.String(), Kind: e.Kind(), Body: e.Body()})
	if err != nil {
		return errorx.Wrap(err, "watch relay: encode", errorx.KindInvalidArgument)
	}
	if len(payload) > maxAnnouncementBytes {
		return errorx.New(errorx.KindInvalidArgument, "watch relay: event is larger than one announcement carries")
	}
	if fence == nil {
		if _, err := r.pool.Exec(ctx, "SELECT pg_notify($1, $2)", TurnEventChannel, string(payload)); err != nil {
			return errorx.Wrap(err, "watch relay: announce", errorx.KindUnavailable)
		}
		return nil
	}
	// A notification is sent when its transaction commits, so the one
	// the fence admits commits before a replacement does or not at all.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return errorx.Wrap(err, "watch relay: begin", errorx.KindUnavailable)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	answer, err := db.New(tx).AdmitExecution(ctx, db.AdmitExecutionParams{
		AgentGrn:  fence.Agent.String(),
		TokenHash: fence.TokenHash,
		Serial:    fence.Serial,
		NotBefore: pgxtype.Timestamptz(fence.NotBefore),
		NotAfter:  pgxtype.Timestamptz(fence.NotAfter),
	})
	if err != nil {
		return errorx.Wrap(err, "watch relay: admit", errorx.KindUnavailable)
	}
	if err := refusalOf(answer); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT pg_notify($1, $2)", TurnEventChannel, string(payload)); err != nil {
		return errorx.Wrap(err, "watch relay: announce", errorx.KindUnavailable)
	}
	if err := tx.Commit(ctx); err != nil {
		return errorx.Wrap(err, "watch relay: commit", errorx.KindUnavailable)
	}
	return nil
}

// Receive delivers announced events until ctx is done. It holds one
// connection for the process rather than one per watcher, and every
// failure — a Postgres failover breaking it, a payload nobody can parse
// — is answered by reconnecting, so only ctx ends this. Reconnecting
// carries no resynchronisation: an event announced while no connection
// was open is gone, which ADR-0022 already accepts.
func (r *Relay) Receive(ctx context.Context, deliver func(agent grn.GRN, e watch.Event)) error {
	delay := relayReconnectMin
	for {
		connected, err := r.receive(ctx, deliver)
		if ctx.Err() != nil {
			return errorx.Wrap(err, "watch relay: stopped", errorx.KindUnavailable)
		}
		if connected {
			delay = relayReconnectMin
		}
		slog.WarnContext(ctx, "watch relay reconnecting",
			slog.String("error", err.Error()),
			slog.Duration("in", delay))
		select {
		case <-ctx.Done():
			return errorx.Wrap(ctx.Err(), "watch relay: stopped", errorx.KindUnavailable)
		case <-time.After(delay):
		}
		delay = min(delay*2, relayReconnectMax)
	}
}

// receive holds one connection for as long as it lasts, and reports
// whether it was established — which is what tells a database that is
// down from one that dropped a live connection.
//
// garam is the only writer on the channel and it writes what Announce
// encoded, so a payload that does not parse is a defect in this package
// rather than input. It is still answered with a reconnect: the answer
// to every failure is the same one, here as in the policy listener.
func (r *Relay) receive(ctx context.Context, deliver func(agent grn.GRN, e watch.Event)) (bool, error) {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return false, errorx.Wrap(err, "watch relay: acquire", errorx.KindUnavailable)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+TurnEventChannel); err != nil {
		return false, errorx.Wrap(err, "watch relay: listen", errorx.KindUnavailable)
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return true, errorx.Wrap(err, "watch relay: wait", errorx.KindUnavailable)
		}
		var a announcement
		if err := json.Unmarshal([]byte(n.Payload), &a); err != nil {
			return true, errorx.Wrap(err, "watch relay: payload", errorx.KindInternal)
		}
		agent, err := grn.Parse(a.Agent)
		if err != nil {
			return true, errorx.Wrap(err, "watch relay: agent", errorx.KindInternal)
		}
		e, err := watch.NewEvent(a.Kind, a.Body)
		if err != nil {
			return true, errorx.Wrap(err, "watch relay: event", errorx.KindInternal)
		}
		deliver(agent, e)
	}
}

// refusalOf reads the answer of agent_execution_admit, the agent
// execution fence's declared surface (ADR-0080): nil for an admitted
// write, and the refusal it names otherwise.
func refusalOf(answer string) error {
	switch answer {
	case "admitted":
		return nil
	case "unknown_agent":
		return errorx.New(errorx.KindNotFound, "agent not found")
	case "token_required":
		return errorx.New(errorx.KindUnauthenticated, "execution token required")
	case "token_stale":
		return errorx.New(errorx.KindPermissionDenied, "execution token is not current")
	case "credential_fenced":
		return errorx.New(errorx.KindFailedPrecondition, "credential is not current")
	}
	return errorx.New(errorx.KindInternal, "unknown admission answer")
}
