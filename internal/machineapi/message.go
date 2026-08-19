package machineapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/wire"
)

// PostMessage stores a message the caller is sending. The sender is
// what the certificate names, and the recipient is whatever the body
// says: garam answers once the message is stored and reports a routing
// failure as a message rather than as a refusal (ADR-0013), so a
// recipient no agent answers to is stored like any other. A recipient
// the sender's policies do not reach is a different answer — nothing
// is stored, because the refusal is deliberate rather than a routing
// failure.
func (s *Server) PostMessage(w http.ResponseWriter, r *http.Request) {
	sender, ok := caller(w, r)
	if !ok {
		return
	}
	var submitted machinegen.MessageSubmission
	if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
		writeError(w, errorx.Wrap(err, "message", errorx.KindInvalidArgument))
		return
	}
	recipient, err := grn.Parse(submitted.Recipient)
	if err != nil {
		writeError(w, errorx.Wrap(err, "recipient", errorx.KindInvalidArgument))
		return
	}
	if err := s.authorizeSend(r.Context(), sender, recipient); err != nil {
		writeError(w, err)
		return
	}
	stored, err := s.messages.Post(r.Context(), sender, recipient, submitted.IdempotencyKey, submitted.Body)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, messageBody(stored))
}

// authorizeSend refuses a recipient of another organization outright,
// and asks the engine about every other one. The engine unions what the
// sender's own identity grants with the policies its organization
// attached to it and to the groups it belongs to, deny-overrides across
// all three (ADR-0007, ADR-0008).
//
// The groups are resolved after the organization comparison and not
// before. By then the sender and the recipient share an organization,
// which is both the one whose policies the engine loads and the one an
// attachment may not leave, so every subject this call hands over —
// the sender and each of its groups — belongs to the organization
// owning the object, which is what a caller owes the engine (ADR-0025).
//
// The organization boundary is a precondition and not a policy
// question, because no statement this listener supplies can hold it.
// The engine resolves the policies of the organization owning the
// object, and an active policy with no attachment applies to every
// subject of that check — so one unattached Allow in the recipient's
// organization admits every sender anywhere, whatever resource pattern
// arrives beside it, and a document written `Action: ["*"]` before
// message:send existed names it in advance (ADR-0024). The api server
// holds the same line by making membership of the object's
// organization a precondition of every check; this listener has no
// membership to read, so it compares the organization segments
// (ADR-0001).
//
// Both GRNs reached a handler through grn.Parse, and this is where one
// stops being inert: sendingStatements splices the organization
// identifier into a resource pattern, where a wildcard would widen the
// statement past the organization it was scoped to.
func (s *Server) authorizeSend(ctx context.Context, sender, recipient grn.GRN) error {
	if err := sender.Validate(); err != nil {
		return err
	}
	if err := recipient.Validate(); err != nil {
		return err
	}
	if sender.Organization != recipient.Organization {
		return errorx.New(errorx.KindPermissionDenied, "recipient is in another organization")
	}
	groups, err := s.groups.List(ctx, sender)
	if err != nil {
		return err
	}
	allowed, err := s.evaluator.Evaluate(ctx, EvalInput{
		Subject:       sender,
		Object:        recipient,
		Action:        message.ActionSend,
		SubjectGroups: groups,
		Statements:    sendingStatements(sender),
	})
	if err != nil {
		return err
	}
	if !allowed {
		return errorx.New(errorx.KindPermissionDenied, "sender may not send to this recipient")
	}
	return nil
}

// sendingStatements is what a machine identity's own GRN grants it
// before any policy: sending inside its own organization. It is this
// listener's half of the union the engine evaluates, as the
// role-derived statements are the api server's (ADR-0007), and it is
// why an agent nobody has written a policy for still sends.
//
// A policy narrows it with a Deny, which is how every grant in this
// model is narrowed. The organization segment in the pattern is
// defence in depth rather than the boundary: authorizeSend has already
// refused every recipient it would exclude, and a pattern could not
// refuse them anyway.
func sendingStatements(sender grn.GRN) []Statement {
	return []Statement{{
		Sid:      "machine-own-org",
		Effect:   wire.EffectAllow,
		Action:   []string{message.ActionSend},
		Resource: []string{"grn:" + sender.Organization + ":*"},
	}}
}

// ClaimMessage hands the caller the next message addressed to the agent
// the path names, waiting for one rather than answering an empty queue
// at once. A caller that is not that agent is refused. The answer
// carries how many are watching the agent, which is what says whether
// the turn's events are worth forwarding (ADR-0022).
func (s *Server) ClaimMessage(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) {
	claimant, recipient, ok := parties(w, r, agentGRN)
	if !ok {
		return
	}
	claim, err := s.messages.Claim(r.Context(), claimant, recipient)
	if errors.Is(err, message.ErrNoMessageReady) {
		// A poll that waited its time and found nothing is the ordinary
		// answer rather than a refusal, so the caller has an empty
		// answer to loop on instead of an error to tell apart. It
		// carries no watcher count either: there is no turn to forward
		// the events of until a message is handed out.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	// The count is read once the message is claimed, so it is the count
	// at the moment the turn starts rather than at the start of a wait
	// that may have run for its whole length.
	writeJSON(w, http.StatusOK, claimBody(claim, s.watchers(r.Context(), claimant, recipient)))
}

// RenewClaim extends the caller's hold on a message it is still working
// on and answers with the hold as it now stands, and with how many are
// watching the agent now.
func (s *Server) RenewClaim(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, claimID machinegen.ClaimId) {
	holder, recipient, ok := parties(w, r, agentGRN)
	if !ok {
		return
	}
	claim, err := s.messages.Renew(r.Context(), holder, recipient, message.ClaimID(claimID))
	if err != nil {
		writeError(w, err)
		return
	}
	// A renewal is what a caller makes while the turn is running, so it
	// is where a person who opened the view mid-turn is first seen, and
	// where one who has left stops being forwarded to (ADR-0022).
	writeJSON(w, http.StatusOK, claimBody(claim, s.watchers(r.Context(), holder, recipient)))
}

// CompleteClaim reports the claimed message delivered.
func (s *Server) CompleteClaim(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn, claimID machinegen.ClaimId) {
	holder, recipient, ok := parties(w, r, agentGRN)
	if !ok {
		return
	}
	if err := s.messages.Complete(r.Context(), holder, recipient, message.ClaimID(claimID)); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// parties returns the identity the connection authenticated as and the
// agent the path names, and writes the refusal itself for a request
// carrying neither. Whether one may act on the other's queue is
// internal/message's to decide, not this boundary's.
func parties(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) (identity, recipient grn.GRN, ok bool) {
	identity, ok = caller(w, r)
	if !ok {
		return grn.GRN{}, grn.GRN{}, false
	}
	recipient, err := grn.Parse(agentGRN)
	if err != nil {
		writeError(w, errorx.Wrap(err, "agent", errorx.KindInvalidArgument))
		return grn.GRN{}, grn.GRN{}, false
	}
	return identity, recipient, true
}

// claimBody renders a claim as the spec's Claim, with the number of
// streams garam is holding for the recipient. The field is always
// written: zero is the ordinary answer, and a shape that changed with it
// would make an agent nobody watches the exceptional case rather than
// the cheap one (ADR-0022).
func claimBody(c *message.Claim, watchers int) machinegen.Claim {
	return machinegen.Claim{
		Id:        string(c.ID()),
		ExpiresAt: c.ExpiresAt(),
		Attempt:   c.Message().Attempts(),
		Watchers:  watchers,
		Message:   messageBody(c.Message()),
	}
}

// messageBody renders a stored message as the spec's Message.
func messageBody(m *message.Message) machinegen.Message {
	return machinegen.Message{
		Id:             string(m.ID()),
		Recipient:      m.Recipient().String(),
		Sender:         m.Sender().String(),
		IdempotencyKey: m.IdempotencyKey(),
		Body:           m.Body(),
		CreatedAt:      m.CreatedAt(),
	}
}
