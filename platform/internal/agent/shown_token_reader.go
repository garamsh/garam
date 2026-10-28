package agent

import (
	"context"

	"github.com/garamsh/garam/platform/internal/grn"
)

// ShownTokenReader reads an agent token a lifecycle authority forwards
// to garam where it forwarded the agent's certificate before
// (ADR-0100): one the assignee minted to be shown to that hosted
// operator, or one a standalone host's supervisor, the assignee and
// the authority at once, minted for garam. The execution service, not
// this read, admits the second for a supervisor alone.
type ShownTokenReader interface {
	// AccessTokenShownTo answers what the token tokenHash names where
	// it was minted to be shown to audience, or minted by audience for
	// garam, as it was recorded: its
	// dates, and whether the agent's assignment and lineage still admit
	// it, are the execution fence's to judge. It answers
	// ErrTokenNotProven where no such token is recorded.
	AccessTokenShownTo(ctx context.Context, tokenHash string, audience grn.GRN) (AccessTokenHolder, error)
}
