package machineapi

import (
	"encoding/json"
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
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

// EnrollOperator answers with the certificate signed for the operator
// the token names, and spends the token (ADR-0064).
//
// It is the one handler here that reads no identity off the
// connection, because the caller has none yet: what authenticates the
// request is the token, minted by a signed-in member of the
// organization registering the operator. The subject is the token's
// and never the request's, so a certificate signing request asking for
// another identity obtains a certificate for the operator the token
// named or nothing at all.
//
// No private key is generated and none is returned. The operator made
// the key pair where the certificate will be used and sent the public
// half over; garam has never held the private one and cannot answer
// with it.
func (s *Server) EnrollOperator(w http.ResponseWriter, r *http.Request) {
	var body machinegen.Enrollment
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "enrollment", errorx.KindInvalidArgument))
		return
	}
	certificate, err := s.operators.Enroll(r.Context(), body.Token,
		operator.CertificateRequest(body.CertificateRequestPem))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, machinegen.EnrolledCertificate{
		Grn:            certificate.Subject.String(),
		CertificatePem: string(certificate.CertificatePEM),
		IssuerPem:      string(certificate.IssuerPEM),
		ServerRootPem:  string(certificate.ServerRootPEM),
		NotAfter:       certificate.NotAfter,
	})
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
