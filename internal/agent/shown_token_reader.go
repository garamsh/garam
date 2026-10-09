package agent

import (
	"context"

	"github.com/garamsh/garam/internal/grn"
)

// ShownTokenReader reads an agent token its assignee minted to be shown
// to a hosted operator, which that operator forwards to garam where it
// forwarded the agent's certificate before (ADR-0100).
type ShownTokenReader interface {
	// AccessTokenShownTo answers what the token tokenHash names where
	// it was minted to be shown to audience, as it was recorded: its
	// dates, and whether the agent's assignment and lineage still admit
	// it, are the execution fence's to judge. It answers
	// ErrTokenNotProven where no such token is recorded.
	AccessTokenShownTo(ctx context.Context, tokenHash string, audience grn.GRN) (AccessTokenHolder, error)
}
