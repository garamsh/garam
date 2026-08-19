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
	"context"
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/watch"
)

// Groups reports which of its organization's groups a machine identity
// belongs to, so a policy attached to one of them decides what that
// identity sends (ADR-0008). Group membership is org data, and this
// surface holds no store: the composition root satisfies this seam from
// internal/org, as it satisfies the trust root internal/machine reads.
type Groups interface {
	List(ctx context.Context, subject grn.GRN) ([]grn.GRN, error)
}

// Server implements machinegen.ServerInterface.
type Server struct {
	agents    agent.Service
	messages  message.Service
	consents  message.ConsentStore
	watches   watch.Service
	groups    Groups
	evaluator Evaluator
}

// Compile-time check that Server satisfies the generated interface.
var _ machinegen.ServerInterface = (*Server)(nil)

// NewServer constructs the machine API server over the services it
// answers from: what an operator does to its agents, what a machine
// identity does to its own queue, the consent relation that lets a
// message cross an organization boundary, who is watching one work and
// what it is saying, which groups a caller belongs to, and what decides
// whom it may send to.
func NewServer(agents agent.Service, messages message.Service, consents message.ConsentStore, watches watch.Service, groups Groups, evaluator Evaluator) *Server {
	return &Server{agents: agents, messages: messages, consents: consents, watches: watches, groups: groups, evaluator: evaluator}
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
