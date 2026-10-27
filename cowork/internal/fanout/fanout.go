// Package fanout delivers live messages to the connections that want
// them: a page's sync connections, a channel's readers. Each user of it
// names its own rooms.
package fanout

// Hub fans a room's live messages out to the connections in it: in one
// process for one instance, and across instances where several serve
// the same rooms (ADR-0127).
type Hub interface {
	// Join adds peer to room and answers the function that removes
	// it.
	Join(room string, peer Peer) (leave func())
	// Publish delivers msg to every peer in room but from.
	Publish(room string, from Peer, msg []byte)
}

// Peer is one connection a Hub delivers to. Deliver does not block: a
// peer too slow to take a message is dropped by its own connection.
type Peer interface {
	Deliver(msg []byte)
}
