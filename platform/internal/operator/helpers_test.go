package operator_test

import (
	"time"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

func operatorGRN(org, id string) grn.GRN {
	return grn.New(org, operator.ServiceType, id)
}

func agentGRN(org, id string) grn.GRN {
	return grn.New(org, "agent", id)
}

// clock is a time a test holds still and moves itself.
type clock struct{ at time.Time }

func (c *clock) now() time.Time { return c.at }
