// Package machineapi implements the ServerInterface generated from
// api/machine.yaml into api/machine (imported as machinegen): what
// `garam serve machine` serves.
//
// A handler's caller is the machine identity the request's token
// authenticated as and nothing else — no session, no user row, no
// organization membership (ADR-0100). What an operator may
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

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/watch"
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
	authority operator.AuthorityService
	// recheck reads the caller's token dates and registration again on
	// a decision.
	rechecker Rechecker
	proofs    operator.ProofService
	// executions is what a lifecycle authority does to an agent's
	// execution (ADR-0078).
	executions agent.ExecutionService
	// supervisors is how a standalone host's supervisor enrolls and
	// exchanges its credential (ADR-0082, ADR-0100).
	supervisors agent.SupervisorService
	// admissions is agent-enrollment.v1's approval and admission
	// (ADR-0102).
	admissions agent.AdmissionService
	// keys signs each message handed out and publishes the keys it
	// signs with (ADR-0101).
	keys message.Keyring
	// credentialAudit records every credential issued, renewed or
	// refused (ADR-0114); nil records nothing.
	credentialAudit CredentialAudit
}

// Rechecker reads again, for one decision, what admitted the caller's
// identity: [machine.Recheck] satisfies it.
type Rechecker interface {
	Recheck(ctx context.Context, identity machine.Identity) error
}

// Compile-time check that Server satisfies the generated interface.
var _ machinegen.ServerInterface = (*Server)(nil)

// NewServer constructs the machine API server over the services it
// answers from: what an operator does to its agents, how an operator
// exchanges its key, what a machine identity does to its own queue, the
// consent relation that lets a message cross an organization boundary,
// who is watching one work and what it is saying, which groups a
// caller belongs to, what decides whom it may send to, what a hosted
// operator reads its delegated operations back through, what rechecks
// a caller on a decision, what proves a controller's current authority
// to that operator, what activates, inspects and recovers an agent's
// execution, how a standalone host's supervisor enrolls, what prepares
// and admits an agent-enrollment.v1 approval, and what signs each
// message handed out.
func NewServer(agents agent.Service, operators operator.Service, messages message.Service, consents message.ConsentStore, watches watch.Service, groups Groups, evaluator Evaluator, authority operator.AuthorityService, recheck Rechecker, proofs operator.ProofService, executions agent.ExecutionService, supervisors agent.SupervisorService, admissions agent.AdmissionService, keys message.Keyring) *Server {
	return &Server{agents: agents, operators: operators, messages: messages, consents: consents, watches: watches, groups: groups, evaluator: evaluator, authority: authority,
		rechecker: recheck, proofs: proofs, executions: executions, supervisors: supervisors, admissions: admissions, keys: keys}
}

// caller returns the GRN the request's token proved, and writes the
// refusal itself when there is none. It is what a handler needing only
// the identity takes it through; see identityOf for why taking it is
// not optional.
func caller(w http.ResponseWriter, r *http.Request) (grn.GRN, bool) {
	identity, ok := identityOf(w, r)
	return identity.GRN(), ok
}

// identityOf returns the identity the request proved, and writes the
// refusal itself when there is none.
//
// The listener admits a request presenting no token, so that the
// routes authenticated by what else a request carries can be reached.
// That makes this refusal the only one standing between such a request
// and a handler: every handler but those has to take its identity
// through here, through callerCredential, or through parties, and a new
// one that does not is a route an unauthenticated caller reaches.
//
// A request that presented a token which proved nothing is refused
// with the reason the listener read (ADR-0100), so a caller holding a
// voided token learns that rather than that it presented nothing.
func identityOf(w http.ResponseWriter, r *http.Request) (machine.Identity, bool) {
	identity, ok := machine.IdentityOf(r)
	if !ok {
		if err := machine.RefusalOf(r); err != nil {
			writeError(w, err)
			return machine.Identity{}, false
		}
		writeError(w, errorx.New(errorx.KindUnauthenticated, "no machine identity"))
		return machine.Identity{}, false
	}
	return identity, true
}

// callerCredential returns the identity and the token it presented,
// whose serial and dates the execution fence reads (ADR-0080,
// ADR-0100), and writes the refusal itself where there is none. An
// operator's token carries no serial, so it is refused here.
func callerCredential(w http.ResponseWriter, r *http.Request) (machine.Identity, bool) {
	identity, ok := identityOf(w, r)
	if !ok {
		return machine.Identity{}, false
	}
	if identity.Serial() == "" {
		writeError(w, errorx.New(errorx.KindUnauthenticated,
			"this operation reads a credential's serial, and this token carries none"))
		return machine.Identity{}, false
	}
	return identity, true
}
