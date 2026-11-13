package api

import (
	"context"

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
// says so (policyfeed.ErrNotApplied).
func (s *Server) awaitLinks(ctx context.Context, orgGRN, userGRN string) error {
	if s.feed == nil {
		return nil
	}
	return s.feed.Await(ctx, orgGRN, userGRN)
}
