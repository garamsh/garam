package machineapi

import (
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
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
