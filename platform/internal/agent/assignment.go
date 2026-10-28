package agent

import (
	"time"

	"github.com/garamsh/garam/platform/internal/grn"
)

// Assignment binds one agent to what holds it: an operator (ADR-0012)
// or the agent's own supervisor (ADR-0082). The epoch rises with every
// assignment and fences the assignee it replaced: a state report is
// accepted only at the current epoch, so a pod left running by a
// previous assignee writes nothing.
//
// Every field is persisted on the row and exported: the type holds no
// invariant over them, so it is a data carrier (ADR-0069).
type Assignment struct {
	Agent     grn.GRN
	Assignee  grn.GRN
	Epoch     int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewAssignment binds the agent to the assignee at epoch, as of now.
// Which pairings are legal is the schema's decision and not this
// domain's (ADR-0015), so this constructs a value and validates none.
func NewAssignment(agentGRN, assigneeGRN grn.GRN, epoch int) *Assignment {
	now := time.Now().UTC()
	return &Assignment{
		Agent:     agentGRN,
		Assignee:  assigneeGRN,
		Epoch:     epoch,
		CreatedAt: now,
		UpdatedAt: now,
	}
}
