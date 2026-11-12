package message_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/message"
)

// TestActionSend_IsDeclaredByTheManifests holds the action a message
// is checked against to one grn.Actions holds, with an undeclared name
// as the control.
func TestActionSend_IsDeclaredByTheManifests(t *testing.T) {
	declared := map[string]bool{}
	for _, a := range grn.Actions() {
		declared[a.Name] = true
	}

	assert.True(t, declared[message.ActionSend], message.ActionSend)
	assert.False(t, declared["iam:message.post"])
}
