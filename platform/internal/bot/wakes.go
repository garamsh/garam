package bot

import (
	"sync"

	"github.com/garamsh/garam/lib/grn"
)

// Wakes is where a process's held polls wait for a write to their
// record. It carries no change, only that one was written: the poll
// reads what was. A Wakes delivers within its process, and the store's
// listener carries a write from any process to it.
type Wakes struct {
	mu      sync.Mutex
	waiters map[grn.GRN]map[chan struct{}]struct{}
}

// NewWakes constructs an empty Wakes.
func NewWakes() *Wakes {
	return &Wakes{waiters: map[grn.GRN]map[chan struct{}]struct{}{}}
}

// Subscribe answers a channel that receives once g is written, and the
// func that stops it. A write while nothing receives is kept, once, so
// one between a poll's read and its wait is not lost.
func (w *Wakes) Subscribe(g grn.GRN) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.waiters[g] == nil {
		w.waiters[g] = map[chan struct{}]struct{}{}
	}
	w.waiters[g][ch] = struct{}{}
	return ch, func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		delete(w.waiters[g], ch)
		if len(w.waiters[g]) == 0 {
			delete(w.waiters, g)
		}
	}
}

// Wake tells every poll held on g that it was written.
func (w *Wakes) Wake(g grn.GRN) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch := range w.waiters[g] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
