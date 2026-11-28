package bot

import (
	"sync"
	"time"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// How a bot's mailbox polls are bounded, per replica (ADR-0194
// decision 3).
const (
	// MailboxPollsHeld is how many polls one bot holds at once: its
	// process's own, and one more so that a process replacing it, or a
	// poll retried before the server saw the last one end, is answered.
	MailboxPollsHeld = 2
	// MailboxPollsPerBot bounds the polls one bot sends per
	// operator.ExchangeWindow: five a second, far past one held poll's
	// pace and a message a poll.
	MailboxPollsPerBot = 300
	// mailboxRetryAfter is what a poll refused for the held bound is
	// told to wait: one held poll's wait is the longest the slot stays
	// taken.
	mailboxRetryAfter = 30 * time.Second
)

// heldPolls counts the polls each bot holds. A bot holding none has no
// entry, so the map holds at most the polls in flight.
type heldPolls struct {
	mu   sync.Mutex
	held map[grn.GRN]int
}

func (h *heldPolls) take(g grn.GRN) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held[g] >= MailboxPollsHeld {
		return false
	}
	h.held[g]++
	return true
}

func (h *heldPolls) give(g grn.GRN) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held[g]--; h.held[g] <= 0 {
		delete(h.held, g)
	}
}

// HoldMailboxPoll admits a poll of g's mailbox, refusing one past
// MailboxPollsPerBot a window or past MailboxPollsHeld held at once
// with *operator.Exceeded, and answers what ends the hold.
func (s *service) HoldMailboxPoll(g grn.GRN) (func(), error) {
	if err := s.mailboxLimit.Check(g.String()); err != nil {
		return nil, err
	}
	if !s.polls.take(g) {
		return nil, &operator.Exceeded{RetryAfter: mailboxRetryAfter}
	}
	var once sync.Once
	return func() { once.Do(func() { s.polls.give(g) }) }, nil
}
