package machineapi

import (
	"context"
	"encoding/json"
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/wire"
)

// IntrospectController answers a positive proof of a controller's
// current authority to the hosted operator calling, for one lifecycle
// decision (ADR-0078). Every check is read on this request: the
// caller's own token and registration, then the controller's token
// exactly as the caller forwarded it, minted for the caller (ADR-0100)
// and never a GRN the caller asserts, then the grant, a named agent's
// placement and current policy.
func (s *Server) IntrospectController(w http.ResponseWriter, r *http.Request, controller machinegen.ControllerGrn, params machinegen.IntrospectControllerParams) {
	if !served(w, params.ContractVersion, authorityValues) {
		return
	}
	identity, ok := identityOf(w, r)
	if !ok {
		return
	}
	subject, err := operator.ParseController(identity.GRN(), controller)
	if err != nil {
		writeError(w, err)
		return
	}
	var body machinegen.ControllerIntrospection
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "controller introspection", errorx.KindInvalidArgument))
		return
	}
	if body.Token == "" {
		writeError(w, errorx.New(errorx.KindInvalidArgument, "controller introspection carries the controller's token"))
		return
	}
	prove := s.controllerCredential(subject, identity.GRN(), body)
	var agent *grn.GRN
	if body.Agent != nil {
		parsed, err := grn.Parse(*body.Agent)
		if err != nil {
			writeError(w, errorx.Wrap(err, "agent", errorx.KindInvalidArgument))
			return
		}
		agent = &parsed
	}
	ctx := r.Context()
	if err := s.rechecker.Recheck(ctx, identity); err != nil {
		// The caller's own record withdrawn is a refusal of its
		// authority and not an unknown controller.
		if errorx.IsKind(err, errorx.KindNotFound) {
			err = errorx.Wrap(err, "the caller's registration is withdrawn", errorx.KindPermissionDenied)
		}
		writeError(w, err)
		return
	}
	if err := prove(ctx); err != nil {
		// A token that does not prove the controller is the one
		// refusal this route answers as 422 rather than 400, so a caller
		// tells a defect of its request from the credential it
		// forwarded.
		status := wire.StatusOf(errorx.KindOf(err))
		if errorx.IsKind(err, errorx.KindInvalidArgument) {
			status = http.StatusUnprocessableEntity
		}
		writeErrorStatus(w, status, err)
		return
	}
	proof, err := s.proofs.Prove(ctx, identity.GRN(), subject, agent)
	if err != nil {
		writeError(w, err)
		return
	}
	out := machinegen.ControllerProof{Operator: proof.Controller.String(), Org: proof.Org.String()}
	if proof.Agent != nil {
		out.Agent = &machinegen.ProvenAgent{Grn: proof.Agent.Agent.String(), Epoch: proof.Agent.Epoch}
	}
	writeJSON(w, http.StatusOK, out)
}

// controllerCredential answers what proves the controller's token the
// request forwards, minted for the caller (ADR-0100).
func (s *Server) controllerCredential(controller, caller grn.GRN, body machinegen.ControllerIntrospection) func(context.Context) error {
	return func(ctx context.Context) error { return s.operators.VerifyShown(ctx, controller, caller, body.Token) }
}
