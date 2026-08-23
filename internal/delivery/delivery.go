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
	"time"

	"github.com/garamsh/garam/internal/errorx"
)

// Delivery sentinels. The loop acts on these two and treats every other
// failure as transient, because every other failure ends the same way:
// the claim is not completed, so the message arrives again.
var (
	// ErrNoMessageReady is the answer to a poll that waited its time and
	// found nothing addressed to the agent. The caller asks again.
	ErrNoMessageReady = errorx.New(errorx.KindNotFound, "delivery: no message ready")
	// ErrClaimLapsed is garam refusing a renewal or a completion for a
	// hold this adapter no longer has. The lease ran out or the ceiling
	// it was held at did, and the message is on its way to whoever
	// claims it next.
	ErrClaimLapsed = errorx.New(errorx.KindFailedPrecondition, "delivery: claim no longer held")
)

// ClaimID is garam's identifier for one hold on one message.
// GatewayID is what the platform minted for the same message when it
// took it. Pairing the two is this package's whole state, and the two
// are never interchangeable.
type (
	ClaimID   string
	GatewayID string
)

// Claim is a message garam handed this adapter and the hold it has on
// it: the body goes to the gateway, and the hold is what completes.
type Claim struct {
	ID        ClaimID
	ExpiresAt time.Time
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
	Messages []GatewayID
}

// Completes reports whether this turn completes the claims it names. A
// turn that succeeded and named its message is the only thing that
// completes one; a turn that failed is an attempt, and its message is
// redelivered (ADR-0018).
func (t TurnEnd) Completes() bool { return t.Outcome == TurnFinished }
