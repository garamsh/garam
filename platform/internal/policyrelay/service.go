// Package policyrelay relays an organization's role links, guardrails
// and permits to the policy service, which stores them (ADR-0146
// item 3). The platform keeps none of a body: it names the caller as
// the writer and the service decides, read current, whether the writer
// may (ADR-0169).
package policyrelay

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/org"
)

// Kind is what a policy says, fixed by its first version.
type Kind string

const (
	KindRoleLink  Kind = "role-link"
	KindGuardrail Kind = "guardrail"
	KindPermit    Kind = "permit"
)

// Scope is the template set a role link is drawn from.
type Scope string

const (
	// ScopeOrganization links a principal to the organization.
	ScopeOrganization Scope = "organization"
	// ScopeTeamOrProject links a principal to a team or project.
	ScopeTeamOrProject Scope = "team-or-project"
)

// Status says whether a policy decides anything.
type Status string

const (
	StatusDraft  Status = "draft"
	StatusActive Status = "active"
)

// RoleLink assigns a role to a principal on a resource.
type RoleLink struct {
	Role      org.Role
	Scope     Scope
	Principal grn.GRN
	Resource  grn.GRN
}

// Body is what one version of a policy says. Text is the Cedar text of
// a guardrail or a permit, and Link is a role link's: the one the kind
// names is set.
type Body struct {
	Kind Kind
	Text string
	Link *RoleLink
}

// Validate refuses a body the platform never relays: one that is
// malformed, or a role link the platform derives from a membership
// (#1796 decision 5), whose writer is the membership and no one else.
// The service validates what it is sent again; this keeps a user's
// organization role, and a team's or project's own scoped link, the
// membership's alone, which no stored body can tell the service apart.
func (b Body) Validate() error {
	switch b.Kind {
	case KindGuardrail, KindPermit:
		if b.Link != nil {
			return errorx.New(errorx.KindInvalidArgument, string(b.Kind)+" carries no role link")
		}
		return nil
	case KindRoleLink:
	default:
		return errorx.New(errorx.KindInvalidArgument, "policy kind is not role-link, guardrail or permit")
	}
	l := b.Link
	if l == nil || b.Text != "" {
		return errorx.New(errorx.KindInvalidArgument, "a role link carries a role link and no text")
	}
	if _, err := org.ParseRole(string(l.Role)); err != nil {
		return err
	}
	switch l.Scope {
	case ScopeOrganization:
		if l.Principal.ServiceType == "user" {
			return errorx.New(errorx.KindInvalidArgument,
				"a user's organization role is its membership's and is not written here")
		}
	case ScopeTeamOrProject:
		if l.Principal == l.Resource {
			return errorx.New(errorx.KindInvalidArgument,
				"a team's or project's own scoped link is derived from it and is not written here")
		}
	default:
		return errorx.New(errorx.KindInvalidArgument, "role link scope is not organization or team-or-project")
	}
	return nil
}

// Policy is a stored policy without its versions.
type Policy struct {
	ID             string
	Name           string
	Kind           Kind
	Status         Status
	DefaultVersion int32
}

// Version is one stored version.
type Version struct {
	Number    int32
	Body      Body
	CreatedBy string
}

// Detail is a policy with its versions, oldest first.
type Detail struct {
	Policy   Policy
	Versions []Version
}

// Write names a change to one policy of an organization and who asks
// for it. Writer is the signed-in user's GRN.
type Write struct {
	Writer grn.GRN
	Org    grn.GRN
	ID     string
}

// Service relays writes and reads of an organization's policies. Every
// refusal is the service's, carried as its kind: permission denied for
// a writer who may not, invalid argument for a body it rejects,
// unavailable when it cannot be reached.
type Service interface {
	// Create stores a policy's first version, as a draft.
	Create(ctx context.Context, w Write, name string, body Body) error
	// AppendVersion stores a further version and returns its number.
	AppendVersion(ctx context.Context, w Write, body Body) (int32, error)
	// Activate makes the default version the policy's decision.
	Activate(ctx context.Context, w Write) error
	// RollBack makes an earlier version the default.
	RollBack(ctx context.Context, w Write, version int32) error
	// Delete removes the policy.
	Delete(ctx context.Context, w Write) error
	// Purge removes, as the platform for actor, every policy of org
	// naming one of entities, whoever wrote it, and returns the
	// identifiers removed (ADR-0192 decision 4). It evaluates nothing:
	// the caller has decided the uninstall it follows.
	Purge(ctx context.Context, actor, org grn.GRN, entities []grn.GRN) ([]string, error)
	// Get reads a policy and its versions.
	Get(ctx context.Context, w Write) (Detail, error)
	// List reads the organization's policies.
	List(ctx context.Context, reader grn.GRN, org grn.GRN) ([]Policy, error)
	// ListAudit reads a page of the organization's audit records,
	// newest first, for a reader the service lets read them
	// (`iam:audit.read`). An empty token reads the first page and a
	// size of 0 the service's bound.
	ListAudit(
		ctx context.Context, reader, org grn.GRN, pageSize int32, pageToken string,
	) (AuditPage, error)
}

// NewID names a policy the platform creates. It is 32 hex digits under
// a prefix no reserved identifier begins with (ADR-0161), so a
// writer's policy never lands among the platform's own links.
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errorx.Wrap(err, "generate policy id", errorx.KindInternal)
	}
	return "p-" + hex.EncodeToString(b[:]), nil
}
