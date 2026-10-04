package machineapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
)

// bootstrapScheme is the Authorization scheme a standalone host spends
// its approval with (ADR-0082).
const bootstrapScheme = "Garam-Bootstrap "

// EnrollStandaloneAgent spends an administrator's approval of a
// standalone host and answers its supervisor's and its agent's first
// certificates. Like EnrollOperator it is reached without a client
// certificate: the bootstrap secret is what authenticates it.
func (s *Server) EnrollStandaloneAgent(w http.ResponseWriter, r *http.Request, params machinegen.EnrollStandaloneAgentParams) {
	if !standaloneContract(w, params.StandaloneContractVersion) {
		return
	}
	r, cancel := lifecycleBudget(r)
	defer cancel()
	secret, ok := strings.CutPrefix(r.Header.Get("Authorization"), bootstrapScheme)
	if !ok || secret == "" {
		writeError(w, agent.ErrBootstrapRefused)
		return
	}
	// The digest an identical retry is matched by is of these exact
	// bytes, so they are kept as received and decoded from the copy.
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, errorx.Wrap(err, "standalone enrollment", errorx.KindInvalidArgument))
		return
	}
	var body machinegen.StandaloneEnrollmentRequest
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, errorx.Wrap(err, "standalone enrollment", errorx.KindInvalidArgument))
		return
	}
	requestID, err := agent.ParseRequestID(body.RequestId)
	if err != nil {
		writeError(w, err)
		return
	}
	result, created, err := s.supervisors.Enroll(r.Context(), agent.EnrollmentInput{
		Secret: secret, RequestID: requestID, Body: raw,
		SupervisorCertificateRequestPEM: []byte(body.SupervisorCertificateRequestPem),
		AgentCertificateRequestPEM:      []byte(body.AgentCertificateRequestPem),
	})
	if err != nil {
		writeCertificateRequestError(w, r, err)
		return
	}
	writeJSON(w, createdOrReplayed(created), machinegen.StandaloneEnrollment{
		Grn: result.Agent.String(), SupervisorGrn: result.Supervisor.String(),
		Epoch: strconv.Itoa(result.Epoch), Lineage: openapi_types.UUID(result.Lineage),
		SupervisorCertificatePem: result.SupervisorCertificatePEM, AgentCertificatePem: result.AgentCertificatePEM,
		IssuerPem: result.IssuerPEM, ServerRootPem: result.ServerRootPEM,
	})
}

// RenewSupervisorCertificate issues the calling supervisor's next
// certificate, or answers again the one an identical renewal of the
// presented certificate issued. The caller's validity and
// registration are read again first, so a revoked supervisor renews
// nothing on a connection it opened before.
func (s *Server) RenewSupervisorCertificate(w http.ResponseWriter, r *http.Request, params machinegen.RenewSupervisorCertificateParams) {
	if !standaloneContract(w, params.StandaloneContractVersion) {
		return
	}
	identity, ok := callerCertificate(w, r)
	if !ok || !s.recheck(w, r, identity) {
		return
	}
	var body machinegen.SupervisorRenewalRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "supervisor renewal", errorx.KindInvalidArgument))
		return
	}
	requestID, err := agent.ParseRequestID(body.RequestId)
	if err != nil {
		writeError(w, err)
		return
	}
	presented := agent.Credential{Serial: identity.Serial(), NotBefore: identity.NotBefore(), NotAfter: identity.NotAfter()}
	issued, created, err := s.supervisors.RenewSupervisor(r.Context(), identity.GRN(), presented, requestID, []byte(body.CertificateRequestPem))
	if err != nil {
		writeCertificateRequestError(w, r, err)
		return
	}
	writeJSON(w, createdOrReplayed(created), machinegen.InitialCertificate{
		CertificatePem: issued.CertificatePEM, IssuerPem: issued.IssuerPEM,
		ServerRootPem: issued.ServerRootPEM, NotAfter: issued.NotAfter.UTC(),
	})
}

// writeCertificateRequestError answers 422 for a certificate request
// that proves no usable key, and the error's own status otherwise.
func writeCertificateRequestError(w http.ResponseWriter, r *http.Request, err error) {
	err = withinBudget(r, err)
	if errors.Is(err, agent.ErrInvalidCertificateRequest) {
		writeErrorStatus(w, http.StatusUnprocessableEntity, err)
		return
	}
	writeError(w, err)
}

// standaloneContract stamps the standalone enrollment contract version
// on the answer and refuses a request that asked for another.
func standaloneContract(w http.ResponseWriter, version machinegen.StandaloneContractVersion) bool {
	w.Header().Set("Garam-Contract-Version", agent.StandaloneContractVersion)
	if version != agent.StandaloneContractVersion {
		writeError(w, agent.ErrUnsupportedStandaloneContract)
		return false
	}
	return true
}
