package machineapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// CreateManagedAgent creates an agent assigned to a controller for the
// hosted control service calling, under a member's agent:create
// reference (ADR-0077). The caller's own credential and registration
// are read again first, as on every lifecycle decision. Creation opens
// the agent's lineage under managed-enrollment.v1 and v2 alike; under
// v2 its assignee then mints the agent's tokens in it (ADR-0100).
func (s *Server) CreateManagedAgent(w http.ResponseWriter, r *http.Request, controllerGRN machinegen.ControllerGrn, params machinegen.CreateManagedAgentParams) {
	if !served(w, r, params.EnrollmentContractVersion, enrollmentValues) {
		return
	}
	r, cancel := lifecycleBudget(r)
	defer cancel()
	identity, ok := identityOf(w, r)
	if !ok || !s.recheck(w, r, identity) {
		return
	}
	caller := identity.GRN()
	controller, err := grn.Parse(controllerGRN)
	if err != nil {
		writeError(w, errorx.Wrap(err, "controller", errorx.KindInvalidArgument))
		return
	}
	var body machinegen.ManagedAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "managed agent", errorx.KindInvalidArgument))
		return
	}
	requestID, err := agent.ParseRequestID(body.RequestId)
	if err != nil {
		writeError(w, err)
		return
	}
	creation, created, err := s.executions.CreateManaged(r.Context(), caller, agent.ManagedCreationInput{
		Controller: controller, RequestID: requestID, OperationRef: body.OperationRef,
	})
	if err != nil {
		writeError(w, withinBudget(r, err))
		return
	}
	writeJSON(w, createdOrReplayed(created), machinegen.ManagedAgent{
		Grn: creation.Agent.String(), Epoch: strconv.Itoa(creation.Epoch),
	})
}

// IssueInitialCertificate signs a managed agent's first certificate
// over the key its controller generated, for the hosted control
// service calling under the agent's creation reference. The answer is
// public: no private key reaches garam or leaves it. It goes with the
// certificate path and refuses a token itself, beside ConfineTokens:
// an agent created under managed-enrollment.v2 is minted tokens by its
// assignee instead (ADR-0100).
func (s *Server) IssueInitialCertificate(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, params machinegen.IssueInitialCertificateParams) {
	if tokenBorne(r) {
		writeError(w, errorx.New(errorx.KindUnauthenticated,
			"initial issuance takes no token: the assignee mints a token-borne agent's tokens instead"))
		return
	}
	if !served(w, r, params.EnrollmentContractVersion, enrollmentValues) {
		return
	}
	r, cancel := lifecycleBudget(r)
	defer cancel()
	caller, subject, ok := s.lifecycleParties(w, r, agentGRN)
	if !ok {
		return
	}
	var body machinegen.InitialCertificateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "initial certificate", errorx.KindInvalidArgument))
		return
	}
	requestID, err := agent.ParseRequestID(body.RequestId)
	if err != nil {
		writeError(w, err)
		return
	}
	epoch, err := parseEpoch(body.Epoch)
	if err != nil {
		writeError(w, err)
		return
	}
	issued, created, err := s.executions.IssueInitialCertificate(r.Context(), caller, agent.InitialCertificateInput{
		Agent: subject, RequestID: requestID, Epoch: epoch, OperationRef: body.OperationRef,
		CertificateRequestPEM: []byte(body.CertificateRequestPem),
	})
	if err != nil {
		err = withinBudget(r, err)
		if errors.Is(err, agent.ErrInvalidCertificateRequest) {
			writeErrorStatus(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, createdOrReplayed(created), machinegen.InitialCertificate{
		CertificatePem: issued.CertificatePEM,
		IssuerPem:      issued.IssuerPEM,
		ServerRootPem:  issued.ServerRootPEM,
		NotAfter:       issued.NotAfter.UTC(),
	})
}

// createdOrReplayed is 201 for what this request wrote and 200 for
// what an identical earlier one did.
func createdOrReplayed(created bool) int {
	if created {
		return http.StatusCreated
	}
	return http.StatusOK
}
