package agent

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
)

// AccessTokenStore records the tokens an agent's assignee mints for it
// and answers what a presented one stands for (ADR-0100).
type AccessTokenStore interface {
	// MintAccessToken records t where caller holds the agent t names at
	// the current epoch, and fills t's Epoch and Lineage from the state
	// it read, under the agent's execution row. An agent is admitted
	// inside its lineage, because the lifecycle authority opened that
	// lineage. It keeps the agent's newest LiveAccessTokensPerAudience
	// for t's audience and its newest LiveAccessTokensPerAgent in all.
	//
	// It answers ErrNotAssigned where caller does not hold the agent or
	// the agent does not exist, and ErrIssuedByLifecycle for an agent
	// with no lineage yet.
	MintAccessToken(ctx context.Context, caller grn.GRN, t *AccessToken) error
	// ResolveAccessToken answers what the live token tokenHash names,
	// read against the agent's current assignment and lineage, or
	// ErrAccessTokenInvalid where none is live.
	ResolveAccessToken(ctx context.Context, tokenHash string) (AccessTokenHolder, error)
}
