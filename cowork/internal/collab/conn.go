package collab

import "context"

// Conn is one client's connection, a message at a time: a WebSocket's
// binary frames.
type Conn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, msg []byte) error
}
