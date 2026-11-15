// Package platform is what cowork asks garam's platform: who presented
// a credential and which organizations it has (ADR-0141 §12), and what
// that caller may take on the organization or on one of its pages,
// which the policy service answers (ADR-0146 §8). cowork keeps no
// access list of its own.
package platform

import (
	"context"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// The cowork actions the policy service answers for (ADR-0127). Each
// is asked on the organization, for its page tree, or on one page.
const (
	ActionPageRead  = "cowork:page.read"
	ActionPageWrite = "cowork:page.write"
	// ActionArtifactRead is asked of an artifact a message embeds, as
	// the artifact package asks it of one it shows.
	ActionArtifactRead = "cowork:artifact.read"
)

// Credential is what a caller presented, passed on as presented: the
// platform's session cookie, or the bearer a CLI session holds. A
// request carrying a bearer is read by it alone, as the platform reads
// one (ADR-0120).
type Credential struct {
	Session string
	Bearer  string
}

// IsZero reports whether the caller presented nothing.
func (c Credential) IsZero() bool { return c.Session == "" && c.Bearer == "" }

// Caller is who the platform says a credential belongs to. It is asked
// with the same credential, so every decision is the caller's own.
type Caller struct {
	GRN        grn.GRN
	Credential Credential
}

// The kinds of resource cowork asks about.
const (
	KindPage     = "cowork-page"
	KindChannel  = "cowork-channel"
	KindTask     = "cowork-task"
	KindArtifact = "cowork-artifact"
)

// Check is one action to decide: on the organization where Kind is
// empty, and on the resource of that kind named ID otherwise. A check
// says nothing of Probe unless it is a question cowork asks to decide
// what to show, which is an attempt otherwise (ADR-0148 item 2).
type Check struct {
	Action string
	Kind   string
	ID     string
	Probe  bool
}

// AsProbe is the check stated as a probe: a question that filters a
// list or decides a control, not a request a person or an agent made.
func (c Check) AsProbe() Check {
	c.Probe = true
	return c
}

// OnPage is a check of action on one page.
func OnPage(action, id string) Check { return Check{Action: action, Kind: KindPage, ID: id} }

// OnChannel is a check of action on one channel, a direct message
// included.
func OnChannel(action, id string) Check { return Check{Action: action, Kind: KindChannel, ID: id} }

// OnTask is a check of action on one task.
func OnTask(action, id string) Check { return Check{Action: action, Kind: KindTask, ID: id} }

// Identity answers for the platform. Identify answers unauthenticated
// for a credential it does not accept, and either method answers
// unavailable when the platform cannot be reached: cowork then refuses
// rather than guesses.
type Identity interface {
	Identify(ctx context.Context, cred Credential) (Caller, error)
	// Organizations answers the organizations the caller belongs to:
	// the workspaces it has.
	Organizations(ctx context.Context, caller Caller) ([]Organization, error)
}

// Platform is what cowork asks to serve a request: Identity, and the
// decisions of the policy service. Allowed answers one decision per
// check, in order, and permission denied for a caller who is not a
// member of org, or unavailable when the service cannot be reached.
type Platform interface {
	Identity
	Allowed(ctx context.Context, caller Caller, org string, checks []Check) ([]bool, error)
}

// Organization is one the caller belongs to: its identifier, the last
// segment of its GRN, and what it is called.
type Organization struct {
	ID   string
	Name string
}

// Nameable reports whether id could identify a resource of GRN type
// typ ("page", "channel", "task" or "artifact") in org. An id no GRN
// can name cannot identify one, so a caller answers not found for it
// before it asks for a decision. An organization that is no GRN is
// reported nameable, and left to the decision's own refusal.
func Nameable(org, typ, id string) bool {
	return grn.Org(org).Validate() != nil || grn.NewIn(org, "cowork", typ, id).Validate() == nil
}

// ErrNotAllowed is a check that was denied.
var ErrNotAllowed = errorx.New(errorx.KindPermissionDenied, "not allowed")

// Require answers nil when every check is allowed, and ErrNotAllowed
// when any is denied.
func Require(ctx context.Context, p Platform, caller Caller, org string, checks ...Check) error {
	allowed, err := p.Allowed(ctx, caller, org, checks)
	if err != nil {
		return err
	}
	for _, ok := range allowed {
		if !ok {
			return ErrNotAllowed
		}
	}
	return nil
}
