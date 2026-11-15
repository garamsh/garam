// Package policy implements platform.Platform's Allowed against the
// policy service: cowork asks it directly, under its own caller token
// for the organization, as ADR-0141 §12 has a service do. Who a
// credential belongs to, and which organizations it has, are still the
// platform's to answer.
package policy

import (
	"context"
	"fmt"
	"time"

	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
)

// requestTimeout bounds one question, as the platform's own is. An
// answer that has not arrived is refused like one that said no.
const requestTimeout = 10 * time.Second

// Evaluator asks the policy service as the cowork of one organization.
type Evaluator interface {
	Evaluations(ctx context.Context, org string, req *policyv1.EvaluationsRequest) (*policyv1.EvaluationsResponse, error)
}

// Platform is a platform.Platform that asks the policy service where
// the platform would be asked for a decision, and the platform for
// everything else.
type Platform struct {
	platform.Platform
	policy Evaluator
}

var _ platform.Platform = (*Platform)(nil)

// New answers base with Allowed replaced by a question to policy.
func New(base platform.Platform, policy Evaluator) *Platform {
	return &Platform{Platform: base, policy: policy}
}

// tenantIsolation is the id of the system policy that denies a caller
// an organization it is no member of (policy/system-policies).
const tenantIsolation = "tenant-isolation"

// Allowed implements platform.Platform. The service has no question
// that is membership of an organization, which cowork may not ask, but
// it says why it denied: a decision whose reasons name tenant
// isolation is a caller outside the organization, and the batch is
// answered permission denied as the platform answers it. A denial for
// any other reason, a guardrail's say, is that check's own false. A
// batch that asks nothing still asks one question, whether the caller
// may read the organization's pages, to learn that from its reasons
// and not its decision. A service that cannot be asked, or answers
// otherwise than one decision per check, is unavailable.
func (p *Platform) Allowed(ctx context.Context, caller platform.Caller, org string, checks []platform.Check) ([]bool, error) {
	subject, err := subjectOf(caller)
	if err != nil {
		return nil, err
	}
	if grn.Org(org).Validate() != nil {
		return nil, errorx.New(errorx.KindPermissionDenied, "not a member of this organization")
	}
	req := &policyv1.EvaluationsRequest{
		Subject:     subject,
		Evaluations: make([]*policyv1.Evaluation, 0, len(checks)),
	}
	// asked[i] is where check i sits in the request; a check naming a
	// resource no GRN can name is denied without being asked.
	asked := make([]int, len(checks))
	for i, ch := range checks {
		resource, err := resourceOf(org, ch)
		if err != nil {
			return nil, err
		}
		if resource == nil {
			asked[i] = -1
			continue
		}
		asked[i] = len(req.Evaluations)
		req.Evaluations = append(req.Evaluations, evaluation(ch, resource))
	}
	if len(req.Evaluations) == 0 {
		req.Evaluations = append(req.Evaluations, evaluation(platform.Check{Action: platform.ActionPageRead}, resourceOfOrg(org)))
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	resp, err := p.policy.Evaluations(ctx, org, req)
	if err != nil {
		return nil, errorx.Wrap(err, "policy service unavailable", errorx.KindUnavailable)
	}
	answers := resp.GetEvaluations()
	if len(answers) != len(req.Evaluations) {
		return nil, errorx.New(errorx.KindUnavailable,
			fmt.Sprintf("policy service: %d questions answered %d decisions", len(req.Evaluations), len(answers)))
	}
	for _, a := range answers {
		for _, r := range a.GetContext().GetReasons() {
			if r.GetPolicyId() == tenantIsolation {
				return nil, errorx.New(errorx.KindPermissionDenied, "not a member of this organization")
			}
		}
	}
	allowed := make([]bool, len(checks))
	for i, at := range asked {
		allowed[i] = at >= 0 && answers[at].GetDecision()
	}
	return allowed, nil
}

// evaluation is the question ch asks of resource. It states probe on
// the entry, since a batch holds filters and attempts together, and
// leaves it unset for an attempt.
func evaluation(ch platform.Check, resource *policyv1.Resource) *policyv1.Evaluation {
	e := &policyv1.Evaluation{Action: &policyv1.Action{Name: ch.Action}, Resource: resource}
	if ch.Probe {
		e.Probe = &ch.Probe
	}
	return e
}

// subjectOf names the caller as the entity its GRN's type is.
func subjectOf(caller platform.Caller) (*policyv1.Subject, error) {
	info, ok := grn.Types("iam")[caller.GRN.ServiceType]
	if !ok || (caller.GRN.ServiceType != "user" && caller.GRN.ServiceType != "agent") {
		return nil, errorx.New(errorx.KindPermissionDenied, "the caller is neither a user nor an agent")
	}
	return &policyv1.Subject{Type: info.Entity, Id: caller.GRN.String()}, nil
}

func resourceOfOrg(org string) *policyv1.Resource {
	return &policyv1.Resource{Type: grn.Types("iam")["org"].Entity, Id: grn.Org(org).String()}
}

// kindTypes maps the kinds of resource cowork asks about to the GRN
// type each is.
var kindTypes = map[string]string{
	platform.KindPage:     "page",
	platform.KindChannel:  "channel",
	platform.KindTask:     "task",
	platform.KindArtifact: "artifact",
}

// resourceOf is what a check is asked of: the organization where it
// names no kind, and the resource of that kind in it otherwise. It is
// nil for an ID no GRN identifier can be.
func resourceOf(org string, ch platform.Check) (*policyv1.Resource, error) {
	if ch.Kind == "" {
		return resourceOfOrg(org), nil
	}
	typ, ok := kindTypes[ch.Kind]
	if !ok {
		return nil, errorx.New(errorx.KindInternal, "policy: no such kind of resource: "+ch.Kind)
	}
	g := grn.NewIn(org, "cowork", typ, ch.ID)
	if g.Validate() != nil {
		return nil, nil
	}
	return &policyv1.Resource{Type: grn.Types("cowork")[typ].Entity, Id: g.String()}, nil
}
