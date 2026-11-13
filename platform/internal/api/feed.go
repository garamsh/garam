package api

import (
	"context"
	"errors"

	"github.com/garamsh/garam/platform/internal/policyfeed"
)

// WithFeed makes a request that changes an organization's membership
// answer once the policy service has applied the role links it marked
// (ADR-0146 §Reading one's own write). Without it nothing is waited
// for: the feed is not delivered, and the server answers as it did
// before the feed existed.
func (s *Server) WithFeed(gate policyfeed.Gate) *Server {
	s.feed = gate
	return s
}

// awaitLinks holds the answer of a request that has committed a change
// to org's memberships until the policy service has applied it. user is
// the member the change is about, or empty where it is about all of
// them. It runs after the request's last write and holds no
// transaction: the change is stored whatever it returns, and an error
// says so (policyfeed.ErrNotApplied, or ErrLinkSetAside where the
// service refused the link for good). That error names the
// organization, so a client that cannot repeat the request, one that
// created the organization or spent an invitation, reads what was
// stored and goes on from it.
func (s *Server) awaitLinks(ctx context.Context, orgGRN, userGRN string) error {
	if s.feed == nil {
		return nil
	}
	return namesStored(s.feed.Await(ctx, orgGRN, userGRN), orgGRN)
}

// awaitOrg holds the answer of a request that has committed a change
// to what the organization's entity carries, its consents, until the
// policy service has applied it (ADR-0159 item 3), and answers as
// awaitLinks does.
func (s *Server) awaitOrg(ctx context.Context, orgGRN string) error {
	if s.feed == nil {
		return nil
	}
	err := s.feed.AwaitEntity(ctx, policyfeed.Entity{Type: "Iam::Org", ID: orgGRN})
	return namesStored(err, orgGRN)
}

// awaitOperator holds the answer of a request that has committed a
// change to what an operator's entity carries, the operations its
// delegation holds, until the policy service has applied it (ADR-0146
// §A), and answers as awaitLinks does, naming the operator's
// organization.
func (s *Server) awaitOperator(ctx context.Context, orgGRN, operatorGRN string) error {
	if s.feed == nil {
		return nil
	}
	err := s.feed.AwaitEntity(ctx, policyfeed.Entity{Type: "Iam::Operator", ID: operatorGRN})
	return namesStored(err, orgGRN)
}

// namesStored wraps an error that says the change is stored but not
// applied with the organization it concerns.
func namesStored(err error, orgGRN string) error {
	if errors.Is(err, policyfeed.ErrNotApplied) ||
		errors.Is(err, policyfeed.ErrLinkSetAside) {
		return &storedError{error: err, resource: orgGRN}
	}
	return err
}

// storedError is an error that names what the request stored before it
// failed: the resource is the identifier of the organization.
type storedError struct {
	error
	resource string
}

func (e *storedError) Unwrap() error { return e.error }

// Resource is the identifier writeError puts in the body's resource.
func (e *storedError) Resource() string { return e.resource }
