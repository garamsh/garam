package machine

import "context"

// Recorder keeps, for each caller and path, the first and last time it
// was seen, and for an exchange or a mint until when the token it last
// issued is accepted (ADR-0109). The composition root satisfies it from
// internal/machine/postgres.
type Recorder interface {
	Record(ctx context.Context, s Sighting) error
}
