package authz

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
	authzv1 "github.com/garamsh/gaon/proto/gen/gaon/authz/v1"
)

// GRPCServer serves the policy engine over gRPC (ADR-0003 stage 2).
// The caller supplies the statements collected for the subject; the
// server matches strings only and never imports a domain.
type GRPCServer struct {
	authzv1.UnimplementedAuthzServiceServer
}

// Compile-time check that GRPCServer satisfies the generated service.
var _ authzv1.AuthzServiceServer = (*GRPCServer)(nil)

// NewGRPCServer constructs the authz gRPC service.
func NewGRPCServer() *GRPCServer {
	return &GRPCServer{}
}

// Check evaluates the request's statements and returns the final
// decision: explicit Deny wins, otherwise any matching Allow permits.
func (s *GRPCServer) Check(ctx context.Context, req *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	in, err := parseInput(req.GetSubjectGrn(), req.GetObjectGrn(), req.GetAction(), req.GetStatements())
	if err != nil {
		return nil, grpcError(err)
	}
	d := NewStatementEvaluator(in.statements).Evaluate(ctx, Context{
		Object: grnObject{in.object},
		Action: in.action,
	})
	return &authzv1.CheckResponse{Allowed: d == DecisionAllow}, nil
}

// Explain evaluates the same input and reports every statement's
// contribution, so a denied request is reproducible after the fact.
func (s *GRPCServer) Explain(_ context.Context, req *authzv1.ExplainRequest) (*authzv1.ExplainResponse, error) {
	in, err := parseInput(req.GetSubjectGrn(), req.GetObjectGrn(), req.GetAction(), req.GetStatements())
	if err != nil {
		return nil, grpcError(err)
	}
	traces := make([]*authzv1.StatementTrace, 0, len(in.statements))
	denied, allowed := false, false
	for _, st := range in.statements {
		matched := st.Matches(in.action, in.object.String())
		traces = append(traces, &authzv1.StatementTrace{
			Sid:     st.Sid,
			Matched: matched,
			Effect:  effectToProto(st.Effect),
		})
		if !matched {
			continue
		}
		if st.Effect == EffectDeny {
			denied = true
		} else {
			allowed = true
		}
	}
	return &authzv1.ExplainResponse{Allowed: allowed && !denied, Traces: traces}, nil
}

// checkInput is a parsed Check/Explain request.
type checkInput struct {
	subject    grn.GRN
	object     grn.GRN
	action     string
	statements []Statement
}

// parseInput validates the wire request once at the transport
// boundary; downstream code trusts the parsed values.
func parseInput(subjectGRN, objectGRN, action string, stmts []*authzv1.Statement) (checkInput, error) {
	subject, err := grn.Parse(subjectGRN)
	if err != nil {
		return checkInput{}, errorx.Wrap(err, "subject_grn")
	}
	object, err := grn.Parse(objectGRN)
	if err != nil {
		return checkInput{}, errorx.Wrap(err, "object_grn")
	}
	if action == "" {
		return checkInput{}, errorx.New(errorx.KindInvalidArgument, "action is required")
	}
	statements := make([]Statement, 0, len(stmts))
	for _, p := range stmts {
		effect, err := effectFromProto(p.GetEffect())
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
	return checkInput{subject: subject, object: object, action: action, statements: statements}, nil
}

func effectFromProto(e authzv1.Effect) (Effect, error) {
	switch e {
	case authzv1.Effect_EFFECT_ALLOW:
		return EffectAllow, nil
	case authzv1.Effect_EFFECT_DENY:
		return EffectDeny, nil
	default:
		return "", errorx.New(errorx.KindInvalidArgument, "effect must be ALLOW or DENY")
	}
}

func effectToProto(e Effect) authzv1.Effect {
	if e == EffectAllow {
		return authzv1.Effect_EFFECT_ALLOW
	}
	return authzv1.Effect_EFFECT_DENY
}

// grpcError renders an errorx error as a gRPC status. Kinds are
// gRPC-aligned, so the kind carries over the status code by name
// (ADR-0003); the mapping point is this transport.
func grpcError(err error) error {
	return status.Error(codeOf(errorx.KindOf(err)), err.Error())
}

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
func (o grnObject) Type() string { return o.g.ServiceType }
