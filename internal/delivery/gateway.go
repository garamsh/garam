package delivery

import "context"

// Gateway is the agent platform's side: the ingress that takes a
// message and the event stream a turn's end arrives on.
type Gateway interface {
	// Post hands one message to the agent and returns the identifier the
	// gateway minted for it. A caller cannot supply one, so this answer
	// is the only thing a turn's event can be paired back to.
	Post(ctx context.Context, body string) (GatewayID, error)
	// Subscribe opens one stream of the agent's events, which lasts
	// until it is closed, it drops, or ctx is done.
	Subscribe(ctx context.Context) (TurnStream, error)
}
