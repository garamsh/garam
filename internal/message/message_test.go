package message_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
)

func recipientGRN() grn.GRN {
	return grn.New("acme", grn.DefaultProject, "agent", "bot")
}

func senderGRN() grn.GRN {
	return grn.New("acme", grn.DefaultProject, "user", "ada")
}

func TestNew_CarriesWhatTheSenderSuppliedUnderAnIdentifierOfItsOwn(t *testing.T) {
	m, err := message.New(recipientGRN(), senderGRN(), "key-1", "hello")
	require.NoError(t, err)
	assert.Equal(t, recipientGRN(), m.Recipient())
	assert.Equal(t, senderGRN(), m.Sender())
	assert.Equal(t, "key-1", m.IdempotencyKey())
	assert.Equal(t, "hello", m.Body())
	assert.Len(t, m.ID(), 32, "sixteen random bytes, hex-encoded")

	other, err := message.New(recipientGRN(), senderGRN(), "key-1", "hello")
	require.NoError(t, err)
	assert.NotEqual(t, m.ID(), other.ID(), "the identifier is the queue's, not the sender's")
}

// The queue carries the body without reading it, so an empty one is a
// message like any other.
func TestNew_CarriesAnEmptyBody(t *testing.T) {
	m, err := message.New(recipientGRN(), senderGRN(), "key-1", "")
	require.NoError(t, err)
	assert.Empty(t, m.Body())
}

// TestNew_BoundsTheBodyAtTheSubmissionLimit pins the boundary exactly:
// a body at the bound is carried whole, and one a character past it is
// refused. The accepted half is the control — it stops being accepted
// only if what refuses the other half is the bound.
func TestNew_BoundsTheBodyAtTheSubmissionLimit(t *testing.T) {
	at := strings.Repeat("b", message.MaxBodyLength)
	m, err := message.New(recipientGRN(), senderGRN(), "key-1", at)
	require.NoError(t, err)
	assert.Equal(t, at, m.Body(), "a body at the bound is carried whole")

	m, err = message.New(recipientGRN(), senderGRN(), "key-1", at+"b")
	assert.Nil(t, m)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "a body past the bound is refused")
}

// TestNew_RejectsAMessageNothingCouldBeDoneWith reads each refusal
// against the message that carries all three: every case below is that
// message with one part emptied, so what refuses is that part rather
// than a constructor that refuses everything.
func TestNew_RejectsAMessageNothingCouldBeDoneWith(t *testing.T) {
	accepted, err := message.New(recipientGRN(), senderGRN(), "key-1", "hello")
	require.NoError(t, err)
	require.NotNil(t, accepted)

	tests := []struct {
		name      string
		recipient grn.GRN
		sender    grn.GRN
		key       string
	}{
		{
			name:      "no recipient to deliver to",
			recipient: grn.GRN{},
			sender:    senderGRN(),
			key:       "key-1",
		},
		{
			name:      "no sender to report a failure to",
			recipient: recipientGRN(),
			sender:    grn.GRN{},
			key:       "key-1",
		},
		{
			name:      "no key to suppress a duplicate by",
			recipient: recipientGRN(),
			sender:    senderGRN(),
			key:       "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := message.New(tt.recipient, tt.sender, tt.key, "hello")
			assert.Nil(t, m)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}
