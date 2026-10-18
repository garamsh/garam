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
	"github.com/garamsh/garam/internal/machine"
)

// bootstrapScheme is the Authorization scheme a standalone host spends
// its approval with (ADR-0082).
const bootstrapScheme = "Garam-Bootstrap "

// EnrollStandaloneAgent spends an administrator's approval of a
// standalone host and answers its supervisor's and its agent's first
// certificates under standalone-enrollment.v1, or the supervisor's
// credential under v2 (ADR-0100). Like EnrollOperator it is reached
// without a client certificate: the bootstrap secret is what
// authenticates it, under either value, so the value the request names
// decides the contract, and the approval refuses the one it was not
// made for.
func (s *Server) EnrollStandaloneAgent(w http.ResponseWriter, r *http.Request, params machinegen.EnrollStandaloneAgentParams) {
	version := params.StandaloneContractVersion
	if version != agent.StandaloneContractVersion && version != agent.CredentialStandaloneContractVersion {
		w.Header().Set("Garam-Contract-Version", agent.StandaloneContractVersion)
		writeError(w, agent.ErrUnsupportedEnrollmentStandaloneContract)
		return
	}
	w.Header().Set("Garam-Contract-Version", version)
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
	requests, err := certificateRequestsOf(version, body)
	if err != nil {
		writeError(w, err)
		return
	}
	result, created, err := s.supervisors.Enroll(r.Context(), agent.EnrollmentInput{
		Secret: secret, RequestID: requestID, Body: raw, Requests: requests,
	})
	if err != nil {
		writeCertificateRequestError(w, r, err)
		return
	}
	out := machinegen.StandaloneEnrollment{
		Grn: result.Agent.String(), SupervisorGrn: result.Supervisor.String(),
		Epoch: strconv.Itoa(result.Epoch), Lineage: openapi_types.UUID(result.Lineage),
		ServerRootPem: result.ServerRootPEM, Placement: machinegen.StandaloneEnrollmentPlacement(result.Placement),
	}
	if c := result.Certificates; c != nil {
		out.SupervisorCertificatePem, out.AgentCertificatePem = &c.SupervisorCertificatePEM, &c.AgentCertificatePEM
		out.IssuerPem = &c.IssuerPEM
	}
	if result.Credential != "" {
		out.Credential = &result.Credential
	}
	if replaced := result.ReplacesActivation; replaced != nil {
		id := openapi_types.UUID(*replaced)
		out.ReplacesActivationId = &id
	}
	writeJSON(w, createdOrReplayed(created), out)
}

// certificateRequestsOf parses the certificate requests an enrollment
// carries: both under standalone-enrollment.v1, and neither under v2,
// which signs nothing (ADR-0100).
func certificateRequestsOf(version string, body machinegen.StandaloneEnrollmentRequest) (*agent.CertificateRequests, error) {
	if version == agent.CredentialStandaloneContractVersion {
		if body.SupervisorCertificateRequestPem != nil || body.AgentCertificateRequestPem != nil {
			return nil, errorx.New(errorx.KindInvalidArgument,
				"under "+agent.CredentialStandaloneContractVersion+" an enrollment signs nothing, and certificate requests are refused")
		}
		return nil, nil
	}
	if body.SupervisorCertificateRequestPem == nil || body.AgentCertificateRequestPem == nil {
		return nil, errorx.New(errorx.KindInvalidArgument,
			"under "+agent.StandaloneContractVersion+" an enrollment carries both certificate requests")
	}
	return &agent.CertificateRequests{
		SupervisorPEM: []byte(*body.SupervisorCertificateRequestPem), AgentPEM: []byte(*body.AgentCertificateRequestPem),
	}, nil
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

// supervisorRoutes is everything a supervisor reaches (ADR-0082): its
// own identity, its own renewal and the execution routes it holds
// lifecycle authority on. They are the router's patterns, so a route
// added later is outside the set until named here.
var supervisorRoutes = map[string]bool{
	http.MethodGet + " /identity":                                              true,
	http.MethodPost + " /supervisors/self/certificate":                         true,
	http.MethodPost + " /agents/{agent}/activations":                           true,
	http.MethodPost + " /agents/{agent}/activations/{activation}/deactivation": true,
	http.MethodPost + " /agents/{agent}/execution/introspection":               true,
}

// supervisorTokenRoutes is what a supervisor reaches beside
// supervisorRoutes when it presents its own token: the mint of its one
// agent's tokens (ADR-0100). A supervisor's certificate reaches no more
// than it did under standalone-enrollment.v1.
var supervisorTokenRoutes = map[string]bool{
	http.MethodPost + " /agents/{agent}/tokens": true,
}

// ConfineSupervisors refuses a supervisor's request to any route
// outside supervisorRoutes before its handler runs. It is the one place
// a supervisor's reach is decided: the handlers behind it, the message
// routes included, answer a machine identity without asking which kind
// it is, and a supervisor is no organization-wide sender. It reads the
// route the router matched, so it runs inside the router.
func ConfineSupervisors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := machine.IdentityOf(r)
		if ok && identity.GRN().ServiceType == agent.ServiceTypeSupervisor && !supervisorRoutes[r.Pattern] &&
			(identity.Method() != machine.MethodToken || !supervisorTokenRoutes[r.Pattern]) &&
			r.Pattern != clientAssertionRoute {
			writeError(w, errorx.New(errorx.KindPermissionDenied, "a supervisor reaches its lifecycle routes alone"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
