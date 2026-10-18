package api

import (
	"context"

	"google.golang.org/grpc"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/wire"
	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
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
		return false, wire.ErrorOf(err)
	}
	return resp.GetAllowed(), nil
}

// EvaluateBatch sends the question to the engine as one CheckBatch
// RPC. An answer that does not hold one decision per object is the
// engine's fault.
func (e *GRPCEvaluator) EvaluateBatch(ctx context.Context, in BatchInput) ([]bool, error) {
	shared, err := statementsToProto(in.Statements)
	if err != nil {
		return nil, err
	}
	objects := make([]*authzv1.CheckBatchObject, 0, len(in.Objects))
	for _, o := range in.Objects {
		own, err := statementsToProto(o.Statements)
		if err != nil {
			return nil, err
		}
		objects = append(objects, &authzv1.CheckBatchObject{ObjectGrn: o.Object.String(), Statements: own})
	}
	resp, err := e.client.CheckBatch(ctx, &authzv1.CheckBatchRequest{
		SubjectGrn:    in.Subject.String(),
		Action:        in.Action,
		Statements:    shared,
		SubjectGroups: wire.GRNsToProto(in.SubjectGroups),
		SubjectOrg:    in.SubjectOrg.String(),
		Objects:       objects,
	})
	if err != nil {
		return nil, wire.ErrorOf(err)
	}
	if len(resp.GetAllowed()) != len(in.Objects) {
		return nil, errorx.New(errorx.KindInternal, "check batch answered a decision count other than its objects'")
	}
	return resp.GetAllowed(), nil
}

// Explain sends the question to the engine as an Explain RPC and
// parses each trace once, here, where it crosses into this process.
func (e *GRPCEvaluator) Explain(ctx context.Context, in EvalInput) (Explanation, error) {
	statements, err := statementsToProto(in.Statements)
	if err != nil {
		return Explanation{}, err
	}
	resp, err := e.client.Explain(ctx, &authzv1.ExplainRequest{
		SubjectGrn:    in.Subject.String(),
		ObjectGrn:     in.Object.String(),
		Action:        in.Action,
		Statements:    statements,
		SubjectGroups: wire.GRNsToProto(in.SubjectGroups),
		SubjectOrg:    in.SubjectOrg.String(),
	})
	if err != nil {
		return Explanation{}, wire.ErrorOf(err)
	}
	traces := make([]Trace, 0, len(resp.GetTraces()))
	for _, t := range resp.GetTraces() {
		trace, err := traceFromProto(t)
		if err != nil {
			return Explanation{}, err
		}
		traces = append(traces, trace)
	}
	return Explanation{Allowed: resp.GetAllowed(), Traces: traces}, nil
}

// traceFromProto parses one trace the engine answered. A trace that
// does not parse is the engine's fault, not the caller's.
func traceFromProto(t *authzv1.StatementTrace) (Trace, error) {
	effect, err := wire.EffectFromProto(t.GetEffect())
	if err != nil {
		return Trace{}, errorx.Wrap(err, "explain trace "+t.GetSid(), errorx.KindInternal)
	}
	var policy grn.GRN
	if raw := t.GetPolicyGrn(); raw != "" {
		if policy, err = grn.Parse(raw); err != nil {
			return Trace{}, errorx.Wrap(err, "explain trace policy grn", errorx.KindInternal)
		}
	}
	attached := make([]grn.GRN, 0, len(t.GetAttachedTo()))
	for _, raw := range t.GetAttachedTo() {
		g, err := grn.Parse(raw)
		if err != nil {
			return Trace{}, errorx.Wrap(err, "explain trace attachment grn", errorx.KindInternal)
		}
		attached = append(attached, g)
	}
	return Trace{Sid: t.GetSid(), Matched: t.GetMatched(), Effect: effect, Policy: policy, AttachedTo: attached}, nil
}

func statementsToProto(in []Statement) ([]*authzv1.Statement, error) {
	out := make([]*authzv1.Statement, 0, len(in))
	for _, s := range in {
		statement, err := wire.StatementToProto(s.Sid, s.Effect, s.Action, s.Resource)
		if err != nil {
			return nil, err
		}
		out = append(out, statement)
	}
	return out, nil
}
