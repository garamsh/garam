package machineapi

import (
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/operator"
)

// GetIdentity answers with the GRN the connection authenticated as. It
// is what tells a certificate that works from one that was never
// tried, and a freshly provisioned agent has nothing else to call.
func (s *Server) GetIdentity(w http.ResponseWriter, r *http.Request) {
	identity, ok := caller(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, machinegen.Identity{Grn: identity.String()})
}

// RenewIdentityCertificate answers with the successor to the
// certificate the connection authenticated with, for an operator
// renewing its own (ADR-0046). The route names no subject, so there is
// nothing to authorize here: what is replaced is what was presented,
// and which certificate that is comes off the handshake rather than off
// the request.
func (s *Server) RenewIdentityCertificate(w http.ResponseWriter, r *http.Request) {
	identity, ok := callerCertificate(w, r)
	if !ok {
		return
	}
	certificate, err := s.operators.Renew(r.Context(), operator.Credential{
		Subject:   identity.GRN(),
		Serial:    operator.Serial(identity.Serial()),
		NotBefore: identity.NotBefore(),
		NotAfter:  identity.NotAfter(),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, machinegen.MachineCertificate{
		Grn:            identity.GRN().String(),
		CertificatePem: string(certificate.CertificatePEM),
		PrivateKeyPem:  string(certificate.PrivateKeyPEM),
		IssuerPem:      string(certificate.IssuerPEM),
		ServerRootPem:  string(certificate.ServerRootPEM),
		NotAfter:       certificate.NotAfter,
	})
}
