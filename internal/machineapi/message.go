package machineapi

import (
	"context"
	"encoding/json"
	"net/http"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/wire"
)

// serviceTypeOrg is the GRN service type of an organization, kept as a
// copy for the reason internal/agent keeps one: this surface may not
// import internal/org, and the consent relation is keyed by the
// organizations' GRNs.
const serviceTypeOrg = "org"

// PostMessage stores a message the caller is sending. The sender is
// what the certificate names, and the recipient is whatever the body
// says: garam answers once the message is stored and reports a routing
// failure as a message rather than as a refusal (ADR-0013), so a
// recipient no agent answers to is stored like any other. A recipient
// the sender's policies do not reach is a different answer — nothing
// is stored, because the refusal is deliberate rather than a routing
// failure.
func (s *Server) PostMessage(w http.ResponseWriter, r *http.Request) {
	sender, fence, ok := fenced(w, r)
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
	admitted, err := s.authorizeSend(r.Context(), sender, recipient)
	if err != nil {
		writeError(w, err)
		return
	}
	if !admitted {
		s.refuseCrossing(w, r, sender, recipient, submitted.IdempotencyKey, fence)
		return
	}
	stored, err := s.messages.Post(r.Context(), sender, recipient, submitted.IdempotencyKey, submitted.Body, messageFence(fence))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, messageBody(stored))
}

// refuseCrossing answers a post no consent relation carries across its
// boundary. garam cannot route the message, so the answer is not a
// status: the refusal returns to the sender as an ordinary inbound
// message (ADR-0013, ADR-0035), and the post is answered once that is
// stored, as for any other. It is stored under the intended recipient's
// identity because garam holds none of its own (ADR-0024); the body
// says garam refused, and the key derives from the sender's so a
// retried post is answered with the refusal already stored rather than
// piling up one per retry.
func (s *Server) refuseCrossing(w http.ResponseWriter, r *http.Request, sender, recipient grn.GRN, key string, fence *agent.Fencing) {
	refusal := "garam could not deliver your message to " + recipient.String() +
		": the organizations " + sender.Organization + " and " + recipient.Organization +
		" do not both consent to cross-organization messages"
	stored, err := s.messages.Post(r.Context(), recipient, sender, "consent-refused:"+sender.String()+":"+key, refusal, messageFence(fence))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, messageBody(stored))
}

// authorizeSend reports whether sender may post to recipient. A
// recipient of the sender's own organization is admitted on the policy
// engine's answer; one across the organization boundary is admitted on
// the consent relation alone. A cross-boundary recipient no standing
// consent covers is not an error: admitted is false, and PostMessage
// answers the post with the refusal message ADR-0013 asks for.
//
// Both GRNs are validated here because sendingStatements splices the
// organization identifier into a resource pattern, where a wildcard
// would widen the statement past the organization it was scoped to.
func (s *Server) authorizeSend(ctx context.Context, sender, recipient grn.GRN) (bool, error) {
	if err := sender.Validate(); err != nil {
		return false, err
	}
	if err := recipient.Validate(); err != nil {
		return false, err
	}
	if sender.Organization != recipient.Organization {
		return s.consentStands(ctx, sender, recipient)
	}
	return s.policyAdmits(ctx, sender, recipient)
}

// consentStands reports whether both organizations still hold the
// consent a message crossing between them needs (ADR-0035). The engine
// is not asked: an active policy with no attachment applies to every
// subject of the check, so one unattached Allow in the recipient's
// organization would admit every sender anywhere (ADR-0024).
func (s *Server) consentStands(ctx context.Context, sender, recipient grn.GRN) (bool, error) {
	// The relation is keyed by the organizations' GRNs, which are
	// root-owned (ADR-0001).
	return s.consents.Stands(ctx,
		grn.New(grn.RootOwner, grn.DefaultProject, serviceTypeOrg, sender.Organization),
		grn.New(grn.RootOwner, grn.DefaultProject, serviceTypeOrg, recipient.Organization))
}

// policyAdmits asks the engine whether sender may post to a recipient
// of its own organization, unioning what the sender's identity grants
// with the policies attached to it and to its groups, deny-overrides
// across all three (ADR-0007, ADR-0008). Sender and recipient share an
// organization by here, which is what the engine is owed (ADR-0025).
func (s *Server) policyAdmits(ctx context.Context, sender, recipient grn.GRN) (bool, error) {
	groups, err := s.groups.List(ctx, sender)
	if err != nil {
		return false, err
	}
	allowed, err := s.evaluator.Evaluate(ctx, EvalInput{
		Subject:       sender,
		Object:        recipient,
		Action:        message.ActionSend,
		SubjectGroups: groups,
		Statements:    sendingStatements(sender),
		SubjectOrg:    subjectOrg(sender),
	})
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, errorx.New(errorx.KindPermissionDenied, "sender may not send to this recipient")
	}
	return true, nil
}

// sendingStatements is what a machine identity's own GRN grants it
// before any policy: sending inside its own organization, which is why
// an agent nobody has written a policy for still sends (ADR-0007). A
// policy narrows it with a Deny. The organization segment in the
// pattern is defence in depth and not the boundary, which is the
// consent relation's (ADR-0035).
func sendingStatements(sender grn.GRN) []Statement {
	return []Statement{{
		Sid:      "machine-own-org",
		Effect:   wire.EffectAllow,
		Action:   []string{message.ActionSend},
		Resource: []string{"grn:" + sender.Organization + ":*"},
	}}
}

// subjectOrg is the GRN of the organization the sender belongs to, the
// engine's half of the membership the api server reads for human
// callers (ADR-0038). A machine identity is not an org member, so what
// the engine sees is the org the sender's GRN already names.
func subjectOrg(sender grn.GRN) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, serviceTypeOrg, sender.Organization)
}

// GetMessageStatus answers how far a message the caller sent has got.
// The sender is the identity the connection proved, so the identifier
// in the path reaches nothing on its own: a message another identity
// sent is answered as one that is not there, which is also the answer
// for one the sweep deleted (ADR-0055).
func (s *Server) GetMessageStatus(w http.ResponseWriter, r *http.Request, messageID machinegen.MessageId) {
	sender, fence, ok := fenced(w, r)
	if !ok {
		return
	}
	status, err := s.messages.Status(r.Context(), sender, message.ID(messageID), messageFence(fence))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, statusBody(status))
}

// ClaimMessage hands the caller the next message addressed to the agent
// the path names, waiting for one rather than answering an empty queue
// at once. A caller that is not that agent is refused. The answer
// carries how many are watching the agent, which is what says whether
// the turn's events are worth forwarding (ADR-0022).
func (s *Server) ClaimMessage(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) {
	claimant, recipient, fence, ok := fencedParties(w, r, agentGRN)
	if !ok {
		return
	}
	claim, err := s.messages.Claim(r.Context(), claimant, recipient, messageFence(fence))
	if errorx.IsKind(err, errorx.KindNotFound) {
		// A poll that waited its time and found nothing is the ordinary
		// answer rather than a refusal, so the caller has an empty
		// answer to loop on instead of an error to tell apart. It
		// carries no watcher count either: there is no turn to forward
		// the events of until a message is handed out. The queue has one
		// not-found to answer a claim with, and the kind is what reaches
		// here (stack-go.md §4).
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
	holder, recipient, fence, ok := fencedParties(w, r, agentGRN)
	if !ok {
		return
	}
	claim, err := s.messages.Renew(r.Context(), holder, recipient, message.ClaimID(claimID), messageFence(fence))
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
	holder, recipient, fence, ok := fencedParties(w, r, agentGRN)
	if !ok {
		return
	}
	if err := s.messages.Complete(r.Context(), holder, recipient, message.ClaimID(claimID), messageFence(fence)); err != nil {
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

// fencedParties is parties for a request that writes an agent's
// state, with the execution fence the request presents (ADR-0080).
func fencedParties(w http.ResponseWriter, r *http.Request, agentGRN machinegen.AgentGrn) (identity, subject grn.GRN, fence *agent.Fencing, ok bool) {
	identity, fence, ok = fenced(w, r)
	if !ok {
		return grn.GRN{}, grn.GRN{}, nil, false
	}
	subject, err := grn.Parse(agentGRN)
	if err != nil {
		writeError(w, errorx.Wrap(err, "agent", errorx.KindInvalidArgument))
		return grn.GRN{}, grn.GRN{}, nil, false
	}
	return identity, subject, fence, true
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

// statusBody renders a message's state as the spec's MessageStatus. The
// body is not among the fields: the sender wrote it, and a completed
// message no longer carries one (ADR-0034).
func statusBody(s *message.Status) machinegen.MessageStatus {
	return machinegen.MessageStatus{
		Id:        string(s.ID()),
		State:     machinegen.MessageState(s.State()),
		Attempts:  s.Attempts(),
		CreatedAt: s.CreatedAt(),
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
