// Package machineapi implements the ServerInterface generated from
// api/machine.yaml into api/machine (imported as machinegen): what
// `garam serve machine` serves.
//
// A handler's caller is the machine identity the connection
// authenticated as and nothing else — no session, no user row, no
// organization membership (ADR-0014, ADR-0015). What an operator may
// reach is bounded by the assignments it holds, which `internal/agent`
// decides; this package reads the caller off the request, parses what
// the path names, and renders the answer.
package machineapi

import (
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
)

// Server implements machinegen.ServerInterface.
type Server struct {
	agents agent.Service
}

// Compile-time check that Server satisfies the generated interface.
var _ machinegen.ServerInterface = (*Server)(nil)

// NewServer constructs the machine API server over the agent service
// it answers from.
func NewServer(agents agent.Service) *Server {
	return &Server{agents: agents}
}

// caller returns the GRN the connection authenticated as, and writes
// the refusal itself when there is none. Every request the machine
// listener serves carries an identity, so the refusal is what a request
// that reached a handler any other way gets.
func caller(w http.ResponseWriter, r *http.Request) (grn.GRN, bool) {
	identity, ok := machine.IdentityOf(r)
	if !ok {
		writeError(w, errorx.New(errorx.KindUnauthenticated, "no machine identity"))
		return grn.GRN{}, false
	}
	return identity.GRN(), true
}
