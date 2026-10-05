// Package delivery carries a message from garam's queue into the agent
// platform's gateway and reports back the turn that handled it. It is
// what docs/architecture/adapter.md calls the adapter (ADR-0013,
// ADR-0018), named here for what it owns.
//
// It runs beside exactly one agent and holds what it is carrying in
// memory alone. A restart loses that, the leases it was holding lapse,
// and garam hands those messages out again: the volatility is the
// mechanism rather than a gap in it, so nothing here is transactional
// and nothing is recovered across a restart. Whether a message has
// already been handled is garam's question, never this package's.
package delivery

import (
	"log/slog"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Delivery sentinels. The loop acts on these and treats every other
// failure as transient, because every other failure ends the same way:
// the claim is not completed, so the message arrives again, or the
// reply stays in the outbox and is offered again.
var (
	// ErrNoMessageReady is the answer to a poll that waited its time and
	// found nothing addressed to the agent. The caller asks again.
	ErrNoMessageReady = errorx.New(errorx.KindNotFound, "delivery: no message ready")
	// ErrClaimLapsed is garam refusing a renewal or a completion for a
	// hold this adapter no longer has. The lease ran out or the ceiling
	// it was held at did, and the message is on its way to whoever
	// claims it next.
	ErrClaimLapsed = errorx.New(errorx.KindFailedPrecondition, "delivery: claim no longer held")
	// ErrFenced is garam refusing the execution token or the credential
	// a protected call presented: no protected call succeeds until what
	// the adapter presents changes (ADR-0084).
	ErrFenced = errorx.New(errorx.KindPermissionDenied, "delivery: garam refused the execution token or credential")
	// ErrUnsupportedPeer is an answer outside the contract the call was
	// made under: a route the peer does not serve, or an answer that does
	// not name the contract's version. Nothing falls back from it.
	ErrUnsupportedPeer = errorx.New(errorx.KindUnimplemented, "delivery: the peer does not serve this contract")
	// ErrInactive is a protected call asked for while no activation is
	// held. Nothing reaches garam.
	ErrInactive = errorx.New(errorx.KindUnavailable, "delivery: no activation is held")
)

// ClaimID is garam's identifier for one hold on one message, which
// changes with every redelivery. MessageID is garam's identifier for
// the message itself, which does not: it is what the runtime is told,
// so two claims of one message are one source there (ADR-0079). The
// two are never interchangeable.
type (
	ClaimID   string
	MessageID string
)

// Claim is a message garam handed this adapter and the hold it has on
// it: the message and its verified sender go to the runtime, and the
// hold is what completes.
type Claim struct {
	ID        ClaimID
	ExpiresAt time.Time
	Message   MessageID
	Sender    grn.GRN
	Body      string
}

// TurnOutcome is how a turn ended. The platform emits exactly one
// terminal event per turn and only a terminal event names the messages
// that turn drained, so these two are the only kinds this package reads.
type TurnOutcome string

const (
	TurnFinished TurnOutcome = "turn_finished"
	TurnFailed   TurnOutcome = "turn_failed"
)

// TurnEnd is one turn that ended and the messages it drained. A turn
// takes a batch as a single input, so one end names as many messages as
// it consumed.
type TurnEnd struct {
	Outcome  TurnOutcome
	Messages []MessageID
}

// Completes reports whether this turn completes the claims it names. A
// turn that succeeded and named its message is the only thing that
// completes one; a turn that failed is an attempt, and its message is
// redelivered (ADR-0018).
func (t TurnEnd) Completes() bool { return t.Outcome == TurnFinished }

// Progress is how far the runtime has got with a message it was
// handed. Only ProgressCompleted completes a claim; the other two leave
// it to the turn's event or to the lease.
type Progress string

const (
	ProgressPending   Progress = "pending"
	ProgressCompleted Progress = "completed"
	ProgressUnknown   Progress = "unknown"
)

// RuntimeState is whether the runtime is taking work or draining
// before it stops.
type RuntimeState string

const (
	RuntimeServing  RuntimeState = "serving"
	RuntimeDraining RuntimeState = "draining"
)

// RuntimeStatus is what the runtime says about the process serving the
// agent: the identity and revision it started under, the generation
// that start minted, and whether it is draining. The epoch and revision
// are opaque and compared whole; an empty revision is unknown.
type RuntimeStatus struct {
	Agent      grn.GRN
	Epoch      string
	Generation string
	Revision   string
	State      RuntimeState
	StartedAt  time.Time
}

// Token is an execution token, which every protected call presents. It
// never renders as itself: a token that reached a log would be a
// credential written down.
type Token string

// String renders the token redacted.
func (Token) String() string { return "[redacted]" }

// LogValue renders the token redacted in a structured log line.
func (Token) LogValue() slog.Value { return slog.StringValue("[redacted]") }

// Binding is what one activation is of: the agent, the assignment's
// epoch, the runtime's generation and its desired revision, all as the
// runtime reported them and compared whole.
type Binding struct {
	Agent      grn.GRN
	Epoch      string
	Generation string
	Revision   string
}

// BindingOf is the binding a runtime status asks to be activated.
func BindingOf(s *RuntimeStatus) Binding {
	return Binding{Agent: s.Agent, Epoch: s.Epoch, Generation: s.Generation, Revision: s.Revision}
}

// Activation is what the lifecycle authority answered an activation
// with, its binding already verified against the request.
type Activation struct {
	ID           string
	TokenVersion int
	Token        Token
	Binding      Binding
}

// Outbound is one reply the adapter offers garam: an outbox entry's
// target, identifier and content, and the instant it was created.
type Outbound struct {
	Recipient      grn.GRN
	IdempotencyKey string
	Body           string
	CreatedAt      time.Time
}

// Stored is the message garam answered an outbound with. It is an
// acknowledgement of that outbound only when [Stored.Acknowledges]
// says so.
type Stored struct {
	ID             string
	Sender         string
	Recipient      string
	IdempotencyKey string
}

// Acknowledges reports whether garam stored this outbound as sent by
// sender. A cross-organization refusal is stored too, addressed back,
// so the status alone acknowledges nothing.
func (s *Stored) Acknowledges(sender grn.GRN, o Outbound) bool {
	return s.ID != "" && s.Sender == sender.String() &&
		s.Recipient == o.Recipient.String() && s.IdempotencyKey == o.IdempotencyKey
}
