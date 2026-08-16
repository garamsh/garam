// Package agent owns the Agent aggregate and its assignment to an
// operator (ADR-0012).
//
// An agent is an autonomous actor an operator provisions inside a
// cluster garam never dials. garam mints its GRN and keeps two facts
// about it that no one else reports: what the operator says about the
// pod, and what the agent says about itself. An agent is never an
// organization member (ADR-0014), so nothing here reaches the user
// domain.
package agent

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

const (
	// ServiceType is the canonical GRN service-type for agents.
	ServiceType = "agent"
	// ServiceTypeOperator is the canonical GRN service-type for the
	// operators agents are assigned to (ADR-0012). An operator is
	// registered nowhere yet; an assignment addresses one by GRN.
	ServiceTypeOperator = "operator"
)

// Domain sentinel errors.
var (
	ErrAgentNotFound      = errorx.New(errorx.KindNotFound, "agent not found")
	ErrAgentAlreadyExists = errorx.New(errorx.KindAlreadyExists, "agent already exists")
	ErrAssignmentNotFound = errorx.New(errorx.KindNotFound, "agent assignment not found")
	// ErrStaleEpoch is returned when a state report does not carry the
	// epoch the agent is currently assigned at. An agent with no
	// assignment has no current epoch, so every report about it is
	// stale.
	ErrStaleEpoch = errorx.New(errorx.KindFailedPrecondition, "agent assignment epoch is stale")
)

// ProvisioningState is what the operator reports about the agent's pod
// (ADR-0012). It never carries what the agent reports about itself.
type ProvisioningState string

const (
	// ProvisioningStatePending is the state of an agent no operator has
	// reported on yet.
	ProvisioningStatePending     ProvisioningState = "pending"
	ProvisioningStateProvisioned ProvisioningState = "provisioned"
	ProvisioningStateReady       ProvisioningState = "ready"
	ProvisioningStateFailed      ProvisioningState = "failed"
)

// ParseProvisioningState parses a ProvisioningState from its stored
// form.
func ParseProvisioningState(s string) (ProvisioningState, error) {
	switch p := ProvisioningState(s); p {
	case ProvisioningStatePending, ProvisioningStateProvisioned, ProvisioningStateReady, ProvisioningStateFailed:
		return p, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "agent: unknown provisioning state")
}

// RuntimeState is what the agent reports about itself (ADR-0012). It
// never carries what the operator reports about the pod.
type RuntimeState string

const (
	// RuntimeStateUnknown is the state of an agent that has not
	// reported since it was created.
	RuntimeStateUnknown      RuntimeState = "unknown"
	RuntimeStateConnected    RuntimeState = "connected"
	RuntimeStateDisconnected RuntimeState = "disconnected"
)

// ParseRuntimeState parses a RuntimeState from its stored form.
func ParseRuntimeState(s string) (RuntimeState, error) {
	switch r := RuntimeState(s); r {
	case RuntimeStateUnknown, RuntimeStateConnected, RuntimeStateDisconnected:
		return r, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "agent: unknown runtime state")
}

// Agent is an autonomous actor owned by an organization. Which operator
// runs it is an Assignment, not part of the Agent.
type Agent struct {
	grn          grn.GRN
	orgGRN       grn.GRN
	provisioning ProvisioningState
	runtime      RuntimeState
	createdAt    time.Time
	updatedAt    time.Time
}

// New constructs an Agent owned by orgGRN, before an operator has
// provisioned it and before it has run. It fails unless g names an
// agent of that organization: ADR-0014 rests tenancy on the
// organization segment of the GRN a certificate carries.
func New(g, orgGRN grn.GRN) (*Agent, error) {
	if err := validateOwnership(g, orgGRN); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &Agent{
		grn:          g,
		orgGRN:       orgGRN,
		provisioning: ProvisioningStatePending,
		runtime:      RuntimeStateUnknown,
		createdAt:    now,
		updatedAt:    now,
	}, nil
}

// validateOwnership returns nil if g names an agent of the organization
// orgGRN identifies.
func validateOwnership(g, orgGRN grn.GRN) error {
	switch {
	case g.ServiceType != ServiceType:
		return errorx.New(errorx.KindInvalidArgument, "agent: grn does not name an agent")
	case g.Organization != orgGRN.Identifier:
		return errorx.New(errorx.KindInvalidArgument, "agent: grn belongs to another organization")
	}
	return nil
}

func (a *Agent) GRN() grn.GRN                         { return a.grn }
func (a *Agent) Organization() grn.GRN                { return a.orgGRN }
func (a *Agent) ProvisioningState() ProvisioningState { return a.provisioning }
func (a *Agent) RuntimeState() RuntimeState           { return a.runtime }
func (a *Agent) CreatedAt() time.Time                 { return a.createdAt }
func (a *Agent) UpdatedAt() time.Time                 { return a.updatedAt }

// NewGRN mints the GRN of an agent owned by orgGRN from eight random
// bytes. Agents belong to the org, not to a project (ADR-0012), and the
// identifier never changes.
func NewGRN(orgGRN grn.GRN) (grn.GRN, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return grn.GRN{}, errorx.Wrap(err, "agent id")
	}
	return grn.New(orgGRN.Identifier, grn.DefaultProject, ServiceType, hex.EncodeToString(b)), nil
}
