package api

import (
	"net/http"

	"github.com/garamsh/garam/platform/internal/grn"
)

// visible answers the items of a page that c may take action on, each
// asked on its own GRN, in one round trip to the engine (ADR-0115). A
// list is authorized on the organization first; visible then drops
// each object whose own read c is denied, so a Deny on one object
// hides it from the list as it refuses that object's read. object
// names an item's GRN and the statements that reach c through it
// alone. The page is filtered as read, so it can come back shorter
// than its limit with more after it.
func visible[T any](s *Server, r *http.Request, c *caller, action string, items []T, object func(T) BatchObject) ([]T, error) {
	if len(items) == 0 {
		return items, nil
	}
	objects := make([]BatchObject, len(items))
	for i, it := range items {
		objects[i] = object(it)
	}
	allowed, err := s.evaluator.EvaluateBatch(r.Context(), BatchInput{
		Subject:       c.user.GRN,
		Action:        action,
		SubjectGroups: c.groups,
		Statements:    memberStatements(c.org, c.member, c.user),
		SubjectOrg:    c.org,
		Objects:       objects,
	})
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(items))
	for i, it := range items {
		if allowed[i] {
			out = append(out, it)
		}
	}
	return out, nil
}

// onItself is the BatchObject of an object no statement reaches the
// caller through.
func onItself(g grn.GRN) BatchObject {
	return BatchObject{Object: g}
}
