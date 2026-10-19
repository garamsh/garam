package machineapi

import (
	"encoding/json"
	"net/http"
	"strings"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/audit"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/machine"
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

// ExchangeToken answers a short-lived token for the standalone host's
// supervisor the presented credential belongs to (ADR-0100). An
// operator exchanges a key with ExchangeClientAssertion instead
// (ADR-0102, ADR-0117).
//
// It reads no identity off the connection: what authenticates the
// request is the credential in its Authorization header, which an
// approved enrollment answered.
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
	if body.Audience != nil {
		writeError(w, agent.ErrSupervisorAudience)
		return
	}
	token, err := s.supervisors.Exchange(r.Context(), credential)
	if err != nil {
		writeError(w, err)
		return
	}
	machine.ReportIssuance(r, machine.Issuance{Subject: token.Supervisor, Until: token.ExpiresAt})
	writeJSON(w, http.StatusCreated, machinegen.MachineToken{
		Token: token.Value, Grn: token.Supervisor.String(), ExpiresAt: token.ExpiresAt,
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
		s.refusedCredential(r, identity.GRN(), audit.CredentialOperatorCertificate, audit.ActionRenew, err)
		writeError(w, err)
		return
	}
	s.issuedCertificate(r, identity.GRN(), audit.CredentialOperatorCertificate, audit.ActionRenew,
		string(certificate.CertificatePEM), false)
	writeJSON(w, http.StatusCreated, machinegen.MachineCertificate{
		Grn:            identity.GRN().String(),
		CertificatePem: string(certificate.CertificatePEM),
		PrivateKeyPem:  string(certificate.PrivateKeyPEM),
		IssuerPem:      string(certificate.IssuerPEM),
		ServerRootPem:  string(certificate.ServerRootPEM),
		NotAfter:       certificate.NotAfter,
	})
}
