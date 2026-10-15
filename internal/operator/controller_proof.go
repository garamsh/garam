package operator

import (
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ControllerProof is a positive proof, for one lifecycle decision by a
// hosted operator, that a controller of its organization currently
// holds the authority the decision needs (ADR-0078). It proves the
// state its reads found and is no transaction with the caller's
// store; the next decision asks again.
type ControllerProof struct {
	Controller grn.GRN
	Org        grn.GRN
	// Agent is the named agent's current placement on the controller,
	// nil where no agent was named.
	Agent *ProvenAgent
}

// ProvenAgent is an agent currently assigned to the proven controller,
// and that assignment's epoch, which is opaque.
type ProvenAgent struct {
	Agent grn.GRN
	Epoch string
}

// Controller proof sentinels.
var (
	// ErrControllerNotFound is one answer for a controller that is not
	// an operator of the caller's organization, so a caller learns
	// nothing about another organization's operators.
	ErrControllerNotFound = errorx.New(errorx.KindNotFound, "controller not found")
	// ErrControllerNotDelegated refuses a controller the caller's
	// current grant does not cover for agent:activate.
	ErrControllerNotDelegated = errorx.New(errorx.KindPermissionDenied,
		"the delegation does not cover agent:activate for this controller")
	// ErrAgentNotFound is one answer for an agent that is not an agent
	// of the caller's organization or is assigned nowhere.
	ErrAgentNotFound = errorx.New(errorx.KindNotFound, "agent not found")
	// ErrAgentElsewhere refuses an agent assigned to another controller.
	ErrAgentElsewhere = errorx.New(errorx.KindPermissionDenied, "the agent is not assigned to this controller")
)

// ParseController parses the controller a hosted operator names. One
// that is not an operator of audience's organization is
// ErrControllerNotFound, decided before anything is read about it.
func ParseController(audience grn.GRN, s string) (grn.GRN, error) {
	controller, err := grn.Parse(s)
	if err != nil {
		return grn.GRN{}, errorx.Wrap(err, "controller", errorx.KindInvalidArgument)
	}
	if controller.ServiceType != ServiceType || controller.Organization != audience.Organization {
		return grn.GRN{}, ErrControllerNotFound
	}
	return controller, nil
}
