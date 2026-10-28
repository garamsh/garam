package machineapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	openapi_types "github.com/oapi-codegen/runtime/types"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/audit"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/machine"
)

// bootstrapScheme is the Authorization scheme a standalone host spends
// its approval with (ADR-0082).
const bootstrapScheme = "Garam-Bootstrap "

// EnrollStandaloneAgent spends an administrator's approval of a
// standalone host and answers its supervisor's credential (ADR-0100).
// It is reached without a token: the bootstrap secret is what
// authenticates it.
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
	})
	if err != nil {
		// A refused enrollment names no subject garam can attribute it
		// to: the bootstrap secret is what names one (ADR-0114).
		writeError(w, withinBudget(r, err))
		return
	}
	// Every answer issues the credential afresh, a replay's included
	// (ADR-0100).
	s.auditCredential(r, audit.CredentialIssued(result.Supervisor, audit.CredentialSupervisorCredential,
		audit.ActionIssue, "", audit.Validity{}, false))
	out := machinegen.StandaloneEnrollment{
		Grn: result.Agent.String(), SupervisorGrn: result.Supervisor.String(),
		Epoch: strconv.Itoa(result.Epoch), Lineage: openapi_types.UUID(result.Lineage),
		Credential: result.Credential, ServerRootPem: result.ServerRootPEM,
		Placement: machinegen.StandaloneEnrollmentPlacement(result.Placement),
	}
	if replaced := result.ReplacesActivation; replaced != nil {
		id := openapi_types.UUID(*replaced)
		out.ReplacesActivationId = &id
	}
	writeJSON(w, createdOrReplayed(created), out)
}

// standaloneContract stamps the standalone enrollment contract version
// on the answer and refuses a request that asked for another.
func standaloneContract(w http.ResponseWriter, version machinegen.StandaloneContractVersion) bool {
	w.Header().Set("Garam-Contract-Version", agent.CredentialStandaloneContractVersion)
	if version != agent.CredentialStandaloneContractVersion {
		writeError(w, agent.ErrUnsupportedStandaloneContract)
		return false
	}
	return true
}

// supervisorRoutes is everything a supervisor reaches (ADR-0082,
// ADR-0100): its own identity, the execution routes it holds lifecycle
// authority on, and the mint of its one agent's tokens. They are the
// router's patterns, so a route added later is outside the set until
// named here.
var supervisorRoutes = map[string]bool{
	http.MethodGet + " /identity":                                              true,
	http.MethodPost + " /agents/{agent}/activations":                           true,
	http.MethodPost + " /agents/{agent}/activations/{activation}/deactivation": true,
	http.MethodPost + " /agents/{agent}/execution/introspection":               true,
	http.MethodPost + " /agents/{agent}/tokens":                                true,
}

// clientAssertionRoute is the router's pattern for the client
// assertion exchange (ADR-0102), which refuses every credential beside
// its assertion itself, in the OAuth error form its caller reads.
const clientAssertionRoute = http.MethodPost + " /oauth/token"

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
			r.Pattern != clientAssertionRoute {
			writeError(w, errorx.New(errorx.KindPermissionDenied, "a supervisor reaches its lifecycle routes alone"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
