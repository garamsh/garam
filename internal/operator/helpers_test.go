package operator_test

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

func operatorGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, operator.ServiceType, id)
}

func agentGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, "agent", id)
}

// clock is a time a test holds still and moves itself.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }
