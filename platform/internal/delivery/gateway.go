package delivery

import "context"

// Gateway is the agent platform's side: the ingress that takes a
// message, the lookup that says whether a message it took completed,
// the event stream a turn's end arrives on, and the status of the
// process serving it.
type Gateway interface {
	// Post hands one message to the agent under garam's identifier for
	// it, with garam's signature over content where signature is not
	// empty, and answers how far the runtime has already got with it.
	// An identical replay of a completed message runs no second turn.
	Post(ctx context.Context, id MessageID, content, signature string) (Progress, error)
	// Lookup answers how far the runtime has got with a message it was
	// handed, which recovers a completion whose event was lost.
	Lookup(ctx context.Context, id MessageID) (Progress, error)
	// Subscribe opens one stream of the agent's events, which lasts
	// until it is closed, it drops, or ctx is done.
	Subscribe(ctx context.Context) (TurnStream, error)
	// Status answers which generation of the runtime is serving, and
	// under what identity and revision it started.
	Status(ctx context.Context) (*RuntimeStatus, error)
}
