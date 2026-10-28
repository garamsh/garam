package api

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/wire"
)

// Statement is one IAM-style rule handed to the authz engine for
// evaluation. Action and Resource entries are wildcard patterns.
// Origin is how the statement reaches the subject; it stays on this
// side of the seam and is what an explanation names a role by.
type Statement struct {
	Sid      string
	Effect   wire.Effect
	Action   []string
	Resource []string
	Origin   Origin
}

// OriginKind is which role a collected statement comes from
// (authz.md §Role-derived statements).
type OriginKind int

const (
	// OriginOrgRole is the subject's org membership role.
	OriginOrgRole OriginKind = iota
	// OriginDirectRole is the roles stored on the subject itself.
	OriginDirectRole
	// OriginGroupRole is the roles of a group the subject belongs to.
	OriginGroupRole
	// OriginProjectRole is the roles of a project the subject belongs
	// to.
	OriginProjectRole
)

// Origin is how a role-derived statement reaches its subject: the kind
// of role, the role names it grants, and the group or project holding
// them — the zero GRN for an org or direct role.
type Origin struct {
	Kind  OriginKind
	Roles []string
	Via   grn.GRN
}

// EvalInput is one authorization question: may Subject perform Action
// on Object, given the Statements the caller collected for it.
// SubjectGroups names the groups Subject belongs to, so the engine can
// find the policies attached to them (ADR-0008). SubjectOrg is the GRN
// of the organization the subject belongs to when the subject is an
// org member; the api server populates it the same way it populates
// SubjectGroups, and the engine refuses the call when it is unset
// (ADR-0038).
type EvalInput struct {
	Subject       grn.GRN
	Object        grn.GRN
	Action        string
	SubjectGroups []grn.GRN
	Statements    []Statement
	SubjectOrg    grn.GRN
}

// BatchInput is EvalInput's question asked of many objects in one
// round trip (ADR-0115): Subject, Action, SubjectGroups and SubjectOrg
// hold for every object, Statements are those that hold for every
// object, and each object carries the statements that reach Subject
// through it alone.
type BatchInput struct {
	Subject       grn.GRN
	Action        string
	SubjectGroups []grn.GRN
	Statements    []Statement
	SubjectOrg    grn.GRN
	Objects       []BatchObject
}

// BatchObject is one object of a BatchInput.
type BatchObject struct {
	Object     grn.GRN
	Statements []Statement
}

// Trace is one statement's part in an explained decision. Policy is
// the persisted policy holding it, and the zero GRN for a statement
// the caller collected. AttachedTo names the subjects of the question —
// the subject or one of its groups — the policy is attached to, and is
// empty for an org-wide policy and a collected statement (ADR-0008).
type Trace struct {
	Sid        string
	Matched    bool
	Effect     wire.Effect
	Policy     grn.GRN
	AttachedTo []grn.GRN
}

// Explanation is the engine's decision and every statement it read.
type Explanation struct {
	Allowed bool
	Traces  []Trace
}

// Evaluator is the authorization seam between the api server and the
// authz engine (ADR-0003). The gRPC client implements it; the api
// package never imports the engine. Explain is asked the question
// Evaluate is, and decides it the same way. EvaluateBatch answers one
// decision per object, in order, each as Evaluate would decide it.
type Evaluator interface {
	Evaluate(ctx context.Context, in EvalInput) (bool, error)
	EvaluateBatch(ctx context.Context, in BatchInput) ([]bool, error)
	Explain(ctx context.Context, in EvalInput) (Explanation, error)
}
