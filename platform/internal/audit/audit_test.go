package audit_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/audit"
)

// TestSend_NamesAMessageExactlyWhenAuthorized holds the event's one
// invariant: an authorized send names the message it stored, and a
// refused send names none, whatever else they share.
func TestSend_NamesAMessageExactlyWhenAuthorized(t *testing.T) {
	sender := grn.New("acme", grn.DefaultProject, "agent", "sender")
	recipient := grn.New("globex", grn.DefaultProject, "agent", "recipient")
	stored := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)

	authorized := audit.Authorized(sender, recipient, "m-1", stored)
	assert.Equal(t, audit.DecisionAuthorized, authorized.Decision())
	id, at, ok := authorized.Message()
	assert.True(t, ok)
	assert.Equal(t, "m-1", id)
	assert.True(t, stored.Equal(at))

	refused := audit.Refused(sender, recipient)
	assert.Equal(t, audit.DecisionRefused, refused.Decision())
	assert.Equal(t, sender, refused.Sender())
	assert.Equal(t, recipient, refused.Recipient())
	_, _, ok = refused.Message()
	assert.False(t, ok, "a refused send named a message")
}
