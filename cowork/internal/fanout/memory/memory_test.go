package memory_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/cowork/internal/fanout/memory"
)

type inbox struct{ got [][]byte }

func (i *inbox) Deliver(msg []byte) { i.got = append(i.got, msg) }

// TestPublish_ReachesTheOtherPeersInTheRoom publishes from one of two
// peers on a page: the other receives it, the sender does not, and a
// peer on another page, the control for the room, receives nothing.
// After leaving, the peer receives nothing more.
func TestPublish_ReachesTheOtherPeersInTheRoom(t *testing.T) {
	h := memory.New()
	a, b, other := &inbox{}, &inbox{}, &inbox{}
	h.Join("acme/p1", a)
	leaveB := h.Join("acme/p1", b)
	h.Join("acme/p2", other)

	h.Publish("acme/p1", a, []byte("hi"))
	assert.Equal(t, [][]byte{[]byte("hi")}, b.got)
	assert.Empty(t, a.got)
	assert.Empty(t, other.got)

	leaveB()
	h.Publish("acme/p1", a, []byte("again"))
	assert.Len(t, b.got, 1)
}
