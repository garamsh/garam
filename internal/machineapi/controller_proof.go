package machineapi

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strings"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/wire"
)

// IntrospectController answers a positive proof of a controller's
// current authority to the hosted operator calling, for one lifecycle
// decision (ADR-0078). The connection authenticated once, so every
// check is read again on this request: the caller's own credential and
// registration, then the controller's credential exactly as the caller
// forwarded it — its certificate, or under operation-authority.v2 its
// token minted for the caller (ADR-0100), never a GRN the caller
// asserts — then the grant, a named agent's placement and current
// policy.
func (s *Server) IntrospectController(w http.ResponseWriter, r *http.Request, controller machinegen.ControllerGrn, params machinegen.IntrospectControllerParams) {
	if !served(w, r, params.ContractVersion, authorityValues) {
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
	prove, err := s.controllerCredential(r, subject, identity.GRN(), body)
	if err != nil {
		writeError(w, err)
		return
	}
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
	if err := s.certificates.Recheck(ctx, identity); err != nil {
		// The caller's own record withdrawn is a refusal of its
		// authority and not an unknown controller.
		if errorx.IsKind(err, errorx.KindNotFound) {
			err = errorx.Wrap(err, "the caller's registration is withdrawn", errorx.KindPermissionDenied)
		}
		writeError(w, err)
		return
	}
	if err := prove(ctx); err != nil {
		// A credential that does not prove the controller is the one
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

// controllerCredential parses the controller's credential the request
// forwards and answers what proves it: its certificate under
// operation-authority.v1, and under operation-authority.v2 its token
// minted for the caller (ADR-0100). Each contract takes its own field
// and refuses the other's.
func (s *Server) controllerCredential(r *http.Request, controller, caller grn.GRN, body machinegen.ControllerIntrospection) (func(context.Context) error, error) {
	if tokenBorne(r) {
		if body.CertificatePem != nil || body.Token == nil {
			return nil, errorx.New(errorx.KindInvalidArgument,
				"under "+operator.TokenAuthorityContractVersion+" the controller's credential is token, and certificatePem is refused")
		}
		token := *body.Token
		return func(ctx context.Context) error { return s.operators.VerifyShown(ctx, controller, caller, token) }, nil
	}
	if body.Token != nil || body.CertificatePem == nil {
		return nil, errorx.New(errorx.KindInvalidArgument,
			"under "+operator.ContractVersion+" the controller's credential is certificatePem, and token is refused")
	}
	leaf, err := leafOf(*body.CertificatePem)
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context) error { return s.certificates.Verify(ctx, controller, leaf) }, nil
}

// leafOf parses the one PEM certificate a request forwards. Anything
// else in the field — no block, another block type, a second block, or
// DER that does not parse — is malformed input.
func leafOf(s string) (*x509.Certificate, error) {
	block, rest := pem.Decode([]byte(s))
	if block == nil || block.Type != "CERTIFICATE" || strings.TrimSpace(string(rest)) != "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "certificatePem must hold exactly one PEM certificate")
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, errorx.Wrap(err, "certificatePem", errorx.KindInvalidArgument)
	}
	return leaf, nil
}
