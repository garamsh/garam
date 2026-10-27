package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// ChannelLivePath is where a channel's messages arrive as they are posted.
const ChannelLivePath = "GET /orgs/{org}/channels/{channel}/live"

// writeTimeout bounds one message's write to a listener.
const writeTimeout = 10 * time.Second

// liveBuffer is how many messages a listener may fall behind by before
// it is dropped, to reconnect and read what it missed.
const liveBuffer = 256

// listener is one live connection's outbound queue.
type listener struct {
	out  chan []byte
	once sync.Once
	slow chan struct{}
}

func (l *listener) Deliver(msg []byte) {
	select {
	case l.out <- msg:
	default:
		l.once.Do(func() { close(l.slow) })
	}
}

// ChannelLive upgrades to a WebSocket that carries each message posted
// in the channel, one text frame each. The upgrade is refused as
// getChannel would refuse it; reading is decided there, once.
func (s *Server) ChannelLive(w http.ResponseWriter, r *http.Request) {
	org, id := r.PathValue("org"), r.PathValue("channel")
	c, err := s.caller(r)
	if err != nil {
		writeError(w, r, err)
		return
	}
	l := &listener{out: make(chan []byte, liveBuffer), slow: make(chan struct{})}
	leave, err := s.channels.Listen(r.Context(), c, org, id, l)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer leave()
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: s.origins})
	if err != nil {
		return
	}
	// The client sends nothing; CloseRead answers its pings and ends
	// ctx when it leaves.
	ctx := conn.CloseRead(r.Context())
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		case <-l.slow:
			_ = conn.Close(websocket.StatusTryAgainLater, "too far behind")
			return
		case msg := <-l.out:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := conn.Write(wctx, websocket.MessageText, msg)
			cancel()
			if err != nil {
				return
			}
		}
	}
}
