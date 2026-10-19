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
	"strconv"
	"time"

	"github.com/garamsh/garam/internal/audit"
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
	// ErrNoSuchMessage is returned when the caller has no message under
	// that identifier. A message another identity sent, one that never
	// existed, and one the sweep deleted are the same answer, so asking
	// teaches a caller nothing about the queue outside its own messages
	// (ADR-0055).
	ErrNoSuchMessage = errorx.New(errorx.KindNotFound, "no such message")
	// ErrUnsupportedTokenDeliveryContract refuses a durable submission
	// carrying no contract version, or one this release does not serve,
	// and names the one it does (ADR-0083, ADR-0100).
	ErrUnsupportedTokenDeliveryContract = errorx.New(errorx.KindInvalidArgument,
		"unsupported Garam-Contract-Version: a request presenting an agent token is served under "+TokenDeliveryContractVersion)
	// ErrCreatedLater refuses a durable submission created after the
	// database's decision instant. Its sender's clock runs ahead of the
	// database's, which time resolves, so the refusal is not final
	// (ADR-0083).
	ErrCreatedLater = errorx.New(errorx.KindOutOfRange, "createdAt is later than garam's clock")
	// ErrSubmissionExpired refuses a durable submission whose budget had
	// run out at the decision instant and whose key stores nothing. No
	// later attempt under that key stores one (ADR-0083).
	ErrSubmissionExpired = errorx.New(errorx.KindDeadlineExceeded,
		"createdAt is past the submission budget and nothing is stored under the key")
	// ErrKeyHoldsAnotherMessage refuses a durable submission whose key
	// already stores a message to another recipient or with another
	// body (ADR-0083).
	ErrKeyHoldsAnotherMessage = errorx.New(errorx.KindAlreadyExists,
		"the idempotency key already stores a different message")
)

// ActionSend is the action a policy names to decide what one identity
// may send to another. The constant is declared here because this
// package defines sending; the engine matches the string only
// (authz.md §Policy document).
const ActionSend = "message:send"

const (
	// MaxBodyLength bounds a submission's body, in characters as the
	// submission schema's maxLength counts them (ADR-0034): well past
	// any plausible agent turn, small enough that the table's worst
	// case is arithmetic.
	MaxBodyLength = 65536
	// RetentionWindow is how long an undelivered message is kept before
	// the sweep deletes it whole (ADR-0034). It must exceed the retry
	// budget a sender operates under, because a deleted row cannot
	// suppress a retry — the budget is the senders', and the window
	// names it.
	RetentionWindow = 30 * 24 * time.Hour
	// SubmissionBudget is how long after its creation a durable submission
	// may still store a message (ADR-0083). It is part of
	// TokenDeliveryContractVersion rather than a setting, and sits well
	// inside RetentionWindow, so every message it admits outlives the last
	// retry that could duplicate it.
	SubmissionBudget = 7 * 24 * time.Hour
)

// TokenDeliveryContractVersion names the durable submission contract
// (ADR-0083, ADR-0100). Its route requires the version in the
// Garam-Contract-Version header and answers with it, and the caller's
// execution token travels in a header of its own.
const TokenDeliveryContractVersion = "durable-delivery.v2"

// ID identifies a stored message. ClaimID identifies one reader's hold
// on it; the two are never interchangeable.
type (
	ID      string
	ClaimID string
)

// Author is who wrote a message, which is what garam signs as its
// sender when it hands the message out (ADR-0101).
type Author string

const (
	// AuthorSender is a message the identity its listener authenticated
	// posted, which is every message but one kind.
	AuthorSender Author = "sender"
	// AuthorGaram is a message garam wrote itself: the refusal of a
	// cross-organization post, stored under the intended recipient's
	// identity (ADR-0035). No caller posted it, so garam signs it as its
	// own.
	AuthorGaram Author = "garam"
)

// sent is the audit event the post of submitted writes beside what its
// key stores (ADR-0106). The parties are the submission's and the
// message is what the key stores, which a retry finds already there.
// garam's own refusal stands in for a crossing it refused, so its event
// is that send's, with the parties swapped back and no message: the
// one stored is the refusal, not what was sent.
func sent(submitted, stored *Message) audit.Send {
	if submitted.author == AuthorGaram {
		return audit.Refused(submitted.recipient, submitted.sender)
	}
	return audit.Authorized(submitted.sender, submitted.recipient, string(stored.id), stored.createdAt)
}

// parseAuthor reads an author. The set is closed, so anything else is a
// caller or a row this process does not understand.
func parseAuthor(s string) (Author, error) {
	switch Author(s) {
	case AuthorSender, AuthorGaram:
		return Author(s), nil
	default:
		return "", errorx.New(errorx.KindInvalidArgument, "message: unknown author "+s)
	}
}

// Message is one message addressed to a recipient. The body is opaque:
// the queue carries it and never reads it. It is dropped at completion
// (ADR-0034), so a message read back after delivery carries none.
type Message struct {
	id             ID
	recipient      grn.GRN
	sender         grn.GRN
	idempotencyKey string
	body           string
	author         Author
	createdAt      time.Time
	attempts       int
}

// New builds a message from what a sender supplied. The recipient is
// not checked against the agents garam knows: garam stores a message
// before it can route it and reports a routing failure back as a
// message (ADR-0013), so an unknown recipient is not a reason to refuse
// one.
func New(recipient, sender grn.GRN, idempotencyKey, body string) (*Message, error) {
	return newAuthored(recipient, sender, idempotencyKey, body, AuthorSender)
}

// newAuthored is New for a message whose author is given.
func newAuthored(recipient, sender grn.GRN, idempotencyKey, body string, author Author) (*Message, error) {
	if _, err := parseAuthor(string(author)); err != nil {
		return nil, err
	}
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
	// The bound is the schema's maxLength, counted the way it counts.
	if len([]rune(body)) > MaxBodyLength {
		return nil, errorx.New(errorx.KindInvalidArgument,
			"message: body must be at most "+strconv.Itoa(MaxBodyLength)+" characters")
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
		author:         author,
	}, nil
}

func (m *Message) ID() ID                 { return m.id }
func (m *Message) Recipient() grn.GRN     { return m.recipient }
func (m *Message) Sender() grn.GRN        { return m.sender }
func (m *Message) IdempotencyKey() string { return m.idempotencyKey }
func (m *Message) Body() string           { return m.body }
func (m *Message) Author() Author         { return m.author }

// CreatedAt is when the queue stored the message, on the database's
// clock — the one the retention sweep measures with — and the zero time
// on a message not stored yet.
func (m *Message) CreatedAt() time.Time { return m.createdAt }

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
	claimedAt time.Time
	expiresAt time.Time
}

func (c *Claim) Message() *Message    { return c.message }
func (c *Claim) ID() ClaimID          { return c.id }
func (c *Claim) ExpiresAt() time.Time { return c.expiresAt }

// ClaimedAt is when the claim was taken, on the database's clock. The
// lease ceiling is measured from it, and so is a hand-out's signature
// (ADR-0101).
func (c *Claim) ClaimedAt() time.Time { return c.claimedAt }

// State is how far a message has got, as its sender may observe it
// (ADR-0055). The four are exhaustive over a stored message: what is
// not one of them is a message that is no longer stored, which is
// ErrNoSuchMessage rather than a state.
type State string

const (
	// StatePending is a message waiting for a reader, or one whose last
	// claim lapsed and which will be handed out again.
	StatePending State = "pending"
	// StateClaimed is a message a reader holds under a lease that has
	// not run out.
	StateClaimed State = "claimed"
	// StateCompleted is a message a reader reported delivered.
	StateCompleted State = "completed"
	// StateExhausted is a message that has spent its attempts without
	// being completed, so the queue hands it out no further (ADR-0018).
	StateExhausted State = "exhausted"
)

// Status is what a sender may observe of a message it sent: what garam
// knows about it that the sender does not. It carries no body — the
// sender wrote it, and a completed message no longer has one
// (ADR-0034) — and no recipient or key, which were the submission's.
type Status struct {
	id        ID
	state     State
	attempts  int
	createdAt time.Time
}

// NewStatus builds the answer to a sender's read from what the queue
// holds. The state is the store's to decide: the lease it rests on is
// measured where the queue measures every other one, so no process's
// clock decides whether a claim still stands.
func NewStatus(id ID, state State, attempts int, createdAt time.Time) *Status {
	return &Status{id: id, state: state, attempts: attempts, createdAt: createdAt}
}

func (s *Status) ID() ID               { return s.id }
func (s *Status) State() State         { return s.state }
func (s *Status) Attempts() int        { return s.attempts }
func (s *Status) CreatedAt() time.Time { return s.createdAt }

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

// Fence is what an agent's write presents to the agent execution fence
// (ADR-0080), as the transport authenticated it: the agent, the
// certificate its connection verified, and the hash of the execution
// token its request carries, nil where it carries none.
type Fence struct {
	Agent     grn.GRN
	Serial    string
	NotBefore time.Time
	NotAfter  time.Time
	TokenHash []byte
}
