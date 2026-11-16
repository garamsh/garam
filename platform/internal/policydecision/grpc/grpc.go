// Package grpc asks the policy service for decisions over its gRPC
// API, as the caller the connection's token names (ADR-0138).
package grpc

import (
	"context"
	"fmt"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
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
	resp, err := c.evaluate(ctx, q)
	switch {
	case err == nil:
		return decision(resp), nil
	case principalNotApplicable(err):
		return policydecision.Decision{Reasons: policydecision.Reasons{}}, nil
	case errorx.KindOf(err) != errorx.KindUnknown:
		// The question named no entity type: it was never asked.
		return policydecision.Decision{}, err
	default:
		return policydecision.Decision{}, wrapCall(err, "Evaluate")
	}
}

// evaluate asks one question and returns the service's answer or its
// refusal as the status it came in, which carries the cause.
func (c *Client) evaluate(
	ctx context.Context, q policydecision.Question,
) (*policyv1.EvaluateResponse, error) {
	subject, resource, err := parties(q)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, feedgrpc.CallTimeout)
	defer cancel()
	return c.client.Evaluate(ctx, &policyv1.EvaluateRequest{
		Subject:  subject,
		Action:   &policyv1.Action{Name: q.Action},
		Resource: resource,
		Probe:    q.Probe,
	})
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
		if !refused(out[i]) {
			continue
		}
		// A refused entry names no cause, only a text, so the cause is
		// read from the refusal of the same question asked alone
		// (ADR-0167).
		_, err := c.evaluate(ctx, qs[i])
		switch {
		case principalNotApplicable(err):
			out[i] = policydecision.Decision{Reasons: policydecision.Reasons{}}
		case err == nil, status.Code(err) == codes.InvalidArgument:
			// Decided when asked alone, or refused for another cause:
			// the entry stays as the batch gave it.
		default:
			return nil, wrapCall(err, "Evaluate")
		}
	}
	return out, nil
}

// refused reports a decision of a question the service refused, which
// carries a reason naming no policy.
func refused(d policydecision.Decision) bool {
	for r := range d.Reasons {
		if r.PolicyID == "" && r.Error != "" {
			return true
		}
	}
	return false
}

// causeReason and causeDomain name the refusal of a question whose
// principal type the action does not apply to (ADR-0167).
const (
	causeReason = "PRINCIPAL_NOT_APPLICABLE"
	causeDomain = "garam.policy.v1"
)

// principalNotApplicable reports whether err is a refusal that names
// that cause in an ErrorInfo detail. A refusal that names no cause, or
// another, is not.
func principalNotApplicable(err error) bool {
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		return false
	}
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok &&
			info.GetReason() == causeReason && info.GetDomain() == causeDomain {
			return true
		}
	}
	return false
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
