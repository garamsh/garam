package machineapi

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// GRPCEvaluator is the Evaluator seam over gRPC (ADR-0003). The
// browser-facing api server reaches the same engine through a seam of
// its own: the two listeners never import each other and never run in
// the same process (ADR-0017).
type GRPCEvaluator struct {
	client authzv1.AuthzServiceClient
}

// Compile-time check that GRPCEvaluator satisfies Evaluator.
var _ Evaluator = (*GRPCEvaluator)(nil)

// NewGRPCEvaluator constructs an Evaluator that calls the authz engine
// over conn.
func NewGRPCEvaluator(conn grpc.ClientConnInterface) *GRPCEvaluator {
	return &GRPCEvaluator{client: authzv1.NewAuthzServiceClient(conn)}
}

// Evaluate sends the question to the engine as a Check RPC. gRPC
// status codes carry errorx kinds by name (ADR-0003); the mapping
// point is this transport.
func (e *GRPCEvaluator) Evaluate(ctx context.Context, in EvalInput) (bool, error) {
	statements, err := statementsToProto(in.Statements)
	if err != nil {
		return false, err
	}
	resp, err := e.client.Check(ctx, &authzv1.CheckRequest{
		SubjectGrn:    in.Subject.String(),
		ObjectGrn:     in.Object.String(),
		Action:        in.Action,
		Statements:    statements,
		SubjectGroups: grnsToProto(in.SubjectGroups),
	})
	if err != nil {
		return false, statusError(err)
	}
	return resp.GetAllowed(), nil
}

// statusError renders a gRPC error as an errorx error; status codes
// carry kinds by name (ADR-0003).
func statusError(err error) error {
	st, _ := status.FromError(err)
	return errorx.New(kindOf(st.Code()), st.Message())
}

func grnsToProto(in []grn.GRN) []string {
	out := make([]string, 0, len(in))
	for _, g := range in {
		out = append(out, g.String())
	}
	return out
}

func statementsToProto(in []Statement) ([]*authzv1.Statement, error) {
	out := make([]*authzv1.Statement, 0, len(in))
	for _, s := range in {
		effect, err := effectToProto(s.Effect)
		if err != nil {
			return nil, errorx.Wrap(err, "statement "+s.Sid)
		}
		out = append(out, &authzv1.Statement{
			Sid:      s.Sid,
			Effect:   effect,
			Action:   s.Action,
			Resource: s.Resource,
		})
	}
	return out, nil
}

// effectToProto renders an Effect on the wire. An Effect that is
// neither constant is refused: this process built the statement, so an
// unrepresentable one is its own defect and not the caller's.
func effectToProto(e Effect) (authzv1.Effect, error) {
	switch e {
	case EffectAllow:
		return authzv1.Effect_EFFECT_ALLOW, nil
	case EffectDeny:
		return authzv1.Effect_EFFECT_DENY, nil
	default:
		return authzv1.Effect_EFFECT_UNSPECIFIED, errorx.New(errorx.KindInternal, "effect must be Allow or Deny")
	}
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
