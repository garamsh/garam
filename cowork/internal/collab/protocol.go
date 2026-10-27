package collab

import (
	"github.com/garamsh/garam/platform/sdk/errorx"
)

// The y-websocket wire format a client speaks (ADR-0127): a message is
// a varuint kind, then the kind's body, every integer a lib0 varuint
// (LEB128) and every byte string its length then its bytes. The web
// app's provider is y-websocket's own, so this is that format and no
// other.

// MessageKind is a message's first varuint.
type MessageKind uint64

const (
	MessageKindSync           MessageKind = 0
	MessageKindAwareness      MessageKind = 1
	MessageKindAuth           MessageKind = 2
	MessageKindQueryAwareness MessageKind = 3
)

// SyncKind is a sync message's second varuint.
type SyncKind uint64

const (
	// SyncKindStep1 carries a state vector: what the sender holds.
	SyncKindStep1 SyncKind = 0
	// SyncKindStep2 carries the update that answers a step 1.
	SyncKindStep2 SyncKind = 1
	// SyncKindUpdate carries an edit as it happens.
	SyncKindUpdate SyncKind = 2
)

// authPermissionDenied is the only auth message y-websocket defines:
// a varuint 0, then the reason as a string.
const authPermissionDenied = 0

// Message is one decoded message. Payload is the state vector, the
// update or the awareness update the message carries; a query carries
// none.
type Message struct {
	Kind    MessageKind
	Sync    SyncKind
	Payload []byte
}

// ErrMalformedMessage is a message that does not decode.
var ErrMalformedMessage = errorx.New(errorx.KindInvalidArgument, "collab: malformed message")

// Decode reads one message. A kind it does not know is malformed:
// nothing a client sends past these is anything the relay may forward.
func Decode(b []byte) (Message, error) {
	r := reader{b: b}
	kind, err := r.uint()
	if err != nil {
		return Message{}, err
	}
	m := Message{Kind: MessageKind(kind)}
	switch m.Kind {
	case MessageKindSync:
		sync, err := r.uint()
		if err != nil {
			return Message{}, err
		}
		if sync > uint64(SyncKindUpdate) {
			return Message{}, ErrMalformedMessage
		}
		m.Sync = SyncKind(sync)
		if m.Payload, err = r.bytes(); err != nil {
			return Message{}, err
		}
	case MessageKindAwareness:
		if m.Payload, err = r.bytes(); err != nil {
			return Message{}, err
		}
	case MessageKindQueryAwareness:
	default:
		return Message{}, ErrMalformedMessage
	}
	if len(r.b) != 0 {
		return Message{}, ErrMalformedMessage
	}
	return m, nil
}

// Encode writes m in the wire format.
func (m Message) Encode() []byte {
	out := appendUint(nil, uint64(m.Kind))
	switch m.Kind {
	case MessageKindSync:
		out = appendUint(out, uint64(m.Sync))
		out = appendBytes(out, m.Payload)
	case MessageKindAwareness:
		out = appendBytes(out, m.Payload)
	}
	return out
}

// permissionDenied is the message that tells a client its edits are
// refused, as y-websocket's provider reads one.
func permissionDenied(reason string) []byte {
	out := appendUint(nil, uint64(MessageKindAuth))
	out = appendUint(out, authPermissionDenied)
	return appendBytes(out, []byte(reason))
}

func appendUint(out []byte, v uint64) []byte {
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func appendBytes(out, b []byte) []byte {
	return append(appendUint(out, uint64(len(b))), b...)
}

type reader struct{ b []byte }

// uint reads a varuint of at most 64 bits.
func (r *reader) uint() (uint64, error) {
	var v uint64
	for shift := 0; shift < 64; shift += 7 {
		if len(r.b) == 0 {
			return 0, ErrMalformedMessage
		}
		c := r.b[0]
		r.b = r.b[1:]
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			return v, nil
		}
	}
	return 0, ErrMalformedMessage
}

func (r *reader) bytes() ([]byte, error) {
	n, err := r.uint()
	if err != nil {
		return nil, err
	}
	if n > uint64(len(r.b)) {
		return nil, ErrMalformedMessage
	}
	b := r.b[:n]
	r.b = r.b[n:]
	return b, nil
}
