package agent

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Assignment binds one agent to what holds it: an operator (ADR-0012)
// or the agent's own supervisor (ADR-0082). The epoch rises with every
// assignment and fences the assignee it replaced: a state report is
// accepted only at the current epoch, so a pod left running by a
// previous assignee writes nothing.
type Assignment struct {
	agentGRN    grn.GRN
	assigneeGRN grn.GRN
	epoch       int
	createdAt   time.Time
	updatedAt   time.Time
}

// NewAssignment binds the agent to the assignee at epoch, as of now.
// Which pairings are legal is the schema's decision and not this
// domain's (ADR-0015), so this constructs a value and validates none.
func NewAssignment(agentGRN, assigneeGRN grn.GRN, epoch int) *Assignment {
	now := time.Now().UTC()
	return &Assignment{
		agentGRN:    agentGRN,
		assigneeGRN: assigneeGRN,
		epoch:       epoch,
		createdAt:   now,
		updatedAt:   now,
	}
}

func (a *Assignment) Agent() grn.GRN       { return a.agentGRN }
func (a *Assignment) Assignee() grn.GRN    { return a.assigneeGRN }
func (a *Assignment) Epoch() int           { return a.epoch }
func (a *Assignment) CreatedAt() time.Time { return a.createdAt }
func (a *Assignment) UpdatedAt() time.Time { return a.updatedAt }
