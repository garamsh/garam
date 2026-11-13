package policyfeed

import "context"

// Sender writes entries to the policy service.
type Sender interface {
	// Send applies entries at sequence, which is above every sequence
	// the platform has sent for any entity in them. It returns the
	// entries the service refused for their own fault; a fault of the
	// call itself is the error.
	Send(ctx context.Context, sequence int64, entries []Entry) ([]Refusal, error)
	// SendLinks makes the policy service hold each link as it says: the
	// role is written where one is named, and the links of the other
	// roles are removed, so a member holds one link or none. A link
	// already written, or already gone, is not a fault, so a pass that
	// sent a link and was then rolled back sends it again unharmed.
	//
	// A link the service refuses with a code that cannot clear by retry
	// is returned in the refusals, after the member's links the platform
	// may remove are removed, and the links after it are still sent. Any
	// other failure is the error and fails the call.
	SendLinks(ctx context.Context, links []Link) ([]LinkRefusal, error)
}
