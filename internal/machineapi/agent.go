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
// assigned to the calling operator at the current epoch, and refuses
// every other one (ADR-0015). The private key it returns is generated
// per certificate and stored nowhere.
func (s *Server) IssueAgentCertificate(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) {
	operator, ok := caller(w, r)
	if !ok {
		return
	}
	subject, err := grn.Parse(agentGRN)
	if err != nil {
		writeError(w, errorx.Wrap(err, "agent", errorx.KindInvalidArgument))
		return
	}
	certificate, err := s.agents.IssueCertificate(r.Context(), operator, subject)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, machinegen.AgentCertificate{
		Grn:            subject.String(),
		CertificatePem: string(certificate.CertificatePEM),
		PrivateKeyPem:  string(certificate.PrivateKeyPEM),
		IssuerPem:      string(certificate.IssuerPEM),
		NotAfter:       certificate.NotAfter,
	})
}
