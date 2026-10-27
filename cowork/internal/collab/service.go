package collab

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/garamsh/garam/cowork/internal/fanout"
	"github.com/garamsh/garam/platform/sdk/errorx"
)

// compactAfter is how many updates may follow a page's snapshot
// before the next update folds them in.
const compactAfter = 200

// outboundBuffer is how many messages a connection may fall behind by
// before it is dropped. A dropped client reconnects and syncs again,
// which loses nothing.
const outboundBuffer = 256

// Room is the page a connection edits, and whether its edits are
// accepted. The caller has already been allowed to read it.
type Room struct {
	Org      string
	Page     string
	Writable bool
}

// key names the page across organizations, as the hub's room.
func (r Room) key() string { return "page/" + r.Org + "/" + r.Page }

// ErrTooSlow ends a connection that fell outboundBuffer messages
// behind.
var ErrTooSlow = errorx.New(errorx.KindResourceExhausted, "collab: the connection fell too far behind")

// Service is real-time editing of one page per connection.
type Service interface {
	// Serve speaks the sync protocol on conn until the client leaves,
	// ctx ends, or the connection fails, and answers why it ended.
	Serve(ctx context.Context, room Room, conn Conn) error
}

type service struct {
	log    UpdateLog
	hub    fanout.Hub
	engine DocEngine
}

// NewService answers the relay over log, hub and engine.
func NewService(log UpdateLog, hub fanout.Hub, engine DocEngine) Service {
	return &service{log: log, hub: hub, engine: engine}
}

// peer is a connection's outbound side. Deliver queues without
// blocking; a full queue marks the peer too slow.
type peer struct {
	out  chan []byte
	once sync.Once
	slow chan struct{}
}

func (p *peer) Deliver(msg []byte) {
	select {
	case p.out <- msg:
	default:
		p.once.Do(func() { close(p.slow) })
	}
}

// state answers the page's whole document as one update.
func (s *service) state(ctx context.Context, room Room) ([]byte, error) {
	doc, err := s.log.Load(ctx, room.Org, room.Page)
	if err != nil {
		return nil, err
	}
	all := doc.Updates
	if len(doc.Snapshot) > 0 {
		all = append([][]byte{doc.Snapshot}, all...)
	}
	if len(all) == 0 {
		// An empty Yjs update: no structs, an empty delete set.
		return []byte{0, 0}, nil
	}
	return s.engine.Merge(all)
}

// Serve implements Service.
func (s *service) Serve(ctx context.Context, room Room, conn Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := &peer{out: make(chan []byte, outboundBuffer), slow: make(chan struct{})}
	leave := s.hub.Join(room.key(), p)
	defer leave()

	writeErr := make(chan error, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				writeErr <- nil
				return
			case <-p.slow:
				writeErr <- ErrTooSlow
				return
			case msg := <-p.out:
				if err := conn.Write(ctx, msg); err != nil {
					writeErr <- err
					return
				}
			}
		}
	}()

	state, err := s.state(ctx, room)
	if err != nil {
		return err
	}
	sv, err := s.engine.StateVector(state)
	if err != nil {
		return err
	}
	p.Deliver(Message{Kind: MessageKindSync, Sync: SyncKindStep1, Payload: sv}.Encode())

	readErr := make(chan error, 1)
	go func() { readErr <- s.read(ctx, room, conn, p) }()
	select {
	case err := <-readErr:
		return err
	case err := <-writeErr:
		return err
	}
}

// read handles the client's messages until one fails.
func (s *service) read(ctx context.Context, room Room, conn Conn, p *peer) error {
	refused := false
	for {
		raw, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		m, err := Decode(raw)
		if err != nil {
			return err
		}
		switch {
		case m.Kind == MessageKindSync && m.Sync == SyncKindStep1:
			state, err := s.state(ctx, room)
			if err != nil {
				return err
			}
			missing, err := s.engine.Diff(state, m.Payload)
			if err != nil {
				return err
			}
			p.Deliver(Message{Kind: MessageKindSync, Sync: SyncKindStep2, Payload: missing}.Encode())
		case m.Kind == MessageKindSync:
			if !room.Writable {
				if !refused {
					p.Deliver(permissionDenied("this page is read-only for you"))
					refused = true
				}
				continue
			}
			if err := s.apply(ctx, room, p, m.Payload); err != nil {
				return err
			}
		case m.Kind == MessageKindAwareness:
			s.hub.Publish(room.key(), p, raw)
		}
	}
}

// apply keeps one update and sends it on to the page's other
// connections. An update that does not decode is refused before it is
// kept, since every later reader would fail on it.
func (s *service) apply(ctx context.Context, room Room, p *peer, update []byte) error {
	if _, err := s.engine.StateVector(update); err != nil {
		return err
	}
	n, err := s.log.Append(ctx, room.Org, room.Page, update)
	if err != nil {
		return err
	}
	s.hub.Publish(room.key(), p, Message{Kind: MessageKindSync, Sync: SyncKindUpdate, Payload: update}.Encode())
	if n >= compactAfter {
		if err := s.compact(ctx, room); err != nil && !errors.Is(err, context.Canceled) {
			// The update is kept either way; a compaction that failed
			// is retried by the next update.
			slog.WarnContext(ctx, "collab: compaction failed", slog.String("page", room.Page), slog.String("error", err.Error()))
		}
	}
	return nil
}

func (s *service) compact(ctx context.Context, room Room) error {
	doc, err := s.log.Load(ctx, room.Org, room.Page)
	if err != nil {
		return err
	}
	all := doc.Updates
	if len(doc.Snapshot) > 0 {
		all = append([][]byte{doc.Snapshot}, all...)
	}
	merged, err := s.engine.Merge(all)
	if err != nil {
		return err
	}
	return s.log.Compact(ctx, room.Org, room.Page, merged, doc.Through)
}
