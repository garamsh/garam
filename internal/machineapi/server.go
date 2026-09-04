// Package machineapi implements the ServerInterface generated from
// api/machine.yaml into api/machine (imported as machinegen): what
// `garam serve machine` serves.
//
// A handler's caller is the machine identity the connection
// authenticated as and nothing else — no session, no user row, no
// organization membership (ADR-0014, ADR-0015). What an operator may
// reach is bounded by the assignments it holds, which `internal/agent`
// decides, and whose queue a caller may act on is `internal/message`'s
// to decide; this package reads the caller off the request, parses what
// the path names, and renders the answer.
//
// What a caller may send is decided here instead, because it is a
// policy question: the answer comes from the authz engine over a seam
// this package declares, and a boundary this one composes may not reach
// past it to another process. That question carries the groups the
// caller belongs to, read over a seam of the same shape, because a
// policy attached to a group decides what its members send (ADR-0008).
// It is never asked across an organization boundary: there the consent
// relation decides alone (ADR-0035), read from the message domain that
// owns it.
package machineapi

import (
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/watch"
)

// Server implements machinegen.ServerInterface.
type Server struct {
	agents    agent.Service
	operators operator.Service
	messages  message.Service
	consents  message.ConsentStore
	watches   watch.Service
	groups    Groups
	evaluator Evaluator
}

// Compile-time check that Server satisfies the generated interface.
var _ machinegen.ServerInterface = (*Server)(nil)

// NewServer constructs the machine API server over the services it
// answers from: what an operator does to its agents, what an operator
// does to its own certificate, what a machine identity does to its own
// queue, the consent relation that lets a message cross an organization
// boundary, who is watching one work and what it is saying, which
// groups a caller belongs to, and what decides whom it may send to.
func NewServer(agents agent.Service, operators operator.Service, messages message.Service, consents message.ConsentStore, watches watch.Service, groups Groups, evaluator Evaluator) *Server {
	return &Server{agents: agents, operators: operators, messages: messages, consents: consents, watches: watches, groups: groups, evaluator: evaluator}
}

// caller returns the GRN the connection authenticated as, and writes
// the refusal itself when there is none. It is what a handler needing
// only the identity takes it through; see callerCertificate for why
// taking it is not optional.
func caller(w http.ResponseWriter, r *http.Request) (grn.GRN, bool) {
	identity, ok := callerCertificate(w, r)
	return identity.GRN(), ok
}

// callerCertificate returns the identity and the certificate it
// authenticated with, and writes the refusal itself when there is none.
//
// The listener admits a connection whose certificate does not
// authenticate, so that an operator with no certificate yet can reach
// the enrollment route (ADR-0064). That makes this refusal the only one
// standing between such a connection and a handler: every handler but
// EnrollOperator has to take its identity through here, or through
// parties, and a new one that does not is a route an unauthenticated
// caller reaches.
//
// One route reads the certificate rather than the GRN alone: what may
// replace a certificate is decided by the certificate presented
// (ADR-0046).
func callerCertificate(w http.ResponseWriter, r *http.Request) (machine.Identity, bool) {
	identity, ok := machine.IdentityOf(r)
	if !ok {
		writeError(w, errorx.New(errorx.KindUnauthenticated, "no machine identity"))
		return machine.Identity{}, false
	}
	return identity, true
}
