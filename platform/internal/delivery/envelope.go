package delivery

import (
	"encoding/json"

	"github.com/garamsh/garam/lib/errorx"
)

// MessageContract names the content the runtime is handed for a garam
// message. The managed and standalone instructions read it by this
// name, so another shape is another value.
const MessageContract = "garam-message.v1"

// envelope is garam-message.v1: its members in this order, encoded by
// encoding/json's default so a redelivery is byte for byte the same
// content and the runtime's replay check holds. The sender is the one
// garam's claim verified, and the body is data that cannot reach either
// member beside it.
type envelope struct {
	Contract string `json:"contract"`
	Sender   string `json:"sender"`
	Body     string `json:"body"`
}

// Content is what the runtime is told about a claimed message: the
// octets garam answered the claim with, unchanged, because its
// signature binds them. A garam that answers none predates ADR-0101,
// and the adapter encodes the same members itself.
func (c *Claim) Content() (string, error) {
	if c.Encoded != "" {
		return c.Encoded, nil
	}
	encoded, err := json.Marshal(envelope{Contract: MessageContract, Sender: c.Sender.String(), Body: c.Body})
	if err != nil {
		return "", errorx.Wrap(err, "delivery: message content", errorx.KindInternal)
	}
	return string(encoded), nil
}
