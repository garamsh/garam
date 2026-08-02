package api

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/gaon/internal/errorx"
	"github.com/garamsh/gaon/internal/grn"
	authzv1 "github.com/garamsh/gaon/proto/gen/gaon/authz/v1"
)

// GRPCEvaluator is the Evaluator seam over gRPC (ADR-0003 stage 2).
type GRPCEvaluator struct {
	client authzv1.AuthzServiceClient
}

// Compile-time check that GRPCEvaluator satisfies Evaluator.
var _ Evaluator = (*GRPCEvaluator)(nil)

// NewGRPCEvaluator constructs an Evaluator that calls the authz
// engine over conn.
func NewGRPCEvaluator(conn grpc.ClientConnInterface) *GRPCEvaluator {
	return &GRPCEvaluator{client: authzv1.NewAuthzServiceClient(conn)}
}

// Evaluate sends the question to the engine as a Check RPC. gRPC
// status codes carry errorx kinds by name (ADR-0003); the mapping
// point is this transport.
func (e *GRPCEvaluator) Evaluate(ctx context.Context, in EvalInput) (bool, error) {
	resp, err := e.client.Check(ctx, &authzv1.CheckRequest{
		SubjectGrn:    in.Subject.String(),
		ObjectGrn:     in.Object.String(),
		Action:        in.Action,
		Statements:    statementsToProto(in.Statements),
		SubjectGroups: grnsToProto(in.SubjectGroups),
	})
	if err != nil {
		st, _ := status.FromError(err)
		return false, errorx.New(kindOf(st.Code()), st.Message())
	}
	return resp.GetAllowed(), nil
}

func statementsToProto(in []Statement) []*authzv1.Statement {
	out := make([]*authzv1.Statement, 0, len(in))
	for _, s := range in {
		out = append(out, &authzv1.Statement{
			Sid:      s.Sid,
			Effect:   effectToProto(s.Effect),
			Action:   s.Action,
			Resource: s.Resource,
		})
	}
	return out
}

func grnsToProto(in []grn.GRN) []string {
	out := make([]string, 0, len(in))
	for _, g := range in {
		out = append(out, g.String())
	}
	return out
}

func effectToProto(e Effect) authzv1.Effect {
	if e == EffectDeny {
		return authzv1.Effect_EFFECT_DENY
	}
	return authzv1.Effect_EFFECT_ALLOW
}

// kindOf maps gRPC status codes back to error kinds by name.
func kindOf(c codes.Code) errorx.Kind {
	switch c {
	case codes.Canceled:
		return errorx.KindCanceled
	case codes.InvalidArgument:
		return errorx.KindInvalidArgument
	case codes.DeadlineExceeded:
		return errorx.KindDeadlineExceeded
	case codes.NotFound:
		return errorx.KindNotFound
	case codes.AlreadyExists:
		return errorx.KindAlreadyExists
	case codes.PermissionDenied:
		return errorx.KindPermissionDenied
	case codes.ResourceExhausted:
		return errorx.KindResourceExhausted
	case codes.FailedPrecondition:
		return errorx.KindFailedPrecondition
	case codes.Aborted:
		return errorx.KindAborted
	case codes.OutOfRange:
		return errorx.KindOutOfRange
	case codes.Unimplemented:
		return errorx.KindUnimplemented
	case codes.Internal:
		return errorx.KindInternal
	case codes.Unavailable:
		return errorx.KindUnavailable
	case codes.DataLoss:
		return errorx.KindDataLoss
	case codes.Unauthenticated:
		return errorx.KindUnauthenticated
	default:
		return errorx.KindUnknown
	}
}
