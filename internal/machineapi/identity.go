package machineapi

import (
	"encoding/json"
	"net/http"
	"strings"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
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

// ExchangeToken answers a short-lived token for the operator or the
// standalone host's supervisor the presented credential belongs to
// (ADR-0100); the credential's prefix names which.
//
// Like EnrollOperator it reads no identity off the connection: what
// authenticates the request is the credential in its Authorization
// header, which a member of the organization issued or an approved
// enrollment answered.
func (s *Server) ExchangeToken(w http.ResponseWriter, r *http.Request, params machinegen.ExchangeTokenParams) {
	w.Header().Set("Garam-Contract-Version", operator.TokenContractVersion)
	// The credential is read first, so a caller that presents none
	// learns what authenticates this route before what it serves.
	credential, ok := strings.CutPrefix(r.Header.Get("Authorization"), operator.CredentialScheme+" ")
	if !ok || credential == "" {
		writeError(w, errorx.New(errorx.KindUnauthenticated,
			"an exchange carries Authorization: "+operator.CredentialScheme+" <credential>"))
		return
	}
	if params.TokenContractVersion != operator.TokenContractVersion {
		writeError(w, operator.ErrUnsupportedTokenContract)
		return
	}
	var body machinegen.TokenExchange
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "token exchange", errorx.KindInvalidArgument))
		return
	}
	// A supervisor's credential, which its prefix names, exchanges for
	// a token garam alone accepts (ADR-0100).
	if strings.HasPrefix(credential, agent.SupervisorCredentialPrefix) {
		if body.Audience != nil {
			writeError(w, agent.ErrSupervisorAudience)
			return
		}
		token, err := s.supervisors.Exchange(r.Context(), credential)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, machinegen.MachineToken{
			Token: token.Value, Grn: token.Supervisor.String(), ExpiresAt: token.ExpiresAt,
		})
		return
	}
	var audience *grn.GRN
	if body.Audience != nil {
		g, err := grn.Parse(*body.Audience)
		if err != nil {
			writeError(w, errorx.Wrap(err, "audience", errorx.KindInvalidArgument))
			return
		}
		audience = &g
	}
	token, err := s.operators.Exchange(r.Context(), credential, audience)
	if err != nil {
		writeError(w, err)
		return
	}
	answer := machinegen.MachineToken{Token: token.Value, Grn: token.Operator.String(), ExpiresAt: token.ExpiresAt}
	if token.Audience != nil {
		named := token.Audience.String()
		answer.Audience = &named
	}
	writeJSON(w, http.StatusCreated, answer)
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
