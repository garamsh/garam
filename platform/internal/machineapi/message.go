package machineapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/message"
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
	admitted, err := s.authorizeSend(r.Context(), sender, recipient, messageFence(fence))
	if err != nil {
		writeError(w, err)
		return
	}
	from, to, key, body, author := sender, recipient, submitted.IdempotencyKey, submitted.Body, message.AuthorSender
	if !admitted {
		from, to, key, body, author = crossingRefusal(sender, recipient, key)
	}
	stored, err := s.messages.Post(r.Context(), from, to, key, body, author, messageFence(fence))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, messageBody(stored))
}

// PostOutboxMessage stores a durable reply under the creation instant
// its outbox entry holds (ADR-0083). It is PostMessage's write behind a
// contract version, checked before anything else is read, and a
// budget, which the store judges at its insert.
func (s *Server) PostOutboxMessage(w http.ResponseWriter, r *http.Request, params machinegen.PostOutboxMessageParams) {
	if !served(w, params.DeliveryContractVersion, deliveryValues) {
		return
	}
	sender, fence, ok := fenced(w, r)
	if !ok {
		return
	}
	var submitted machinegen.OutboxSubmission
	if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
		writeError(w, errorx.Wrap(err, "message", errorx.KindInvalidArgument))
		return
	}
	created, err := parseCreatedAt(submitted.CreatedAt)
	if err != nil {
		writeError(w, err)
		return
	}
	recipient, err := grn.Parse(submitted.Recipient)
	if err != nil {
		writeError(w, errorx.Wrap(err, "recipient", errorx.KindInvalidArgument))
		return
	}
	admitted, err := s.authorizeSend(r.Context(), sender, recipient, messageFence(fence))
	if err != nil {
		writeError(w, err)
		return
	}
	from, to, key, body, author := sender, recipient, submitted.IdempotencyKey, submitted.Body, message.AuthorSender
	if !admitted {
		from, to, key, body, author = crossingRefusal(sender, recipient, key)
	}
	stored, err := s.messages.PostWithinBudget(r.Context(), from, to, key, body, author, created, messageFence(fence))
	if errors.Is(err, message.ErrSubmissionExpired) {
		// The shared mapping answers the kind 504, which reads as a
		// timeout a retry outlasts; the sentinel, not the kind, picks
		// 410, so a timeout never reads as expiry.
		writeErrorStatus(w, http.StatusGone, err)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, messageBody(stored))
}

// parseCreatedAt reads an outbox entry's creation instant. A missing
// one is refused rather than taken as now, so no receipt time ever
// stands in for it.
func parseCreatedAt(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errorx.New(errorx.KindInvalidArgument, "createdAt is required")
	}
	created, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, errorx.New(errorx.KindInvalidArgument, "createdAt is not an RFC 3339 instant")
	}
	return created, nil
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
	// Every hand-out is signed (ADR-0101). The key is opened before the
	// wait, and the claim is signed inside the transaction that takes
	// it, so a claim that cannot be signed spends none of the message's
	// attempts on an answer the caller never got (ADR-0104).
	signer, err := s.keys.Signer(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	claim, signed, err := s.messages.Claim(r.Context(), claimant, recipient, messageFence(fence), signer)
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
	body := claimBody(claim, s.watchers(r.Context(), claimant, recipient))
	body.Content, body.Signature = &signed.Content, &signed.Signature
	writeJSON(w, http.StatusOK, body)
}

// GetMessageSigningKeys publishes the keys garam signs messages with,
// as a JWK Set (ADR-0101). It takes no identity: a verifier anywhere
// may read it, and it carries nothing a key set does not.
func (s *Server) GetMessageSigningKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.keys.PublishedKeys(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	set := machinegen.MessageSigningKeySet{Keys: make([]machinegen.MessageSigningKey, 0, len(keys))}
	for _, k := range keys {
		set.Keys = append(set.Keys, machinegen.MessageSigningKey{
			Kid: string(k.ID),
			Kty: machinegen.EC,
			Crv: machinegen.P256,
			Alg: machinegen.ES256,
			Use: machinegen.Sig,
			X:   base64.RawURLEncoding.EncodeToString(k.X),
			Y:   base64.RawURLEncoding.EncodeToString(k.Y),
		})
	}
	w.Header().Set("Cache-Control", "max-age="+strconv.Itoa(int(message.KeySetMaxAge.Seconds())))
	writeJSONAs(w, http.StatusOK, "application/jwk-set+json", set)
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
		Id:        string(c.ID),
		ExpiresAt: c.ExpiresAt,
		Attempt:   c.Message.Attempts,
		Watchers:  watchers,
		Message:   messageBody(c.Message),
	}
}

// statusBody renders a message's state as the spec's MessageStatus. The
// body is not among the fields: the sender wrote it, and a completed
// message no longer carries one (ADR-0034).
func statusBody(s *message.Status) machinegen.MessageStatus {
	return machinegen.MessageStatus{
		Id:        string(s.ID),
		State:     machinegen.MessageState(s.State),
		Attempts:  s.Attempts,
		CreatedAt: s.CreatedAt,
	}
}

// messageBody renders a stored message as the spec's Message.
func messageBody(m *message.Message) machinegen.Message {
	return machinegen.Message{
		Id:             string(m.ID),
		Recipient:      m.Recipient.String(),
		Sender:         m.Sender.String(),
		IdempotencyKey: m.IdempotencyKey,
		Body:           m.Body,
		CreatedAt:      m.CreatedAt,
	}
}
