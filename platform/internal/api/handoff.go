package api

import (
	"net/http"
	"time"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/authn"
	"github.com/garamsh/garam/platform/internal/errorx"
)

// The bounds of a handoff's state, as the specification states them.
// The lower one is what keeps a state a secret the receiving app drew
// rather than a value a page could guess.
const (
	minHandoffState = 32
	maxHandoffState = 128
	maxHandoffCode  = 128
)

// ErrHandoffStateInvalid refuses a state outside its bounds. It names
// the rule and never the value sent.
var ErrHandoffStateInvalid = errorx.New(errorx.KindInvalidArgument, "state must be 32 to 128 characters")

// IssueSessionHandoff hands the caller's sign-in to another of garam's
// apps (ADR-0131). The session cookie is host-only, so the site issues
// a code and the receiving app redeems it on its own host. Only a
// browser's session hands off: a bearer is a CLI's, and a CLI that
// could buy a browser session would hold more than ADR-0120 gave it.
func (s *Server) IssueSessionHandoff(w http.ResponseWriter, r *http.Request) {
	token, bearer, ok := presentedSession(r)
	if !ok || bearer {
		writeError(w, errorx.New(errorx.KindUnauthenticated, "a browser session is required"))
		return
	}
	var body apigen.SessionHandoffRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if n := len(body.State); n < minHandoffState || n > maxHandoffState {
		writeError(w, ErrHandoffStateInvalid)
		return
	}
	code, err := authn.NewToken()
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.sessions.IssueHandoff(r.Context(), token, code, body.State, authn.HandoffTTL); err != nil {
		writeError(w, errorx.Wrap(err, "session", errorx.KindUnauthenticated))
		return
	}
	writeJSON(w, http.StatusCreated, apigen.SessionHandoff{Code: code})
}

// RedeemSessionHandoff spends a handoff code and sets the session
// cookie on the host it is asked on (ADR-0131). The receiving app's
// server makes this call and replays the cookie to the browser, so the
// session is host-only to that app.
func (s *Server) RedeemSessionHandoff(w http.ResponseWriter, r *http.Request) {
	var body apigen.SessionHandoffRedemption
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if n := len(body.State); n < minHandoffState || n > maxHandoffState {
		writeError(w, ErrHandoffStateInvalid)
		return
	}
	if body.Code == "" || len(body.Code) > maxHandoffCode {
		writeError(w, authn.ErrHandoffNotLive)
		return
	}
	token, err := authn.NewToken()
	if err != nil {
		writeError(w, err)
		return
	}
	sess, err := s.sessions.RedeemHandoff(r.Context(), body.Code, body.State, token)
	if err != nil {
		writeError(w, err)
		return
	}
	u, err := s.users.Get(r.Context(), sess.User)
	if err != nil {
		writeError(w, err)
		return
	}
	identify(r, u)
	s.setCookie(w, sessionCookie, token, time.Until(sess.ExpiresAt))
	w.WriteHeader(http.StatusNoContent)
}
