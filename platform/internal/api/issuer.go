package api

import (
	"errors"
	"net/http"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/identity"
)

// WithIdentity makes the server garam's OIDC issuer (ADR-0141): its
// discovery document, its key set and the exchange of a CLI session
// for a token. Without it the three operations answer 404 as any path
// the listener has no route for does.
func (s *Server) WithIdentity(issuer *identity.Issuer) *Server {
	s.issuer = issuer
	return s
}

// GetIssuerConfiguration serves the discovery document.
func (s *Server) GetIssuerConfiguration(w http.ResponseWriter, r *http.Request) {
	if s.issuer == nil {
		http.NotFound(w, r)
		return
	}
	writeBytes(w, s.issuer.Discovery())
}

// GetIssuerKeys serves the issuer's key set.
func (s *Server) GetIssuerKeys(w http.ResponseWriter, r *http.Request) {
	if s.issuer == nil {
		http.NotFound(w, r)
		return
	}
	writeBytes(w, s.issuer.JWKS())
}

// writeBytes answers body, already JSON, as 200.
func writeBytes(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// ExchangeSessionToken answers RFC 8693's token exchange: the access
// token of a CLI session for a token signed for one service.
func (s *Server) ExchangeSessionToken(w http.ResponseWriter, r *http.Request) {
	if s.issuer == nil {
		http.NotFound(w, r)
		return
	}
	form, refusal := oauthForm(r)
	if refusal != nil {
		writeOAuthError(w, *refusal)
		return
	}
	if form.Get("grant_type") != identity.GrantTypeTokenExchange {
		writeOAuthError(w, oauthRefusal(apigen.UnsupportedGrantType, "the grant is token exchange"))
		return
	}
	// The profile takes none of RFC 8693's other parameters, and
	// ignoring one would answer a token that is not the one asked for.
	for _, name := range []string{"scope", "resource", "actor_token", "actor_token_type"} {
		if _, ok := form[name]; ok {
			writeOAuthError(w, oauthRefusal(apigen.InvalidRequest, name+" is not accepted"))
			return
		}
	}
	exchanged, err := s.issuer.Exchange(r.Context(), identity.ExchangeRequest{
		SubjectToken:       form.Get("subject_token"),
		SubjectTokenType:   form.Get("subject_token_type"),
		Audience:           form.Get("audience"),
		RequestedTokenType: form.Get("requested_token_type"),
	})
	switch {
	case errors.Is(err, identity.ErrInvalidRequest):
		writeOAuthError(w, oauthRefusal(apigen.InvalidRequest, "a parameter is missing or not one the exchange takes"))
		return
	case errors.Is(err, identity.ErrInvalidTarget):
		writeOAuthError(w, oauthRefusal(apigen.InvalidTarget, "the audience is policy or codehub"))
		return
	case errors.Is(err, identity.ErrInvalidGrant):
		writeOAuthError(w, oauthRefusal(apigen.InvalidGrant, "the subject token is not a live session's access token"))
		return
	case err != nil:
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, apigen.TokenExchangeResponse{
		AccessToken:     exchanged.Token,
		IssuedTokenType: apigen.TokenExchangeResponseIssuedTokenType(identity.TokenTypeAccessToken),
		TokenType:       "Bearer",
		ExpiresIn:       exchanged.ExpiresIn,
	})
}
