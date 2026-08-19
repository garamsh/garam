package machineapi

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/wire"
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
		SubjectGroups: wire.GRNsToProto(in.SubjectGroups),
		SubjectOrg:    in.SubjectOrg.String(),
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
	return errorx.New(wire.KindOf(st.Code()), st.Message())
}

func statementsToProto(in []Statement) ([]*authzv1.Statement, error) {
	out := make([]*authzv1.Statement, 0, len(in))
	for _, s := range in {
		effect, err := wire.EffectToProto(s.Effect)
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
