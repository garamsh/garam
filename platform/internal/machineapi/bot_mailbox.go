package machineapi

import (
	"net/http"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/dpop"
	"github.com/garamsh/garam/platform/internal/message"
)

// PollBotMailbox hands a bot's process the oldest message addressed to
// the bot that it has not acknowledged, acknowledging the one its
// cursor names (ADR-0194). It is the first route that accepts a bot's
// token, so it holds ADR-0191 decision 6: the token arrives as DPoP,
// and the proof is by the key the token is bound to, for this request,
// spent once. The token is resolved before the proof is checked, so a
// request presenting no live token writes no jti.
func (s *Server) PollBotMailbox(w http.ResponseWriter, r *http.Request, botGRN machinegen.BotGrn, params machinegen.PollBotMailboxParams) {
	caller, ok := s.botCaller(w, r)
	if !ok {
		return
	}
	recipient, err := grn.Parse(botGRN)
	if err != nil {
		writeError(w, errorx.Wrap(err, "bot", errorx.KindInvalidArgument))
		return
	}
	var cursor *message.ClaimID
	if params.Cursor != nil {
		id := message.ClaimID(*params.Cursor)
		cursor = &id
	}
	signer, err := s.keys.Signer(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	claim, signed, err := s.messages.Poll(r.Context(), caller, recipient, cursor, signer)
	if errorx.IsKind(err, errorx.KindNotFound) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, machinegen.BotDelivery{
		Cursor: string(claim.ID), Attempt: claim.Message.Attempts, Message: messageBody(claim.Message),
		Content: signed.Content, Signature: signed.Signature,
	})
}

// botCaller answers the bot a request's DPoP-bound token names once
// its proof verified, and writes the refusal itself, with the
// challenge RFC 9449 §7.1 names, where it does not.
func (s *Server) botCaller(w http.ResponseWriter, r *http.Request) (grn.GRN, bool) {
	token, err := dpop.Presented(r.Header.Get("Authorization"))
	if err == nil && len(r.Header.Values("Authorization")) > 1 {
		err = dpop.ErrSchemeRefused
	}
	if err != nil {
		refuseBotToken(w, err)
		return grn.GRN{}, false
	}
	if s.bots == nil || s.dpopProofs == nil || s.origin == "" {
		writeError(w, errorx.New(errorx.KindUnavailable, "no bot token is accepted on this listener"))
		return grn.GRN{}, false
	}
	held, err := s.bots.ResolveBotToken(r.Context(), token)
	if err != nil {
		refuseBotToken(w, err)
		return grn.GRN{}, false
	}
	if err := s.dpopProofs.Bound(r.Context(), r, s.origin+r.URL.EscapedPath(), token, held.KeyThumbprint); err != nil {
		refuseBotToken(w, err)
		return grn.GRN{}, false
	}
	return held.Bot, true
}

// refuseBotToken answers a refused bot token or proof 401 with its
// challenge, and any other failure, a store that could not be read
// among them, as its kind maps.
func refuseBotToken(w http.ResponseWriter, err error) {
	challenge := dpop.Challenge(err)
	if challenge == "" && errorx.IsKind(err, errorx.KindUnauthenticated) {
		challenge = dpop.Challenge(dpop.ErrSchemeRefused)
	}
	if challenge == "" {
		writeError(w, err)
		return
	}
	w.Header().Set("WWW-Authenticate", challenge)
	writeErrorStatus(w, http.StatusUnauthorized, err)
}
