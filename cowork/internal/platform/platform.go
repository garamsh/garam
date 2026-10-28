// Package platform is what cowork asks garam's platform: who presented
// a credential, and whether that caller may take an action on the
// organization or on one of its pages (ADR-0127). cowork keeps no
// access list of its own.
package platform

import (
	"context"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// The cowork actions the platform answers for (ADR-0127). Each is
// asked on the organization, for its page tree, or on one page.
const (
	ActionPageRead  = "cowork:page-read"
	ActionPageWrite = "cowork:page-write"
	// ActionArtifactRead is asked of an artifact a message embeds, as
	// the artifact package asks it of one it shows.
	ActionArtifactRead = "cowork:artifact-read"
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

// The kinds of resource cowork asks about, as the platform's
// `PermissionResourceType` names them.
const (
	KindPage     = "cowork-page"
	KindChannel  = "cowork-channel"
	KindTask     = "cowork-task"
	KindArtifact = "cowork-artifact"
)

// Check is one action to decide: on the organization where Kind is
// empty, and on the resource of that kind named ID otherwise.
type Check struct {
	Action string
	Kind   string
	ID     string
}

// OnPage is a check of action on one page.
func OnPage(action, id string) Check { return Check{Action: action, Kind: KindPage, ID: id} }

// OnChannel is a check of action on one channel, a direct message
// included.
func OnChannel(action, id string) Check { return Check{Action: action, Kind: KindChannel, ID: id} }

// OnTask is a check of action on one task.
func OnTask(action, id string) Check { return Check{Action: action, Kind: KindTask, ID: id} }

// Platform answers for the platform. Identify answers unauthenticated
// for a credential it does not accept. Allowed answers one decision per
// check, in order, and permission denied for a caller who is not a
// member of org. Either answers unavailable when the platform cannot be
// reached: cowork then refuses rather than guesses.
type Platform interface {
	Identify(ctx context.Context, cred Credential) (Caller, error)
	Allowed(ctx context.Context, caller Caller, org string, checks []Check) ([]bool, error)
	// Organizations answers the organizations the caller belongs to:
	// the workspaces it has.
	Organizations(ctx context.Context, caller Caller) ([]Organization, error)
}

// Organization is one the caller belongs to: its identifier, the last
// segment of its GRN, and what it is called.
type Organization struct {
	ID   string
	Name string
}

// ErrNotAllowed is a check the platform denied.
var ErrNotAllowed = errorx.New(errorx.KindPermissionDenied, "not allowed")

// Require answers nil when the platform allows every check, and
// ErrNotAllowed when it denies any.
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
