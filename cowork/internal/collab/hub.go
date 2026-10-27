package collab

// Hub fans a page's live messages out to the connections editing it:
// in one process for one instance, and across instances where several
// serve the same pages (ADR-0127).
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
