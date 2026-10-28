package machineapi

import (
	"encoding/json"
	"net/http"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/operator"
)

// IntrospectOperationAuthority answers what an operation authority
// binds, to its audience only and once current state still authorizes
// it (ADR-0078). It consumes nothing.
func (s *Server) IntrospectOperationAuthority(w http.ResponseWriter, r *http.Request, params machinegen.IntrospectOperationAuthorityParams) {
	if !served(w, params.ContractVersion, authorityValues) {
		return
	}
	identity, ok := caller(w, r)
	if !ok {
		return
	}
	var body machinegen.AuthorityIntrospection
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "authority introspection", errorx.KindInvalidArgument))
		return
	}
	in, err := s.authority.Introspect(r.Context(), identity, body.Authority)
	if err != nil {
		writeError(w, err)
		return
	}
	out := bindingToAPI(in.Binding)
	out.ExpiresAt = &in.ExpiresAt
	writeJSON(w, http.StatusOK, out)
}

// CheckOperationReference rechecks a durable operation reference for
// its audience, as an Operator does before activating a new desired
// revision. It is not a restart capability.
func (s *Server) CheckOperationReference(w http.ResponseWriter, r *http.Request, ref machinegen.OperationRef, params machinegen.CheckOperationReferenceParams) {
	if !served(w, params.ContractVersion, authorityValues) {
		return
	}
	identity, ok := caller(w, r)
	if !ok {
		return
	}
	b, err := s.authority.Reference(r.Context(), identity, operator.OperationRef(ref))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bindingToAPI(b))
}

func bindingToAPI(b operator.Binding) machinegen.OperationBinding {
	out := machinegen.OperationBinding{
		OperationRef: string(b.Ref),
		GrantId:      string(b.Grant),
		OrgGrn:       b.Org.String(),
		ActorGrn:     b.Actor.String(),
		AudienceGrn:  b.Audience.String(),
		Operation:    machinegen.OperationBindingOperation(b.Operation),
		TargetGrn:    b.Target.String(),
		RequestId:    string(b.RequestID),
		BodySha256:   string(b.Digest),
	}
	if b.Assignment != nil {
		out.Assignment = &machinegen.BoundAssignment{
			OperatorGrn: b.Assignment.Assignee.String(),
			Epoch:       b.Assignment.Epoch,
		}
	}
	if b.RequestTarget != nil {
		target := string(*b.RequestTarget)
		out.RequestTarget = &target
	}
	if b.Destination != nil {
		destination := b.Destination.String()
		out.DestinationGrn = &destination
	}
	return out
}
