package machineapi

import (
	"encoding/json"
	"net/http"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/audit"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/operator"
)

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
	caller, subject, fence, ok := fencedParties(w, r, agentGRN)
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
	if fence == nil {
		// Not an agent: a runtime report is the agent's own, and the
		// domain refuses another caller before anything is admitted.
		fence = &agent.Fencing{Agent: caller}
	}
	if err := s.agents.ReportRuntimeState(r.Context(), caller, subject, report.Epoch, state, *fence); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MintAgentToken answers a token for an agent the calling operator
// holds at the current epoch, which the agent authenticates with in
// place of a certificate (ADR-0100). The assignment, the agent's
// admission and the epoch and lineage the token is minted in are the
// agent domain's to decide; this reads the caller, the path and the
// audience asked for.
func (s *Server) MintAgentToken(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, params machinegen.MintAgentTokenParams) {
	w.Header().Set("Garam-Contract-Version", operator.TokenContractVersion)
	if params.TokenContractVersion != operator.TokenContractVersion {
		writeError(w, operator.ErrUnsupportedTokenContract)
		return
	}
	caller, subject, ok := parties(w, r, agentGRN)
	if !ok {
		return
	}
	var body machinegen.TokenExchange
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, errorx.Wrap(err, "token request", errorx.KindInvalidArgument))
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
	token, err := s.agents.MintAccessToken(r.Context(), caller, subject, audience)
	if err != nil {
		s.refusedCredential(r, subject, audit.CredentialAgentToken, audit.ActionIssue, err)
		writeError(w, err)
		return
	}
	s.auditCredential(r, audit.CredentialIssued(token.Agent, audit.CredentialAgentToken, audit.ActionIssue, "",
		audit.Validity{NotBefore: token.IssuedAt, NotAfter: token.ExpiresAt}, false))
	machine.ReportIssuance(r, machine.Issuance{Subject: token.Agent, Audience: token.Audience != nil, Until: token.ExpiresAt})
	answer := machinegen.AgentToken{Token: token.Value, Grn: token.Agent.String(), Epoch: token.Epoch,
		ExpiresAt: token.ExpiresAt, ServerRootPem: string(token.ServerRootPEM)}
	if token.Audience != nil {
		named := token.Audience.String()
		answer.Audience = &named
	}
	writeJSON(w, http.StatusCreated, answer)
}
