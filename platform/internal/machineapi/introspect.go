package machineapi

import (
	"net/http"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/identity"
	"github.com/garamsh/garam/platform/internal/operator"
)

// WithIdentity has the server answer introspection of the tokens
// issuer signed (ADR-0141) to the services callers authenticates.
// Without it the operation answers 404 as any path the listener has no
// route for does.
func (s *Server) WithIdentity(issuer *identity.Issuer, callers *identity.Callers) *Server {
	s.issuer, s.callers = issuer, callers
	return s
}

// IntrospectIssuedToken answers RFC 7662's introspection of a token
// garam issued, to the service it was issued for. The caller
// authenticates by `private_key_jwt` and so reads no identity off the
// request otherwise, as the client assertion exchange does.
func (s *Server) IntrospectIssuedToken(w http.ResponseWriter, r *http.Request) {
	if s.issuer == nil || s.callers == nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")

	form, refusal := assertionForm(r)
	if refusal != nil {
		writeOAuthError(w, refusal)
		return
	}
	if form.Get("client_assertion_type") != operator.ClientAssertionTypeJWT {
		writeOAuthError(w, &oauthRefusal{machinegen.InvalidRequest,
			"client_assertion_type is " + operator.ClientAssertionTypeJWT})
		return
	}
	for _, name := range []string{"token", "client_id", "client_assertion"} {
		if form.Get(name) == "" {
			writeOAuthError(w, &oauthRefusal{machinegen.InvalidRequest, name + " is required"})
			return
		}
	}
	audience, err := s.callers.Authenticate(form.Get("client_id"), form.Get("client_assertion"))
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	claims, active, err := s.issuer.Introspect(r.Context(), audience, form.Get("token"))
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	if !active {
		writeJSON(w, http.StatusOK, machinegen.Introspection{Active: false})
		return
	}
	iat, exp := int(claims.IssuedAt.Unix()), int(claims.IssuedAt.Add(claims.Lifetime).Unix())
	writeJSON(w, http.StatusOK, machinegen.Introspection{
		Active: true,
		Iss:    &claims.Issuer,
		Sub:    &claims.Subject,
		Aud:    &claims.Audience,
		Iat:    &iat,
		Exp:    &exp,
		Jti:    &claims.JTI,
		Sid:    &claims.SessionID,
	})
}
