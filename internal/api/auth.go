package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/user"
)

const (
	sessionCookie  = "garam_session"
	stateCookie    = "garam_oauth_state"
	verifierCookie = "garam_oauth_verifier"

	// oauthFlowTTL bounds the login redirect round trip.
	oauthFlowTTL = 10 * time.Minute
)

// Login starts the OAuth authorization code flow with PKCE.
func (s *Server) Login(w http.ResponseWriter, r *http.Request, provider apigen.Provider) {
	p, ok := s.providers[provider]
	if !ok {
		writeError(w, errorx.New(errorx.KindNotFound, "unknown provider"))
		return
	}
	state, err := randomHex(16)
	if err != nil {
		writeError(w, err)
		return
	}
	verifier, err := randomHex(32)
	if err != nil {
		writeError(w, err)
		return
	}
	s.setCookie(w, stateCookie, state, oauthFlowTTL)
	s.setCookie(w, verifierCookie, verifier, oauthFlowTTL)
	http.Redirect(w, r, p.AuthCodeURL(state, verifier), http.StatusFound)
}

// OauthCallback completes the flow: it validates state, exchanges the
// code, upserts the user, and issues a session cookie.
func (s *Server) OauthCallback(w http.ResponseWriter, r *http.Request, provider apigen.Provider, params apigen.OauthCallbackParams) {
	p, ok := s.providers[provider]
	if !ok {
		writeError(w, errorx.New(errorx.KindNotFound, "unknown provider"))
		return
	}
	state, err := r.Cookie(stateCookie)
	if err != nil || state.Value == "" || state.Value != params.State {
		writeError(w, errorx.New(errorx.KindUnauthenticated, "oauth state mismatch"))
		return
	}
	verifier, err := r.Cookie(verifierCookie)
	if err != nil || verifier.Value == "" {
		writeError(w, errorx.New(errorx.KindUnauthenticated, "missing pkce verifier"))
		return
	}
	identity, err := p.Exchange(r.Context(), params.Code, verifier.Value)
	if err != nil {
		writeError(w, err)
		return
	}
	u, err := s.upsertUser(r, identity)
	if err != nil {
		writeError(w, err)
		return
	}
	token, err := authn.NewToken()
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.sessions.Create(r.Context(), token, authn.NewSession(u.GRN(), s.cfg.SessionTTL)); err != nil {
		writeError(w, err)
		return
	}
	s.expireCookie(w, stateCookie)
	s.expireCookie(w, verifierCookie)
	s.setCookie(w, sessionCookie, token, s.cfg.SessionTTL)
	http.Redirect(w, r, "/", http.StatusFound)
}

// Logout revokes the current session.
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		writeError(w, errorx.New(errorx.KindUnauthenticated, "no session"))
		return
	}
	if err := s.sessions.Delete(r.Context(), cookie.Value); err != nil {
		writeError(w, err)
		return
	}
	s.expireCookie(w, sessionCookie)
	w.WriteHeader(http.StatusNoContent)
}

// Me returns the signed-in user.
func (s *Server) Me(w http.ResponseWriter, r *http.Request) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apigen.User{Grn: u.GRN().String(), Nickname: u.Nickname()})
}

// upsertUser returns the user for the identity, creating it on first
// login. The provider subject becomes the user's GRN identifier.
func (s *Server) upsertUser(r *http.Request, identity *authn.Identity) (*user.User, error) {
	g := userGRN(identity.Subject)
	u, err := s.users.Get(r.Context(), g)
	if err == nil {
		return u, nil
	}
	if !errorx.IsKind(err, errorx.KindNotFound) {
		return nil, err
	}
	u = user.New(g, identity.Nickname)
	if err := s.users.Create(r.Context(), u); err != nil {
		return nil, err
	}
	return u, nil
}

// currentUser resolves the session cookie to the user.
func (s *Server) currentUser(r *http.Request) (*user.User, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return nil, errorx.New(errorx.KindUnauthenticated, "no session")
	}
	sess, err := s.sessions.Get(r.Context(), cookie.Value)
	if err != nil {
		return nil, errorx.Wrap(err, "session", errorx.KindUnauthenticated)
	}
	u, err := s.users.Get(r.Context(), sess.User())
	if err != nil {
		return nil, err
	}
	return u, nil
}

// setCookie writes an HttpOnly SameSite=Lax cookie.
func (s *Server) setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// expireCookie clears a cookie.
func (s *Server) expireCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// userGRN builds the GRN for a user identified by provider subject.
// Users are root-owned: they exist before any org membership
// (ADR-0001).
func userGRN(subject string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, subject)
}

// randomHex returns n random bytes hex-encoded.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", errorx.Wrap(err, "random")
	}
	return hex.EncodeToString(b[:]), nil
}
