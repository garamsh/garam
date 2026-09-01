package machineapi

import (
	"encoding/json"
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// CreateAgent creates an agent in the calling operator's organization
// and assigns it to that operator. The request carries no body: the
// organization and the operator are what the certificate names, and
// the identifier is garam's to mint (ADR-0012).
func (s *Server) CreateAgent(w http.ResponseWriter, r *http.Request) {
	operator, ok := caller(w, r)
	if !ok {
		return
	}
	assignment, err := s.agents.Create(r.Context(), operator)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, machinegen.AgentAssignment{
		Grn:      assignment.Agent().String(),
		Operator: assignment.Operator().String(),
		Epoch:    assignment.Epoch(),
	})
}

// IssueAgentCertificate answers with the certificate of an agent
// assigned to the calling operator at the current epoch (ADR-0015), or
// with a renewal for the agent calling as itself (ADR-0040), and
// refuses every other caller. The private key it returns is generated
// per certificate and stored nowhere; the garam server root beside it
// is what the holder verifies garam by.
func (s *Server) IssueAgentCertificate(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) {
	identity, ok := caller(w, r)
	if !ok {
		return
	}
	subject, err := grn.Parse(agentGRN)
	if err != nil {
		writeError(w, errorx.Wrap(err, "agent", errorx.KindInvalidArgument))
		return
	}
	certificate, err := s.agents.IssueCertificate(r.Context(), identity, subject)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, machinegen.MachineCertificate{
		Grn:            subject.String(),
		CertificatePem: string(certificate.CertificatePEM),
		PrivateKeyPem:  string(certificate.PrivateKeyPEM),
		IssuerPem:      string(certificate.IssuerPEM),
		ServerRootPem:  string(certificate.ServerRootPEM),
		NotAfter:       certificate.NotAfter,
	})
}

// ReportProvisioningState records what the calling operator says about
// the agent's pod, which garam dials no cluster to learn (ADR-0012).
// `internal/agent` decides which agents an operator may report on. The
// answer carries nothing: what the store holds beside it is the runtime
// state the agent reports about itself, which is the other party's
// fact.
func (s *Server) ReportProvisioningState(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) {
	operator, subject, ok := parties(w, r, agentGRN)
	if !ok {
		return
	}
	var report machinegen.ProvisioningStateReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		writeError(w, errorx.Wrap(err, "provisioning state report", errorx.KindInvalidArgument))
		return
	}
	state, err := agent.ParseProvisioningState(string(report.State))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.agents.ReportProvisioningState(r.Context(), operator, subject, report.Epoch, state); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ReportRuntimeState records what an agent says about itself
// (ADR-0012). The caller reports for the agent it authenticates as and
// is refused every other one, as it is on its own queue and its own
// turn events.
func (s *Server) ReportRuntimeState(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) {
	caller, subject, ok := parties(w, r, agentGRN)
	if !ok {
		return
	}
	var report machinegen.RuntimeStateReport
	if err := json.NewDecoder(r.Body).Decode(&report); err != nil {
		writeError(w, errorx.Wrap(err, "runtime state report", errorx.KindInvalidArgument))
		return
	}
	state, err := agent.ParseRuntimeState(string(report.State))
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.agents.ReportRuntimeState(r.Context(), caller, subject, report.Epoch, state); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
