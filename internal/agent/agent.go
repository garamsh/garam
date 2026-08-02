// Package agent owns the Agent aggregate.
//
// An Agent is an autonomous identity that acts on behalf of a User or
// an Organization. Like Users, Agents have their own roles and
// policies. Agent org membership is not modeled yet: org.Membership
// ties a user to an Organization.
package agent

import (
	"time"

	"github.com/garamsh/gaon/internal/grn"
)

// ServiceType is the canonical GRN service-type for agents.
const ServiceType = "agent"

// Agent is an autonomous identity.
type Agent struct {
	grn       grn.GRN
	name      string
	roles     []string
	policies  []string
	createdAt time.Time
	updatedAt time.Time
}

// New constructs an Agent.
func New(g grn.GRN, name string) *Agent {
	now := time.Now().UTC()
	return &Agent{grn: g, name: name, createdAt: now, updatedAt: now}
}

func (a *Agent) GRN() grn.GRN       { return a.grn }
func (a *Agent) Name() string       { return a.name }
func (a *Agent) Roles() []string    { return a.roles }
func (a *Agent) Policies() []string { return a.policies }
