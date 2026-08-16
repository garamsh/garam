package machineapi

import (
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
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
