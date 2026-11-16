package machineapi

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/agent"
)

// AgentReader reads an agent garam holds by its GRN, and answers
// [agent.ErrAgentNotFound] for one it does not. A send reads it for the
// sender's own organization alone, to tell a recipient that names no
// agent from one the policy service can decide (ADR-0168).
type AgentReader interface {
	Get(ctx context.Context, g grn.GRN) (*agent.Agent, error)
}
