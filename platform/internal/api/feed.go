package api

import (
	"context"
	"errors"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	"github.com/garamsh/garam/platform/internal/user"
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

// awaitOrg holds the answer of a request that has committed a change
// to what the organization's entity carries, its consents, until the
// policy service has applied it (ADR-0159 item 3), and answers as
// awaitMarks does.
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
// §A), and answers as awaitMarks does, naming the operator's
// organization.
func (s *Server) awaitOperator(ctx context.Context, orgGRN, operatorGRN string) error {
	if s.feed == nil {
		return nil
	}
	err := s.feed.AwaitEntity(ctx, policyfeed.Entity{Type: "Iam::Operator", ID: operatorGRN})
	return namesStored(err, orgGRN)
}

// awaitScoped holds the answer of a request that has committed a change
// to the roles of a group or a project, or its end, until the policy
// service has applied the scoped links they mark (ADR-0162), and
// answers as awaitMarks does, naming the organization.
func (s *Server) awaitScoped(ctx context.Context, orgGRN, ownerGRN string) error {
	if s.feed == nil {
		return nil
	}
	return namesStored(s.feed.AwaitScoped(ctx, ownerGRN), orgGRN)
}

// awaitMarks holds the answer of a request that has committed a change
// to what the marks name until the policy service has applied them
// (ADR-0165 item 2). It runs after the request's last write and holds
// no transaction: the change is stored whatever it returns, and an
// error says so (policyfeed.ErrNotApplied, or ErrLinkSetAside where the
// service refused a link for good). That error names the organization,
// so a client that cannot repeat the request, one that created the
// organization or spent an invitation, reads what was stored and goes
// on from it. The marks are the ones the request's own write raised,
// so a request that raised none passes none and is answered at once.
func (s *Server) awaitMarks(ctx context.Context, orgGRN string, marks ...policyfeed.Entity) error {
	if s.feed == nil {
		return nil
	}
	return namesStored(s.feed.AwaitMarks(ctx, marks), orgGRN)
}

// recording returns ctx with a collection the request's delete fills
// with the marks its own transaction raised, for a write that cascades
// to marks no handler can name (ADR-0165 item 3). Without a feed it
// returns ctx and nil, and awaitRaised then waits for nothing.
func (s *Server) recording(ctx context.Context) (context.Context, *policyfeed.Raised) {
	if s.feed == nil {
		return ctx, nil
	}
	return policyfeed.WithRaised(ctx)
}

// awaitRaised holds the answer until the policy service has applied
// the marks the request's delete raised, and answers as awaitMarks
// does.
func (s *Server) awaitRaised(ctx context.Context, raised *policyfeed.Raised, orgGRN string) error {
	if raised == nil {
		return nil
	}
	return s.awaitMarks(ctx, orgGRN, raised.Entities()...)
}

// roleLinkMark is the mark of a member's role link in an organization,
// whose id is the two GRNs (ADR-0159 item 4).
func roleLinkMark(orgGRN, userGRN string) policyfeed.Entity {
	return policyfeed.Entity{Type: policyfeed.RoleLinkType, ID: orgGRN + " " + userGRN}
}

// scopedLinkMark is the mark of the scoped links of a group or a
// project, whose id is its GRN (ADR-0162 item 2).
func scopedLinkMark(ownerGRN string) policyfeed.Entity {
	return policyfeed.Entity{Type: policyfeed.ScopedRoleLinkType, ID: ownerGRN}
}

// memberMark is the mark a change to a group's or a project's members
// raises for the member: its user's or its agent's entity, which
// carries the group as a parent. A member the policy service holds no
// entity for raises none (policy_outbox_track).
func memberMark(member grn.GRN) []policyfeed.Entity {
	if member.Service != "iam" {
		return nil
	}
	switch member.ServiceType {
	case user.ServiceType:
		return []policyfeed.Entity{{Type: policyfeed.UserType, ID: member.String()}}
	case agent.ServiceType:
		return []policyfeed.Entity{{Type: policyfeed.AgentType, ID: member.String()}}
	}
	return nil
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
