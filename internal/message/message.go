// Package message owns the durable queue a message reaches an agent
// through (ADR-0013).
//
// A message is fire-and-forget: nothing here pairs it with a reply,
// which arrives as a message of its own. The queue is a table, so any
// garam instance can hand any recipient's message to a reader and
// nothing about a recipient is held in a process.
package message

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Domain sentinel errors.
var (
	// ErrNoMessageReady is returned when a recipient has nothing a
	// reader may claim. Messages another reader holds are not ready
	// until its lease runs out.
	ErrNoMessageReady = errorx.New(errorx.KindNotFound, "no message ready")
	// ErrStaleClaim is returned when a claim no longer holds the message
	// it was handed. A lease that ran out is claimed by the next reader,
	// and the reader it replaced completes nothing.
	ErrStaleClaim = errorx.New(errorx.KindFailedPrecondition, "message claim is stale")
	// ErrNotRecipient refuses a caller acting on a queue that is not its
	// own. A message is claimed, renewed and completed by the recipient
	// it is addressed to and by nobody else.
	ErrNotRecipient = errorx.New(errorx.KindPermissionDenied, "caller is not the recipient")
)

// ID identifies a stored message. ClaimID identifies one reader's hold
// on it; the two are never interchangeable.
type (
	ID      string
	ClaimID string
)

// Message is one message addressed to a recipient. The body is opaque:
// the queue carries it and never reads it.
type Message struct {
	id             ID
	recipient      grn.GRN
	sender         grn.GRN
	idempotencyKey string
	body           string
	createdAt      time.Time
	attempts       int
}

// New builds a message from what a sender supplied. The recipient is
// not checked against the agents garam knows: garam stores a message
// before it can route it and reports a routing failure back as a
// message (ADR-0013), so an unknown recipient is not a reason to refuse
// one.
func New(recipient, sender grn.GRN, idempotencyKey, body string) (*Message, error) {
	if err := recipient.Validate(); err != nil {
		return nil, errorx.Wrap(err, "message: recipient")
	}
	if err := sender.Validate(); err != nil {
		return nil, errorx.Wrap(err, "message: sender")
	}
	// The key scopes duplicate suppression to the sender, so an empty
	// one would collapse everything that sender ever sends into a single
	// stored message.
	if idempotencyKey == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "message: empty idempotency key")
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	return &Message{
		id:             id,
		recipient:      recipient,
		sender:         sender,
		idempotencyKey: idempotencyKey,
		body:           body,
		createdAt:      time.Now().UTC(),
	}, nil
}

func (m *Message) ID() ID                 { return m.id }
func (m *Message) Recipient() grn.GRN     { return m.recipient }
func (m *Message) Sender() grn.GRN        { return m.sender }
func (m *Message) IdempotencyKey() string { return m.idempotencyKey }
func (m *Message) Body() string           { return m.body }
func (m *Message) CreatedAt() time.Time   { return m.createdAt }

// Attempts is how many times the message has been handed to a reader,
// the hold the caller is reading it through included. Delivery is
// at-least-once (ADR-0018), so this is what tells a first delivery from
// a repeat; the count is bounded, and a message that spends it is not
// handed out again.
func (m *Message) Attempts() int { return m.attempts }

// Claim is a message handed to one reader and the token that proves the
// reader still holds it. It expires: a reader that dies leaves the
// message claimable instead of stranded.
type Claim struct {
	message   *Message
	id        ClaimID
	expiresAt time.Time
}

func (c *Claim) Message() *Message    { return c.message }
func (c *Claim) ID() ClaimID          { return c.id }
func (c *Claim) ExpiresAt() time.Time { return c.expiresAt }

// newID mints a message identifier. A message is a queue entry rather
// than a resource a policy names, so it carries an opaque identifier
// and no GRN.
func newID() (ID, error) {
	s, err := randomHex()
	if err != nil {
		return "", err
	}
	return ID(s), nil
}

// newClaimID mints the token one claim is fenced by.
func newClaimID() (ClaimID, error) {
	s, err := randomHex()
	if err != nil {
		return "", err
	}
	return ClaimID(s), nil
}

// randomHex returns sixteen random bytes, hex-encoded.
func randomHex() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errorx.Wrap(err, "message: random identifier")
	}
	return hex.EncodeToString(b[:]), nil
}
