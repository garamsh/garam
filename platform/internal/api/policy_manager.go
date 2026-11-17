package api

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/listfilter"
	"github.com/garamsh/garam/platform/internal/wire"
)

// Policy is an org's versioned rule set as the engine reports it.
type Policy struct {
	GRN            grn.GRN
	OrgGRN         grn.GRN
	Name           string
	Status         wire.PolicyStatus
	DefaultVersion int
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// PolicyManager is the policy seam between the api server and the
// authz engine (ADR-0003). The gRPC client implements it; the api
// package never imports the engine. It only reads what the engine
// still holds: the policy service stores policies, and the policy
// endpoints are the relay's (ADR-0169, ADR-0174).
type PolicyManager interface {
	ListPolicies(ctx context.Context, orgGRN grn.GRN) ([]*Policy, error)
	SearchPolicies(ctx context.Context, orgGRN grn.GRN, n listfilter.Needle) ([]*Policy, error)
}
