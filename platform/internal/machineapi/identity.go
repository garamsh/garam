package machineapi

import (
	"encoding/json"
	"net/http"
	"strings"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/operator"
)

// GetIdentity answers with the GRN the request's token authenticated
// as. It is what tells a token that works from one that was never
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
// It reads no identity off the request: what authenticates it is the
// credential in its Authorization header, which an approved
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
