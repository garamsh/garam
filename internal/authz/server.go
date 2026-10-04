package authz

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// GRPCServer serves the policy engine over gRPC (ADR-0003 stage 2).
// It evaluates the union of the caller's role-derived statements and
// the object org's persisted policies (ADR-0007); it matches strings
// only and never imports a domain.
type GRPCServer struct {
	authzv1.UnimplementedAuthzServiceServer
	policies PolicySource
}

// Compile-time check that GRPCServer satisfies the generated service.
var _ authzv1.AuthzServiceServer = (*GRPCServer)(nil)

// NewGRPCServer constructs the authz gRPC service over the org
// statement cache.
func NewGRPCServer(policies PolicySource) *GRPCServer {
	return &GRPCServer{policies: policies}
}

// Check evaluates the applicable statements and returns the final
// decision: explicit Deny wins, otherwise any matching Allow permits.
func (s *GRPCServer) Check(ctx context.Context, req *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	in, err := parseInput(req.GetSubjectGrn(), req.GetObjectGrn(), req.GetAction(), req.GetSubjectGroups(), req.GetSubjectOrg(), req.GetStatements())
	if err != nil {
		return nil, grpcError(err)
	}
	allowed, err := decide(ctx, in, s.policies.Statements)
	if err != nil {
		return nil, grpcError(err)
	}
	return &authzv1.CheckResponse{Allowed: allowed}, nil
}

// CheckCurrent decides as Check does, reading the persisted policies
// from storage rather than the cache. The caller's statements, groups
// and org are taken as sent.
func (s *GRPCServer) CheckCurrent(ctx context.Context, req *authzv1.CheckCurrentRequest) (*authzv1.CheckCurrentResponse, error) {
	in, err := parseInput(req.GetSubjectGrn(), req.GetObjectGrn(), req.GetAction(), req.GetSubjectGroups(), req.GetSubjectOrg(), req.GetStatements())
	if err != nil {
		return nil, grpcError(err)
	}
	allowed, err := decide(ctx, in, s.policies.CurrentStatements)
	if err != nil {
		return nil, grpcError(err)
	}
	return &authzv1.CheckCurrentResponse{Allowed: allowed}, nil
}

// decide scopes the request, reads the persisted statements through
// read and evaluates them with the caller's.
func decide(ctx context.Context, in checkInput, read readStatements) (bool, error) {
	if err := in.checkScope(); err != nil {
		return false, err
	}
	statements, err := applicable(ctx, in, read)
	if err != nil {
		return false, err
	}
	d := NewStatementEvaluator(statements).Evaluate(ctx, Context{
		Object: grnObject{in.object},
		Action: in.action,
	})
	return d == DecisionAllow, nil
}

// Explain evaluates the same input and reports every statement's
// contribution, so a denied request is reproducible after the fact.
func (s *GRPCServer) Explain(ctx context.Context, req *authzv1.ExplainRequest) (*authzv1.ExplainResponse, error) {
	in, err := parseInput(req.GetSubjectGrn(), req.GetObjectGrn(), req.GetAction(), req.GetSubjectGroups(), req.GetSubjectOrg(), req.GetStatements())
	if err != nil {
		return nil, grpcError(err)
	}
	if err := in.checkScope(); err != nil {
		return nil, grpcError(err)
	}
	statements, err := applicable(ctx, in, s.policies.Statements)
	if err != nil {
		return nil, grpcError(err)
	}
	traces := make([]*authzv1.StatementTrace, 0, len(statements))
	denied, allowed := false, false
	for _, st := range statements {
		matched := st.Matches(in.action, in.object.String())
		traces = append(traces, &authzv1.StatementTrace{
			Sid:     st.Sid,
			Matched: matched,
			Effect:  effectToProto(st.Effect),
		})
		if !matched {
			continue
		}
		if st.Effect == wire.EffectDeny {
			denied = true
		} else {
			allowed = true
		}
	}
	return &authzv1.ExplainResponse{Allowed: allowed && !denied, Traces: traces}, nil
}

// serviceTypeOrg is the GRN service type of an organization. The
// engine needs it to scope a request to the org whose policies apply;
// it carries no other domain knowledge (ADR-0007).
const serviceTypeOrg = "org"

// readStatements is one of PolicySource's two reads.
type readStatements func(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]Statement, error)

// applicable unions the persisted policy statements read reports as
// reaching the subject — org-wide, attached to it, or attached to one
// of its groups (ADR-0008) — with the role-derived statements the
// caller supplied (ADR-0007).
func applicable(ctx context.Context, in checkInput, read readStatements) ([]Statement, error) {
	stored, err := read(ctx, orgGRNOf(in.object), in.subjects)
	if err != nil {
		return nil, err
	}
	statements := make([]Statement, 0, len(stored)+len(in.statements))
	statements = append(statements, stored...)
	return append(statements, in.statements...), nil
}

// orgGRNOf returns the GRN of the org that owns the object. Orgs are
// root-owned, so an org's identifier is the organization segment of
// every resource beneath it (ADR-0001).
func orgGRNOf(object grn.GRN) grn.GRN {
	if object.ServiceType == serviceTypeOrg {
		return object
	}
	return grn.New(grn.RootOwner, grn.DefaultProject, serviceTypeOrg, object.Organization)
}

// checkInput is a parsed Check, CheckCurrent or Explain request.
// subjects holds the GRNs a policy attachment may name for this
// request: the subject itself and the groups it belongs to. subjectOrg
// is the GRN of the organization the subject belongs to when the
// subject is an org member (ADR-0038); the empty value carries the
// meaning "no membership was established", which the scope check
// refuses.
type checkInput struct {
	object     grn.GRN
	action     string
	subjects   []grn.GRN
	statements []Statement
	subjectOrg grn.GRN
}

// checkScope enforces the engine's organization boundary (ADR-0038):
// the subject's organization, when given, must match the object's.
// An empty subjectOrg is a refusal rather than a default-deny: the
// engine was not given the relationship it would need to establish
// membership, and a cross-package fixture (#296) added the same
// property as a caller obligation; the engine is where the obligation
// now lives.
func (in checkInput) checkScope() error {
	if in.subjectOrg.Identifier == "" {
		return errorx.New(errorx.KindPermissionDenied, "subject_org is required")
	}
	if in.subjectOrg != orgGRNOf(in.object) {
		return errorx.New(errorx.KindPermissionDenied, "subject is not a member of the object's organization")
	}
	return nil
}

// parseInput validates the wire request once at the transport
// boundary; downstream code trusts the parsed values.
func parseInput(subjectGRN, objectGRN, action string, subjectGroups []string, subjectOrg string, stmts []*authzv1.Statement) (checkInput, error) {
	subject, err := grn.Parse(subjectGRN)
	if err != nil {
		return checkInput{}, errorx.Wrap(err, "subject_grn")
	}
	subjects := make([]grn.GRN, 0, len(subjectGroups)+1)
	subjects = append(subjects, subject)
	for _, raw := range subjectGroups {
		g, err := grn.Parse(raw)
		if err != nil {
			return checkInput{}, errorx.Wrap(err, "subject_groups")
		}
		subjects = append(subjects, g)
	}
	object, err := grn.Parse(objectGRN)
	if err != nil {
		return checkInput{}, errorx.Wrap(err, "object_grn")
	}
	var org grn.GRN
	if subjectOrg != "" {
		org, err = grn.Parse(subjectOrg)
		if err != nil {
			return checkInput{}, errorx.Wrap(err, "subject_org")
		}
	}
	if action == "" {
		return checkInput{}, errorx.New(errorx.KindInvalidArgument, "action is required")
	}
	statements := make([]Statement, 0, len(stmts))
	for _, p := range stmts {
		effect, err := wire.EffectFromProto(p.GetEffect())
		if err != nil {
			return checkInput{}, errorx.Wrap(err, "statement "+p.GetSid())
		}
		if len(p.GetAction()) == 0 || len(p.GetResource()) == 0 {
			return checkInput{}, errorx.New(errorx.KindInvalidArgument, "statement "+p.GetSid()+": action and resource patterns are required")
		}
		statements = append(statements, Statement{
			Sid:      p.GetSid(),
			Effect:   effect,
			Action:   p.GetAction(),
			Resource: p.GetResource(),
		})
	}
	return checkInput{object: object, action: action, subjects: subjects, statements: statements, subjectOrg: org}, nil
}

// effectToProto renders a stored Effect for an Explain trace. It
// coerces rather than refuses: the trace reports what was evaluated,
// and a stored statement passed the parse boundary to be stored. It
// agrees with wire.EffectToProto on both constants; the agreement test
// in server_internal_test.go is what keeps that true.
func effectToProto(e wire.Effect) authzv1.Effect {
	if e == wire.EffectAllow {
		return authzv1.Effect_EFFECT_ALLOW
	}
	return authzv1.Effect_EFFECT_DENY
}

// grpcError renders an errorx error as a gRPC status. Kinds are
// gRPC-aligned, so the kind carries over the status code by name
// (ADR-0003); the mapping point is this transport.
//
// The message is what a caller may be told (wire.MessageOf) rather
// than the chain. A status carries text and no types, and it is the
// types that say whether this project wrote a message — so the peer
// receiving one cannot tell this project's sentence from a driver's,
// and the decision is taken here, on the side that still holds the
// chain.
func grpcError(err error) error {
	return disclosed{
		status: status.New(codeOf(errorx.KindOf(err)), wire.MessageOf(err)),
		cause:  err,
	}
}

// disclosed is what leaves the engine: the status a peer reads, over
// the chain this process keeps. grpc-go sends the status; anything on
// this side that reads the error — the logging interceptor is the one —
// reads the chain, so a fault of this process is still diagnosable from
// its own logs (runtime-safety.md §Errors reach a handler).
type disclosed struct {
	status *status.Status
	cause  error
}

func (d disclosed) Error() string { return d.cause.Error() }

func (d disclosed) Unwrap() error { return d.cause }

func (d disclosed) GRPCStatus() *status.Status { return d.status }

// codeOf maps error kinds to gRPC status codes by name.
func codeOf(k errorx.Kind) codes.Code {
	switch k {
	case errorx.KindCanceled:
		return codes.Canceled
	case errorx.KindInvalidArgument:
		return codes.InvalidArgument
	case errorx.KindDeadlineExceeded:
		return codes.DeadlineExceeded
	case errorx.KindNotFound:
		return codes.NotFound
	case errorx.KindAlreadyExists:
		return codes.AlreadyExists
	case errorx.KindPermissionDenied:
		return codes.PermissionDenied
	case errorx.KindResourceExhausted:
		return codes.ResourceExhausted
	case errorx.KindFailedPrecondition:
		return codes.FailedPrecondition
	case errorx.KindAborted:
		return codes.Aborted
	case errorx.KindOutOfRange:
		return codes.OutOfRange
	case errorx.KindUnimplemented:
		return codes.Unimplemented
	case errorx.KindInternal:
		return codes.Internal
	case errorx.KindUnavailable:
		return codes.Unavailable
	case errorx.KindDataLoss:
		return codes.DataLoss
	case errorx.KindUnauthenticated:
		return codes.Unauthenticated
	default:
		return codes.Unknown
	}
}

// grnObject adapts a parsed GRN to the Object seam for wire-driven
// checks.
type grnObject struct {
	g grn.GRN
}

func (o grnObject) GRN() grn.GRN { return o.g }
