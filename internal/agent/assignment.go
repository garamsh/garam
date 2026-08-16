package agent

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Assignment binds one agent to one operator (ADR-0012). The epoch
// rises with every assignment and fences the operator it replaced: a
// state report is accepted only at the current epoch, so a pod left
// running by a previous operator writes nothing.
type Assignment struct {
	agentGRN    grn.GRN
	operatorGRN grn.GRN
	epoch       int
	createdAt   time.Time
	updatedAt   time.Time
}

// NewAssignment binds the agent to the operator at epoch, as of now.
// Which pairings are legal is the schema's decision and not this
// domain's (ADR-0015), so this constructs a value and validates none.
func NewAssignment(agentGRN, operatorGRN grn.GRN, epoch int) *Assignment {
	now := time.Now().UTC()
	return &Assignment{
		agentGRN:    agentGRN,
		operatorGRN: operatorGRN,
		epoch:       epoch,
		createdAt:   now,
		updatedAt:   now,
	}
}

func (a *Assignment) Agent() grn.GRN       { return a.agentGRN }
func (a *Assignment) Operator() grn.GRN    { return a.operatorGRN }
func (a *Assignment) Epoch() int           { return a.epoch }
func (a *Assignment) CreatedAt() time.Time { return a.createdAt }
func (a *Assignment) UpdatedAt() time.Time { return a.updatedAt }
