package message

import (
	"encoding/json"

	"github.com/garamsh/garam/platform/internal/errorx"
)

// ContentContract names the content a runtime is handed for a message
// (ADR-0084). garam encodes it and signs its digest (ADR-0101), so a
// relay forwards the characters unaltered rather than encoding them
// again.
const ContentContract = "garam-message.v1"

// content is ContentContract: its members in this order, encoded by
// encoding/json's default, so every attempt of one message carries the
// same characters and a runtime's replay check holds. The body is data
// that cannot reach either member beside it.
type content struct {
	Contract string `json:"contract"`
	Sender   string `json:"sender"`
	Body     string `json:"body"`
}

// Content encodes what a runtime is handed for m. The sender is the
// one the message was stored under, which for a message garam wrote is
// the intended recipient of the post it refuses (ADR-0035); who wrote
// it is the signature's to say (ADR-0101).
func Content(m *Message) (string, error) {
	encoded, err := json.Marshal(content{Contract: ContentContract, Sender: m.Sender.String(), Body: m.Body})
	if err != nil {
		return "", errorx.Wrap(err, "message: content", errorx.KindInternal)
	}
	return string(encoded), nil
}
