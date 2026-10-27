// Package memory implements fanout.Hub inside one process: every
// connection to a room reaches every other through this instance. A
// deployment of several instances needs the postgres hub instead.
package memory

import (
	"sync"

	"github.com/garamsh/garam/cowork/internal/fanout"
)

// Hub holds each room's peers behind one lock.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]map[fanout.Peer]struct{}
}

var _ fanout.Hub = (*Hub)(nil)

// New answers a hub with no rooms.
func New() *Hub {
	return &Hub{rooms: map[string]map[fanout.Peer]struct{}{}}
}

// Join implements fanout.Hub.
func (h *Hub) Join(room string, peer fanout.Peer) func() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[room] == nil {
		h.rooms[room] = map[fanout.Peer]struct{}{}
	}
	h.rooms[room][peer] = struct{}{}
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.rooms[room], peer)
		if len(h.rooms[room]) == 0 {
			delete(h.rooms, room)
		}
	}
}

// Publish implements fanout.Hub. Deliver does not block, so the lock
// is held across the room.
func (h *Hub) Publish(room string, from fanout.Peer, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for p := range h.rooms[room] {
		if p != from {
			p.Deliver(msg)
		}
	}
}
