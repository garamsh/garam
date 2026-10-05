package workload

import "context"

// Activator is garam's activateAgent, called with the supervisor's own
// certificate (ADR-0082).
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
	// CertificatePEM is the agent's leaf as its adapter presented it on
	// the request being forwarded.
	CertificatePEM string
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
