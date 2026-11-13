// Package policyfeed feeds the policy service from the platform's
// dirty set of entities (ADR-0140): it switches the feed on and off,
// and delivers what the owners' tables say each marked entity is. What
// the feed holds, and the triggers that fill it, are migrations 000067
// and 000068's.
package policyfeed

import "context"

// Switch turns the feed on and off.
type Switch interface {
	// Start enables the feed and marks every live entity, in one
	// transaction that waits out the writers of the source tables. It
	// returns how many entities the outbox then holds.
	Start(ctx context.Context) (marked int64, err error)
	// Stop disables the feed and empties the outbox. It returns how
	// many marks it removed.
	Stop(ctx context.Context) (removed int64, err error)
	// Enabled reports whether the feed is on.
	Enabled(ctx context.Context) (bool, error)
}

// Entity names an entity of the policy service's schema.
type Entity struct {
	Type string
	ID   string
}

// State is what the owners' tables say an entity is: where it sits and
// what it belongs to, and nothing else (ADR-0138 item 6).
type State struct {
	// Org is the organization an entity of a resource type sits in.
	Org *Entity
	// Members is the OrgMembers an Org names as its own.
	Members *Entity
	Parents []Entity
	// Consents are the organizations toward which an Org holds a
	// consent half that stands (ADR-0147 item 2).
	Consents []Entity
}

// RoleLinkType is the mark kind of a member's role link (ADR-0146
// item 4). Its id is the organization's GRN and the member's, with a
// space between them, so a link's mark is told from an entity's by its
// type alone.
const RoleLinkType = "Iam::RoleLink"

// Link is the role link the platform writes for one membership. It is
// written with no writer, as the caller `platform` (ADR-0146
// §Security), and is named by the pair, so a role that changes is the
// replacement of the link that held the old one.
type Link struct {
	// Org and User are the GRNs of the organization and the member.
	Org, User string
	// Role is the role the membership holds, and empty where it holds
	// none, which is the link's removal.
	Role string
}

// Entry is one entity a pass delivers.
type Entry struct {
	Entity Entity
	// State is nil where the owners hold no such entity, which is a
	// delete.
	State *State
}

// Refusal is an entry the policy service refused for its own fault.
type Refusal struct {
	Entity Entity
	Reason string
}

// LinkRefusal is a link the policy service refused with a code that
// retrying cannot clear. The link is set aside: its mark stays in the
// outbox, recorded with the code and the reason, and no pass reads it
// again until a write of the membership marks it anew.
type LinkRefusal struct {
	Link Link
	// Code is the gRPC code the service answered, as its name.
	Code string
	// Reason is the call that was refused and the service's message.
	Reason string
}
