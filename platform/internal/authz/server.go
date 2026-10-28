package authz

import (
	"context"
	"slices"

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
	_, allowed, err := decide(ctx, in, s.policies.Statements)
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
	_, allowed, err := decide(ctx, in, s.policies.CurrentStatements)
	if err != nil {
		return nil, grpcError(err)
	}
	return &authzv1.CheckCurrentResponse{Allowed: allowed}, nil
}

// CheckBatch decides Check's question for each object of the request,
// reading the persisted statements once: every object is of the
// subject's organization, so one read answers for all of them. An
// object outside it refuses the call before anything is read, as
// Check refuses it.
func (s *GRPCServer) CheckBatch(ctx context.Context, req *authzv1.CheckBatchRequest) (*authzv1.CheckBatchResponse, error) {
	inputs, err := parseBatch(req)
	if err != nil {
		return nil, grpcError(err)
	}
	for _, in := range inputs {
		if err := in.checkScope(); err != nil {
			return nil, grpcError(err)
		}
	}
	allowed := make([]bool, len(inputs))
	if len(inputs) == 0 {
		return &authzv1.CheckBatchResponse{Allowed: allowed}, nil
	}
	stored, err := s.policies.Statements(ctx, inputs[0].subjectOrg, inputs[0].subjects)
	if err != nil {
		return nil, grpcError(err)
	}
	for i, in := range inputs {
		_, allowed[i] = evaluate(ctx, in, stored)
	}
	return &authzv1.CheckBatchResponse{Allowed: allowed}, nil
}

// decide scopes the request, reads the persisted statements through
// read and evaluates them with the caller's. It returns the grants it
// evaluated beside the decision, so Explain traces the set Check
// decided over and takes its answer from the same evaluator.
func decide(ctx context.Context, in checkInput, read readStatements) ([]Grant, bool, error) {
	if err := in.checkScope(); err != nil {
		return nil, false, err
	}
	stored, err := read(ctx, orgGRNOf(in.object), in.subjects)
	if err != nil {
		return nil, false, err
	}
	grants, allowed := evaluate(ctx, in, stored)
	return grants, allowed, nil
}

// evaluate decides in over the persisted statements stored that reach
// its subject, unioned with the caller's.
func evaluate(ctx context.Context, in checkInput, stored []Grant) ([]Grant, bool) {
	grants := applicable(in, stored)
	statements := make([]Statement, 0, len(grants))
	for _, g := range grants {
		statements = append(statements, g.Statement)
	}
	d := NewStatementEvaluator(statements).Evaluate(ctx, Context{
		Object: grnObject{in.object},
		Action: in.action,
	})
	return grants, d == DecisionAllow
}

// Explain evaluates the same input and reports every statement's
// contribution, so a denied request is reproducible after the fact.
// A persisted statement's trace names its policy and the subjects of
// the request that policy is attached to; a caller's statement names
// neither.
func (s *GRPCServer) Explain(ctx context.Context, req *authzv1.ExplainRequest) (*authzv1.ExplainResponse, error) {
	in, err := parseInput(req.GetSubjectGrn(), req.GetObjectGrn(), req.GetAction(), req.GetSubjectGroups(), req.GetSubjectOrg(), req.GetStatements())
	if err != nil {
		return nil, grpcError(err)
	}
	grants, allowed, err := decide(ctx, in, s.policies.Statements)
	if err != nil {
		return nil, grpcError(err)
	}
	traces := make([]*authzv1.StatementTrace, 0, len(grants))
	for _, g := range grants {
		trace := &authzv1.StatementTrace{
			Sid:        g.Sid,
			Matched:    g.Matches(in.action, in.object.String()),
			Effect:     effectToProto(g.Effect),
			AttachedTo: wire.GRNsToProto(g.AttachedTo),
		}
		if !g.Policy.IsZero() {
			trace.PolicyGrn = g.Policy.String()
		}
		traces = append(traces, trace)
	}
	return &authzv1.ExplainResponse{Allowed: allowed, Traces: traces}, nil
}

// readStatements is one of PolicySource's two reads.
type readStatements func(ctx context.Context, orgGRN grn.GRN, subjects []grn.GRN) ([]Grant, error)

// applicable unions the persisted policy statements stored that reach
// the subject — org-wide, attached to it, or attached to one of its
// groups (ADR-0008) — with the role-derived statements the caller
// supplied (ADR-0007).
func applicable(in checkInput, stored []Grant) []Grant {
	grants := make([]Grant, 0, len(stored)+len(in.statements))
	grants = append(grants, stored...)
	for _, st := range in.statements {
		grants = append(grants, Grant{Statement: st})
	}
	return grants
}

// orgGRNOf returns the GRN of the org that owns the object. Orgs are
// root-owned, so an org's identifier is the organization segment of
// every resource beneath it (ADR-0001).
func orgGRNOf(object grn.GRN) grn.GRN {
	if object.ServiceType == grn.ServiceTypeOrg {
		return object
	}
	return grn.Org(object.Organization)
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
	in, err := parseSubject(subjectGRN, action, subjectGroups, subjectOrg)
	if err != nil {
		return checkInput{}, err
	}
	if in.object, err = grn.Parse(objectGRN); err != nil {
		return checkInput{}, errorx.Wrap(err, "object_grn")
	}
	if in.statements, err = parseStatements(stmts); err != nil {
		return checkInput{}, err
	}
	return in, nil
}

// parseBatch validates a CheckBatch request as parseInput validates a
// Check, answering one input per object: the request's subject, action
// and statements, with the object's own statements after them.
func parseBatch(req *authzv1.CheckBatchRequest) ([]checkInput, error) {
	subject, err := parseSubject(req.GetSubjectGrn(), req.GetAction(), req.GetSubjectGroups(), req.GetSubjectOrg())
	if err != nil {
		return nil, err
	}
	shared, err := parseStatements(req.GetStatements())
	if err != nil {
		return nil, err
	}
	inputs := make([]checkInput, 0, len(req.GetObjects()))
	for _, o := range req.GetObjects() {
		in := subject
		if in.object, err = grn.Parse(o.GetObjectGrn()); err != nil {
			return nil, errorx.Wrap(err, "objects.object_grn")
		}
		own, err := parseStatements(o.GetStatements())
		if err != nil {
			return nil, err
		}
		in.statements = append(slices.Clip(shared), own...)
		inputs = append(inputs, in)
	}
	return inputs, nil
}

// parseSubject parses what a request says of its subject and action,
// leaving the object and the statements to the caller.
func parseSubject(subjectGRN, action string, subjectGroups []string, subjectOrg string) (checkInput, error) {
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
	return checkInput{action: action, subjects: subjects, subjectOrg: org}, nil
}

// parseStatements parses the statements a caller sent.
func parseStatements(stmts []*authzv1.Statement) ([]Statement, error) {
	statements := make([]Statement, 0, len(stmts))
	for _, p := range stmts {
		effect, err := wire.EffectFromProto(p.GetEffect())
		if err != nil {
			return nil, errorx.Wrap(err, "statement "+p.GetSid())
		}
		if len(p.GetAction()) == 0 || len(p.GetResource()) == 0 {
			return nil, errorx.New(errorx.KindInvalidArgument, "statement "+p.GetSid()+": action and resource patterns are required")
		}
		statements = append(statements, Statement{
			Sid:      p.GetSid(),
			Effect:   effect,
			Action:   p.GetAction(),
			Resource: p.GetResource(),
		})
	}
	return statements, nil
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
		status: wire.GRPCStatusOf(err),
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

// grnObject adapts a parsed GRN to the Object seam for wire-driven
// checks.
type grnObject struct {
	g grn.GRN
}

func (o grnObject) GRN() grn.GRN { return o.g }
