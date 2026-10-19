package workload

import "context"

// Activator is garam's activateAgent, called with the supervisor's own
// token under execution-fence.v2 (ADR-0082, ADR-0100).
//
// An error of KindUnavailable leaves the outcome unknown and is retried
// under the same request; KindFailedPrecondition is garam refusing the
// request as superseded or reused; every other error is garam refusing
// the caller's authority.
type Activator interface {
	Activate(ctx context.Context, req ActivationRequest) (*Activation, error)
}

// ActivationRequest is what the supervisor asks garam to activate.
type ActivationRequest struct {
	RequestID  string
	Epoch      string
	Generation string
	// Replaces is the agent's most recent activation, empty before the
	// first.
	Replaces string
	// Token is the agent token the supervisor minted for garam: the
	// assignee forwards its own (ADR-0100). garam
	// does not count it in what makes a retry the same request, so a
	// retry carries the token current then.
	Token string
}

// Activation is garam's answer.
type Activation struct {
	ID           string
	Token        string
	TokenVersion int
	GRN          string
	Epoch        string
	Generation   string
	// Replayed is an answer to a request garam had already activated.
	Replayed bool
}
