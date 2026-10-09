package operator

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// ContractVersion names the delegation and operation-authority contract
// (ADR-0079). Every route of it requires the version in the
// Garam-Contract-Version header and answers with it.
const ContractVersion = "operation-authority.v1"

// TokenAuthorityContractVersion is the same contract for a request a
// token authenticated, under which a controller proof takes the
// controller's token rather than its certificate (ADR-0100).
const TokenAuthorityContractVersion = "operation-authority.v2"

// ContractVersionHeader carries ContractVersion on requests and
// answers.
const ContractVersionHeader = "Garam-Contract-Version"

// ErrUnsupportedContract refuses a request carrying no version, or one
// this release does not serve, and names the one it does.
var ErrUnsupportedContract = errorx.New(errorx.KindInvalidArgument,
	"unsupported "+ContractVersionHeader+": this route serves "+ContractVersion)

// ErrUnsupportedTokenAuthorityContract is that refusal for a request a
// token authenticated, which is served under the token's value
// (ADR-0100).
var ErrUnsupportedTokenAuthorityContract = errorx.New(errorx.KindInvalidArgument,
	"unsupported "+ContractVersionHeader+": a request presenting a token is served under "+TokenAuthorityContractVersion)

// Bounds on a grant (#1160). They are constants, not configuration: a
// deployment tuning them would be widening what an administrator can
// delegate, which is the decision the bound exists to hold.
const (
	maxGrantLifetime = 30 * 24 * time.Hour
	maxControllers   = 32
)

// Operation is what a grant lets its hosted operator do. Each value is
// also the action a member must hold to hand the operator a request for
// it; internal/org owns those constants and this package does not
// import it, so the strings are repeated here on purpose.
type Operation string

const (
	// OperationAgentCreate registers an agent from an immutable template
	// or profile the Operator already allows.
	OperationAgentCreate Operation = "agent:create"
	// OperationAgentConfigure changes an agent's execution settings: a
	// high-trust action no member role grants.
	OperationAgentConfigure Operation = "agent:configure"
	// OperationAgentActivate bounds the hosted operator's own lifecycle
	// authority (ADR-0078). No member mints it.
	OperationAgentActivate Operation = "agent:activate"
	// OperationAgentRecover replaces an agent's credential lineage: a
	// high-trust action no member role grants, minted for one exact
	// recovery request (ADR-0078).
	OperationAgentRecover Operation = "agent:recover"
	// OperationTemplateRead reads the Operator's published agent
	// templates (#1170).
	OperationTemplateRead Operation = "agent-template:read"
	// OperationTemplatePublish publishes the next immutable version of an
	// Operator template. It edits no agent created from an earlier one.
	OperationTemplatePublish Operation = "agent-template:publish"
	// OperationProfileRead reads the Operator's published execution
	// profiles. Publishing one stays with the deployment.
	OperationProfileRead Operation = "execution-profile:read"
	// OperationExecutionRead reads an agent's pending, rendered and
	// runtime-reported execution. Reading the agent's identity, which
	// agent:read is, grants none of it.
	OperationExecutionRead Operation = "agent:execution-read"
	// OperationAgentCutover moves a legacy agent's source to the hosted
	// operator under the agent's existing GRN: a high-trust action no
	// member role grants. Each reference binds the request target of the
	// one cutover route it is carried to, so it authorizes one stage
	// (ADR-0086).
	OperationAgentCutover Operation = "agent:cutover"
)

// ParseOperation parses an operation a grant may carry.
func ParseOperation(s string) (Operation, error) {
	switch o := Operation(s); o {
	case OperationAgentCreate, OperationAgentConfigure, OperationAgentActivate, OperationAgentRecover,
		OperationTemplateRead, OperationTemplatePublish, OperationProfileRead, OperationExecutionRead,
		OperationAgentCutover:
		return o, nil
	}
	return "", errorx.New(errorx.KindInvalidArgument, "unknown operation "+s)
}

// IsMintable reports whether a member can hand the operation to an
// operator request by request.
func (o Operation) IsMintable() bool {
	return o == OperationAgentCreate || o.TargetsAgent() || o.TargetsOrg()
}

// TargetsAgent reports whether the operation acts on an existing agent:
// it is authorized on that agent and bound to where the agent runs when
// it is minted.
func (o Operation) TargetsAgent() bool {
	return o == OperationAgentConfigure || o == OperationAgentRecover || o == OperationExecutionRead ||
		o == OperationAgentCutover
}

// TargetsOrg reports whether the operation acts on what the Operator
// holds for the whole organization: it is authorized on the
// organization and names no controller.
func (o Operation) TargetsOrg() bool {
	return o == OperationTemplateRead || o == OperationTemplatePublish || o == OperationProfileRead
}

// BindsRequestTarget reports whether an authority for the operation
// binds the exact request target the Operator receives. Every
// operation that does was added with that binding; the earlier ones
// bind only their body and take none.
func (o Operation) BindsRequestTarget() bool {
	return o.TargetsOrg() || o == OperationExecutionRead || o == OperationAgentCutover
}

// RequestID is a caller's stable identifier for one write, which makes
// a retry of it recognisable.
type RequestID string

var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,128}$`)

// ParseRequestID parses a request identifier.
func ParseRequestID(s string) (RequestID, error) {
	if !requestIDPattern.MatchString(s) {
		return "", errorx.New(errorx.KindInvalidArgument, "request_id must match [A-Za-z0-9._~-]{1,128}")
	}
	return RequestID(s), nil
}

// GrantID identifies one immutable grant.
type GrantID string

// Grant is an organization's delegation to a hosted operator: which
// controllers it may act on, which operations, and until when. It never
// changes once written; replacing it ends it and writes another.
type Grant struct {
	ID          GrantID
	Org         grn.GRN
	Operator    grn.GRN
	Controllers []grn.GRN
	Operations  []Operation
	ExpiresAt   time.Time
	RequestID   RequestID
	// Replaces is the grant the write expected to be current and ended,
	// nil where it found none.
	Replaces  *GrantID
	GrantedBy grn.GRN
	GrantedAt time.Time
	// EndedAt is when a replacement or a withdrawal ended the grant,
	// nil while it is current.
	EndedAt *time.Time
}

// Covers reports whether the grant, at now, lets its operator perform
// op for controller. An operation on the organization names no
// controller, so controller is not read for one.
func (g *Grant) Covers(op Operation, controller grn.GRN, now time.Time) bool {
	return g.EndedAt == nil && now.Before(g.ExpiresAt) && slices.Contains(g.Operations, op) &&
		(op.TargetsOrg() || slices.Contains(g.Controllers, controller))
}

// GrantTerms is what a grant write asks for, parsed.
type GrantTerms struct {
	Controllers []grn.GRN
	Operations  []Operation
	ExpiresAt   time.Time
}

// ParseGrantTerms parses and bounds what a grant write asks for.
// operator is the hosted operator the grant is for, which may not also
// be one of its controllers: the two hold distinct credentials
// (ADR-0077).
func ParseGrantTerms(org, operator grn.GRN, controllers, operations []string, expiresAt, now time.Time) (GrantTerms, error) {
	if len(controllers) == 0 || len(controllers) > maxControllers {
		return GrantTerms{}, errorx.New(errorx.KindInvalidArgument, "a grant names 1 to 32 controllers")
	}
	// Stored at the database's microsecond precision, so the expiry a
	// write answers is the one a replay of it is compared against.
	terms := GrantTerms{ExpiresAt: expiresAt.UTC().Truncate(time.Microsecond)}
	for _, c := range controllers {
		g, err := grn.Parse(c)
		if err != nil {
			return GrantTerms{}, errorx.Wrap(err, "controller", errorx.KindInvalidArgument)
		}
		if err := validateOwnership(g, org); err != nil {
			return GrantTerms{}, err
		}
		if g == operator {
			return GrantTerms{}, errorx.New(errorx.KindInvalidArgument, "the hosted operator cannot be its own controller")
		}
		if slices.Contains(terms.Controllers, g) {
			return GrantTerms{}, errorx.New(errorx.KindInvalidArgument, "controller named twice")
		}
		terms.Controllers = append(terms.Controllers, g)
	}
	if len(operations) == 0 {
		return GrantTerms{}, errorx.New(errorx.KindInvalidArgument, "a grant carries at least one operation")
	}
	for _, s := range operations {
		op, err := ParseOperation(s)
		if err != nil {
			return GrantTerms{}, err
		}
		if slices.Contains(terms.Operations, op) {
			return GrantTerms{}, errorx.New(errorx.KindInvalidArgument, "operation named twice")
		}
		terms.Operations = append(terms.Operations, op)
	}
	if !terms.ExpiresAt.After(now) || terms.ExpiresAt.After(now.Add(maxGrantLifetime)) {
		return GrantTerms{}, errorx.New(errorx.KindInvalidArgument, "expires_at must be within the next 30 days")
	}
	slices.SortFunc(terms.Controllers, func(a, b grn.GRN) int { return strings.Compare(a.String(), b.String()) })
	slices.Sort(terms.Operations)
	return terms, nil
}

// Records reports whether the grant is what w would write: the same
// terms, by the same actor, replacing the same grant. Only then is w a
// replay of the write that created it.
func (g *Grant) Records(w GrantWrite) bool {
	sameReplaced := (g.Replaces == nil && w.Expected == nil) ||
		(g.Replaces != nil && w.Expected != nil && *g.Replaces == *w.Expected)
	return sameReplaced && g.GrantedBy == w.Actor && g.ExpiresAt.Equal(w.Terms.ExpiresAt) &&
		slices.Equal(g.Controllers, w.Terms.Controllers) && slices.Equal(g.Operations, w.Terms.Operations)
}

// GrantWrite is one request to create or replace a hosted operator's
// grant. Expected is the grant the caller saw as current, nil where it
// saw none: a write made from a view that has since moved is refused
// rather than overwriting what moved it.
type GrantWrite struct {
	Org       grn.GRN
	Operator  grn.GRN
	RequestID RequestID
	Expected  *GrantID
	Terms     GrantTerms
	Actor     grn.GRN
	Now       time.Time
}

// Grant sentinels.
var (
	ErrGrantNotFound = errorx.New(errorx.KindNotFound, "the operator holds no current delegation")
	// ErrGrantStale refuses a write whose expected grant is not the
	// current one: another write or a withdrawal moved it first.
	ErrGrantStale = errorx.New(errorx.KindAborted, "the delegation changed since it was read: read it again")
	// ErrGrantRequestConflict refuses a request identifier already used
	// for a different write.
	ErrGrantRequestConflict = errorx.New(errorx.KindAlreadyExists, "request_id was used for a different delegation write")
	// ErrGrantEnded refuses a replayed write whose grant has since been
	// ended. Recreating it would undo the withdrawal.
	ErrGrantEnded = errorx.New(errorx.KindFailedPrecondition, "the delegation this request wrote has been ended")
)
