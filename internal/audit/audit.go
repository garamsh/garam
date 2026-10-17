// Package audit owns the record garam keeps of what it decided, held
// by garam rather than by either party it decided between (ADR-0106).
//
// Send events are the only events it holds. Decision and change events
// are not built.
package audit

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Decision is what garam decided about one send.
type Decision string

const (
	// DecisionAuthorized is a send garam admitted and stored.
	DecisionAuthorized Decision = "authorized"
	// DecisionRefused is a send garam did not deliver to its
	// recipient: a policy denied it, or no consent relation carried it
	// across the organization boundary.
	DecisionRefused Decision = "refused"
)

// Send is one message send as garam decided it. It names the parties
// and the message and never carries a body, so the record outlives no
// content (ADR-0106). The fields are closed: an authorized send always
// names the message it stored, and a refused one never names one.
type Send struct {
	sender    grn.GRN
	recipient grn.GRN
	decision  Decision
	messageID string
	stored    time.Time
}

// Authorized is the send from sender to recipient that stored the
// message id, which the queue stored at stored.
func Authorized(sender, recipient grn.GRN, messageID string, stored time.Time) Send {
	return Send{sender: sender, recipient: recipient, decision: DecisionAuthorized,
		messageID: messageID, stored: stored}
}

// Refused is the send from sender to recipient that reached no
// recipient.
func Refused(sender, recipient grn.GRN) Send {
	return Send{sender: sender, recipient: recipient, decision: DecisionRefused}
}

// Sender is the identity that sent.
func (s Send) Sender() grn.GRN { return s.sender }

// Recipient is the identity the send was addressed to.
func (s Send) Recipient() grn.GRN { return s.recipient }

// Decision is what garam decided.
func (s Send) Decision() Decision { return s.decision }

// Message is the identifier of the message an authorized send stored
// and when the queue stored it; ok is false for a refused send.
func (s Send) Message() (id string, stored time.Time, ok bool) {
	return s.messageID, s.stored, s.decision == DecisionAuthorized
}
