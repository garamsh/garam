// Package grpc asks the policy service for decisions over its gRPC
// API, as the caller the connection's token names (ADR-0138).
package grpc

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/policydecision"
	feedgrpc "github.com/garamsh/garam/platform/internal/policyfeed/grpc"
)

// Client implements policydecision.Service over a policy service
// client. Dial in policyfeed/grpc gives the connection its token,
// whose subject is the caller `platform`.
type Client struct {
	client policyv1.PolicyServiceClient
}

// Compile-time check that Client satisfies policydecision.Service.
var _ policydecision.Service = (*Client)(nil)

// New constructs a client over client.
func New(client policyv1.PolicyServiceClient) *Client {
	return &Client{client: client}
}

// Evaluate asks one question.
func (c *Client) Evaluate(
	ctx context.Context, q policydecision.Question,
) (policydecision.Decision, error) {
	subject, resource, err := parties(q)
	if err != nil {
		return policydecision.Decision{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, feedgrpc.CallTimeout)
	defer cancel()
	resp, err := c.client.Evaluate(ctx, &policyv1.EvaluateRequest{
		Subject:  subject,
		Action:   &policyv1.Action{Name: q.Action},
		Resource: resource,
		Probe:    q.Probe,
	})
	if err != nil {
		return policydecision.Decision{}, wrapCall(err, "Evaluate")
	}
	return decision(resp), nil
}

// Evaluations asks the questions in one call. Each carries its own
// probe, so a mixed batch states each as its caller does.
func (c *Client) Evaluations(
	ctx context.Context, qs []policydecision.Question,
) ([]policydecision.Decision, error) {
	if len(qs) == 0 {
		return nil, errorx.New(errorx.KindInvalidArgument, "no question to ask")
	}
	asked := make([]*policyv1.Evaluation, len(qs))
	for i, q := range qs {
		subject, resource, err := parties(q)
		if err != nil {
			return nil, err
		}
		probe := q.Probe
		asked[i] = &policyv1.Evaluation{
			Subject:  subject,
			Action:   &policyv1.Action{Name: q.Action},
			Resource: resource,
			Probe:    &probe,
		}
	}
	ctx, cancel := context.WithTimeout(ctx, feedgrpc.CallTimeout)
	defer cancel()
	resp, err := c.client.Evaluations(ctx, &policyv1.EvaluationsRequest{Evaluations: asked})
	if err != nil {
		return nil, wrapCall(err, "Evaluations")
	}
	if len(resp.GetEvaluations()) != len(qs) {
		return nil, errorx.New(errorx.KindInternal, fmt.Sprintf(
			"policy service answered %d of %d questions", len(resp.GetEvaluations()), len(qs)))
	}
	out := make([]policydecision.Decision, len(qs))
	for i, r := range resp.GetEvaluations() {
		out[i] = decision(r)
	}
	return out, nil
}

// parties names the subject and the resource as the entity types their
// GRNs' services declare.
func parties(q policydecision.Question) (*policyv1.Subject, *policyv1.Resource, error) {
	subject, err := entity(q.Subject)
	if err != nil {
		return nil, nil, err
	}
	resource, err := entity(q.Resource)
	if err != nil {
		return nil, nil, err
	}
	return &policyv1.Subject{Type: subject, Id: q.Subject.String()},
		&policyv1.Resource{Type: resource, Id: q.Resource.String()}, nil
}

func entity(g grn.GRN) (string, error) {
	if err := g.Validate(); err != nil {
		return "", errorx.Wrap(err, "policy question names no GRN", errorx.KindInvalidArgument)
	}
	info, ok := grn.Types(g.Service)[g.ServiceType]
	if !ok {
		return "", errorx.New(errorx.KindInvalidArgument,
			"no entity type for "+g.Service+" "+g.ServiceType)
	}
	return info.Entity, nil
}

func decision(r *policyv1.EvaluateResponse) policydecision.Decision {
	reasons := policydecision.Reasons{}
	for _, x := range r.GetContext().GetReasons() {
		reasons[policydecision.Reason{PolicyID: x.GetPolicyId(), Error: x.GetError()}] = struct{}{}
	}
	return policydecision.Decision{Allowed: r.GetDecision(), Reasons: reasons}
}

// wrapCall names a failed call by the kind its status says. A service
// that cannot be reached is unavailable, which a caller reads as a
// denial and not as an answer.
func wrapCall(err error, method string) error {
	kind := errorx.KindInternal
	switch status.Code(err) {
	case codes.Unavailable:
		kind = errorx.KindUnavailable
	case codes.DeadlineExceeded:
		kind = errorx.KindDeadlineExceeded
	case codes.Canceled:
		kind = errorx.KindCanceled
	case codes.Unauthenticated, codes.PermissionDenied:
		kind = errorx.KindPermissionDenied
	case codes.InvalidArgument:
		kind = errorx.KindInvalidArgument
	}
	return errorx.Wrap(err, "policy service "+method, kind)
}
