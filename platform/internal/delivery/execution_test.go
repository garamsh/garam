// Internal test: which answer an execution keeps is decided by hold,
// which only the supervisor calls, and no exported surface can hand it
// two answers in a chosen order.
package delivery

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExecution_KeepsTheHighestTokenVersionOfAnActivation is ADR-0084's
// ordering: answers may arrive in any order, and within one activation
// a lower or equal version replaces nothing. A higher version, and
// another activation, are the controls: each is held.
func TestExecution_KeepsTheHighestTokenVersionOfAnActivation(t *testing.T) {
	e := NewFencedExecution()
	assert.True(t, e.hold(&Activation{ID: "activation-1", TokenVersion: 3, Token: "token-3"}))
	assert.False(t, e.hold(&Activation{ID: "activation-1", TokenVersion: 2, Token: "token-2"}), "an older answer arriving late")
	assert.False(t, e.hold(&Activation{ID: "activation-1", TokenVersion: 3, Token: "token-3b"}), "an equal version")
	presented, err := e.Present()
	assert.NoError(t, err)
	assert.Equal(t, Token("token-3"), presented)

	assert.True(t, e.hold(&Activation{ID: "activation-1", TokenVersion: 4, Token: "token-4"}), "the control: a newer version")
	assert.True(t, e.hold(&Activation{ID: "activation-2", TokenVersion: 1, Token: "token-b1"}), "and another activation")
	presented, _ = e.Present()
	assert.Equal(t, Token("token-b1"), presented)
}

// TestExecution_ClosesOnlyOnARefusalOfWhatItHolds is the race a newer
// activation wins: a call that presented the token it replaced is
// refused, and that refusal says nothing about the token held now. The
// refusal of the token held is the control.
func TestExecution_ClosesOnlyOnARefusalOfWhatItHolds(t *testing.T) {
	e := NewFencedExecution()
	e.hold(&Activation{ID: "activation-1", TokenVersion: 1, Token: "old"})
	e.hold(&Activation{ID: "activation-1", TokenVersion: 2, Token: "new"})

	e.Refused("old")
	_, err := e.Present()
	assert.NoError(t, err, "a refusal of a replaced token closes nothing")

	e.Refused("new")
	_, err = e.Present()
	assert.ErrorIs(t, err, ErrInactive, "the control: a refusal of the token held closes it")
}
