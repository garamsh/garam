package bot_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/operator"
)

// TestHoldMailboxPoll_HoldsTwoPollsABotAtOnce holds ADR-0194
// decision 3's bound on polls held at once: a bot's third poll is
// refused while two are held, and admitted once one ends. Another
// bot's poll, the control, is admitted throughout, so what refused
// the third was its bot's count.
func TestHoldMailboxPoll_HoldsTwoPollsABotAtOnce(t *testing.T) {
	svc, _ := newBotService()
	g, other := bot.BotGRN("acme", "decl-a"), bot.BotGRN("acme", "decl-b")

	first, err := svc.HoldMailboxPoll(g)
	require.NoError(t, err)
	_, err = svc.HoldMailboxPoll(g)
	require.NoError(t, err)
	_, err = svc.HoldMailboxPoll(g)
	var exceeded *operator.Exceeded
	require.ErrorAs(t, err, &exceeded, "a third poll held at once")
	assert.Positive(t, exceeded.RetryAfterSeconds())

	_, err = svc.HoldMailboxPoll(other)
	assert.NoError(t, err, "the control: another bot's poll")

	first()
	first()
	_, err = svc.HoldMailboxPoll(g)
	assert.NoError(t, err, "a poll ended gives its slot back")
	_, err = svc.HoldMailboxPoll(g)
	assert.ErrorAs(t, err, &exceeded, "ending one poll twice gives back one slot")
}

// TestHoldMailboxPoll_IsBoundedPerBot holds the rate bound: past
// MailboxPollsPerBot in a window a bot's poll is refused, however few
// it holds, and another bot's is not.
func TestHoldMailboxPoll_IsBoundedPerBot(t *testing.T) {
	svc, _ := newBotService()
	g, other := bot.BotGRN("acme", "decl-a"), bot.BotGRN("acme", "decl-b")
	for range bot.MailboxPollsPerBot {
		done, err := svc.HoldMailboxPoll(g)
		require.NoError(t, err)
		done()
	}
	_, err := svc.HoldMailboxPoll(g)
	var exceeded *operator.Exceeded
	assert.ErrorAs(t, err, &exceeded)
	_, err = svc.HoldMailboxPoll(other)
	assert.NoError(t, err, "the control: another bot's poll")
}
