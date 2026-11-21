package api

import (
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// evaluationsPerCall is the most questions the policy service answers
// in one Evaluations call (MAX_EVALUATIONS in its policy_service.rs).
// A page longer than that, which a list asked without a limit can be,
// is asked in as many calls as it takes.
const evaluationsPerCall = 1000

// visible answers the items of a page that c may take action on, each
// asked on its own GRN, in one Evaluations call to the policy service
// (ADR-0172). A list is authorized on the organization first; visible
// then drops each object whose own read c is denied, so a Deny on one
// object hides it from the list as it refuses that object's read. The
// questions are probes: a denial that only hides an item is not
// recorded as an attempt (ADR-0148 item 2). An error of the service is
// unavailable, never an allow and never an empty page. The page is
// filtered as read, so it can come back shorter than its limit with
// more after it.
func visible[T any](s *Server, r *http.Request, c *caller, action string, items []T, object func(T) grn.GRN) ([]T, error) {
	out := make([]T, 0, len(items))
	for from := 0; from < len(items); from += evaluationsPerCall {
		chunk := items[from:min(from+evaluationsPerCall, len(items))]
		questions := make([]policydecision.Question, len(chunk))
		for i, it := range chunk {
			questions[i] = policydecision.Question{
				Subject: c.user.GRN, Action: action, Resource: askedOn(object(it)), Probe: true,
			}
		}
		decisions, err := s.decisions.Evaluations(r.Context(), questions)
		if err != nil {
			return nil, errorx.Wrap(err, "policy decision", errorx.KindUnavailable)
		}
		for i, it := range chunk {
			if decisions[i].Allowed {
				out = append(out, it)
			}
		}
	}
	return out, nil
}
