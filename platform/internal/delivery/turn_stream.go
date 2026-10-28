package delivery

// TurnStream is one open subscription, read to its end by one reader.
type TurnStream interface {
	// Next blocks until the agent's next turn ends, and answers an error
	// once the stream carries nothing more.
	Next() (TurnEnd, error)
	// Close ends the subscription.
	Close() error
}
