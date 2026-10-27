// Package memory implements collab.Hub inside one process: every
// connection to a page reaches every other through this instance. A
// deployment of several instances needs the postgres hub instead.
package memory

import (
	"sync"

	"github.com/garamsh/garam/cowork/internal/collab"
)

// Hub holds each room's peers behind one lock.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]map[collab.Peer]struct{}
}

var _ collab.Hub = (*Hub)(nil)

// New answers a hub with no rooms.
func New() *Hub {
	return &Hub{rooms: map[string]map[collab.Peer]struct{}{}}
}

// Join implements collab.Hub.
func (h *Hub) Join(room string, peer collab.Peer) func() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[room] == nil {
		h.rooms[room] = map[collab.Peer]struct{}{}
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

// Publish implements collab.Hub. Deliver does not block, so the lock
// is held across the room.
func (h *Hub) Publish(room string, from collab.Peer, msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for p := range h.rooms[room] {
		if p != from {
			p.Deliver(msg)
		}
	}
}
