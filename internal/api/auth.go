package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"slices"
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
	s.startLogin(w, r, provider, "")
}

// LoginWithReturnPath starts the same flow, landing on the console path
// the form body names once it completes. The path is held with the
// login's state rather than echoed anywhere, and one that is not a
// console path is dropped: a cross-site form can start a login, and
// the most it decides is where inside the console that login lands.
func (s *Server) LoginWithReturnPath(w http.ResponseWriter, r *http.Request, provider apigen.Provider) {
	if err := r.ParseForm(); err != nil {
		writeError(w, errorx.Wrap(err, "login form", errorx.KindInvalidArgument))
		return
	}
	returnPath, _ := safeReturnPath(r.PostForm.Get("next"))
	s.startLogin(w, r, provider, returnPath)
}

// startLogin issues the state, records it with returnPath, and sends
// the browser to the provider.
func (s *Server) startLogin(w http.ResponseWriter, r *http.Request, provider apigen.Provider, returnPath string) {
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
	// Recorded before it is handed out, and for the same lifetime the
	// cookie gets: the callback spends the record, so a state that
	// outlived it would be a cookie no callback accepts.
	if err := s.states.Issue(r.Context(), state, oauthFlowTTL, returnPath); err != nil {
		writeError(w, err)
		return
	}
	s.setCookie(w, stateCookie, state, oauthFlowTTL)
	s.setCookie(w, verifierCookie, verifier, oauthFlowTTL)
	http.Redirect(w, r, p.AuthCodeURL(state, verifier), http.StatusFound)
}

// OauthCallback completes the flow: it validates state, exchanges the
// code, upserts the user, spends the state, and issues a session
// cookie. Spending the state is what makes the callback single-use.
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
	identify(r, u)
	token, err := authn.NewToken()
	if err != nil {
		writeError(w, err)
		return
	}
	// Spent before the session exists, so two callbacks carrying one
	// state leave one session between them. Nothing that fails above
	// reaches here, which leaves the state live for the browser that
	// owns it — as a refused membership leaves an invitation unburned.
	returnPath, err := s.states.Consume(r.Context(), state.Value)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.sessions.Create(r.Context(), token, authn.NewSession(u.GRN, s.cfg.SessionTTL)); err != nil {
		writeError(w, err)
		return
	}
	s.expireCookie(w, stateCookie)
	s.expireCookie(w, verifierCookie)
	s.setCookie(w, sessionCookie, token, s.cfg.SessionTTL)
	// `/` is the public landing page; a signed-in visitor belongs in
	// the console, at the path the login was started for where there
	// is one. It was checked before it was stored, and is checked again
	// here so a row written by anything else cannot redirect off it.
	destination, ok := safeReturnPath(returnPath)
	if !ok {
		destination = "/console"
	}
	http.Redirect(w, r, destination, http.StatusFound)
}

// ListProviders returns the names of the registered sign-in providers.
// A name absent from it has no login route: /auth/login/{provider} is
// generic and answers 404 for a provider nothing registered.
func (s *Server) ListProviders(w http.ResponseWriter, _ *http.Request) {
	names := make([]string, 0, len(s.providers))
	for name := range s.providers {
		names = append(names, name)
	}
	slices.Sort(names)
	writeJSON(w, http.StatusOK, names)
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
	writeJSON(w, http.StatusOK, apigen.User{Grn: u.GRN.String(), Nickname: u.Nickname})
}

// upsertUser returns the user for the identity, creating it on first
// login. The provider subject becomes the user's GRN identifier.
func (s *Server) upsertUser(r *http.Request, identity *authn.Identity) (*user.User, error) {
	if reservedSubject(identity.Subject) {
		return nil, errorx.New(errorx.KindUnauthenticated, "provider subject claims a reserved identifier")
	}
	g := userGRN(identity.Subject)
	// The provider chooses the subject, so it is checked before it is
	// stored: a GRN written to a row it cannot be read back out of is
	// the one failure no later boundary can catch.
	if err := g.Validate(); err != nil {
		return nil, errorx.Wrap(err, "provider subject")
	}
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
	identify(r, u)
	return u, nil
}

// Principal is who a request turned out to be. A handler resolves
// that partway through, after any wrapping handler has started, so
// the answer travels back out through this cell rather than through a
// return value.
type Principal struct {
	// GRN is the signed-in user, empty until a handler
	// authenticates the request.
	GRN string
}

type principalKey struct{}

// WithPrincipal returns r carrying p, for a caller that wants the
// identity the handler resolves. A request served without a cell
// behaves identically; only the answer is lost.
func WithPrincipal(r *http.Request, p *Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), principalKey{}, p))
}

// PrincipalFrom returns the cell [WithPrincipal] attached, or nil.
func PrincipalFrom(r *http.Request) *Principal {
	p, _ := r.Context().Value(principalKey{}).(*Principal)
	return p
}

// identify records u as the request's principal.
func identify(r *http.Request, u *user.User) {
	if p := PrincipalFrom(r); p != nil {
		p.GRN = u.GRN.String()
	}
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
