package delivery

import (
	"context"
	"sync"
)

// gate is whether protected calls may be made now.
type gate int

const (
	// gateClosed holds protected calls while an activation is sought.
	gateClosed gate = iota
	// gateOpen lets them through, presenting what is held.
	gateOpen
	// gateRefused holds them after a definite refusal, until an input
	// changes (ADR-0084).
	gateRefused
)

// Execution is what this adapter's protected calls present and whether
// they may be made at all. A legacy execution presents no token and is
// open from the start, for an agent that was never cut over; a fenced
// one opens only on an activation. Either closes on garam refusing what
// was presented.
type Execution struct {
	fenced bool

	mu         sync.Mutex
	gate       gate
	activation *Activation
	changed    chan struct{}
}

// NewLegacyExecution is the execution of an agent never cut over to
// activated execution: no token, and no activation to wait for.
func NewLegacyExecution() *Execution {
	return &Execution{gate: gateOpen, changed: make(chan struct{})}
}

// NewFencedExecution is the execution of an agent whose protected calls
// carry an activation's token, closed until one is held.
func NewFencedExecution() *Execution {
	return &Execution{fenced: true, gate: gateClosed, changed: make(chan struct{})}
}

// Present answers the token a protected call carries, empty for a
// legacy execution, or ErrInactive while the gate is not open.
func (e *Execution) Present() (Token, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.gate != gateOpen {
		return "", ErrInactive
	}
	if e.activation == nil {
		return "", nil
	}
	return e.activation.Token, nil
}

// Await blocks until protected calls may be made, or ctx is done.
func (e *Execution) Await(ctx context.Context) error {
	for {
		e.mu.Lock()
		open, changed := e.gate == gateOpen, e.changed
		e.mu.Unlock()
		if open {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Refused records garam refusing what a protected call presented. The
// gate closes only while that is still what is held: a token a newer
// activation already replaced says nothing about the one that did.
func (e *Execution) Refused(presented Token) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.gate != gateOpen {
		return
	}
	if e.activation != nil && e.activation.Token != presented {
		return
	}
	e.set(gateRefused)
}

// hold opens the gate on an activation, and answers false for one it
// discards: within one activation the highest token version is kept,
// whatever order answers arrive in, so a lower or equal one changes
// nothing.
func (e *Execution) hold(a *Activation) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if held := e.activation; held != nil && held.ID == a.ID && held.TokenVersion >= a.TokenVersion {
		return false
	}
	e.activation = a
	e.set(gateOpen)
	return true
}

// state answers the gate and the activation held, for the supervisor.
func (e *Execution) state() (gate, *Activation) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.gate, e.activation
}

// close holds protected calls, as refused or while an activation is
// sought.
func (e *Execution) close(to gate) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.set(to)
}

// open lets protected calls through on what is already held, which for
// a legacy execution is nothing.
func (e *Execution) open() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.set(gateOpen)
}

// set moves the gate and wakes whoever waits on it. The caller holds
// mu.
func (e *Execution) set(to gate) {
	if e.gate == to {
		return
	}
	e.gate = to
	close(e.changed)
	e.changed = make(chan struct{})
}
