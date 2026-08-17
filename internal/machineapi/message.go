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

// authorizeSend enforces the policies deciding whether sender may send
// to recipient. The engine unions what the sender's own identity grants
// with the policies its organization attached to it, deny-overrides
// across both (ADR-0007, ADR-0008).
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
	allowed, err := s.evaluator.Evaluate(ctx, EvalInput{
		Subject:    sender,
		Object:     recipient,
		Action:     message.ActionSend,
		Statements: sendingStatements(sender),
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
// before any policy: sending inside its own organization and nowhere
// else. It is this listener's half of the union the engine evaluates,
// as the role-derived statements are the api server's (ADR-0007), and
// it is why an agent nobody has written a policy for still sends.
//
// A policy narrows it with a Deny, which is how every grant in this
// model is narrowed. An organization's identifier is the organization
// segment of every resource it owns (ADR-0001), so the pattern is the
// tenancy boundary the recipient comparison already draws on this
// listener.
func sendingStatements(sender grn.GRN) []Statement {
	return []Statement{{
		Sid:      "machine-own-org",
		Effect:   EffectAllow,
		Action:   []string{message.ActionSend},
		Resource: []string{"grn:" + sender.Organization + ":*"},
	}}
}

// ClaimMessage hands the caller the next message addressed to the agent
// the path names, waiting for one rather than answering an empty queue
// at once. A caller that is not that agent is refused.
func (s *Server) ClaimMessage(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) {
	claimant, recipient, ok := parties(w, r, agentGRN)
	if !ok {
		return
	}
	claim, err := s.messages.Claim(r.Context(), claimant, recipient)
	if errors.Is(err, message.ErrNoMessageReady) {
		// A poll that waited its time and found nothing is the ordinary
		// answer rather than a refusal, so the caller has an empty
		// answer to loop on instead of an error to tell apart.
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, claimBody(claim))
}

// RenewClaim extends the caller's hold on a message it is still working
// on and answers with the hold as it now stands.
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
	writeJSON(w, http.StatusOK, claimBody(claim))
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

// claimBody renders a claim as the spec's Claim.
func claimBody(c *message.Claim) machinegen.Claim {
	return machinegen.Claim{
		Id:        string(c.ID()),
		ExpiresAt: c.ExpiresAt(),
		Attempt:   c.Message().Attempts(),
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
