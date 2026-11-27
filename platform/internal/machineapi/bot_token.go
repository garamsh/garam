package machineapi

import (
	"net/http"
	"net/url"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/dpop"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/operator"
)

// exchangeBotToken answers a token for the bot a connector's RFC 8693
// exchange names (ADR-0189 decision 2), bound by DPoP to the key of
// the bot's process (ADR-0191). The connector's own token, the actor,
// is what authenticates the request; the subject is the bot's GRN,
// which a live Bot declaration must place on the actor's record
// (AUTH-5). The audience, where one is named, is garam's. The proof,
// the bot's process's, names the key the token is bound to and
// carries a nonce garam issued, and every answer carries the current
// one for the next proof (RFC 9449 §8). The proof is checked here and
// spent by the domain once the actor authenticated, so a request that
// authenticates nothing writes no jti.
func (s *Server) exchangeBotToken(w http.ResponseWriter, r *http.Request, form url.Values) {
	if s.bots == nil || s.dpopProofs == nil || s.tokenURL == "" {
		writeOAuthError(w, operator.ErrAssertionProfileUnavailable)
		return
	}
	nonce, err := s.dpopProofs.Nonce(r.Context())
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	w.Header().Set(dpop.NonceHeader, nonce)
	if refusal := botExchange(form); refusal != nil {
		writeOAuthError(w, refusal)
		return
	}
	proof, err := dpop.Proof(r.Header)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	checked, err := s.dpopProofs.Check(r.Context(), proof, dpop.Request{Method: r.Method, URL: s.tokenURL, Nonce: true})
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	exchange := bot.BotExchange{
		ActorToken: form.Get("actor_token"), Subject: form.Get("subject_token"), Scope: form.Get("scope"),
		KeyThumbprint: checked.KeyThumbprint, SpendProof: checked.Spend,
	}
	if raw, ok := form["audience"]; ok {
		exchange.Audience = &raw[0]
	}
	token, err := s.bots.MintBotToken(r.Context(), exchange)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	machine.ReportIssuance(r, machine.Issuance{Subject: token.Bot, Until: token.ExpiresAt})
	issued := machinegen.OAuthTokenIssuedTokenType(bot.TokenTypeAccessToken)
	writeJSON(w, http.StatusOK, machinegen.OAuthToken{
		AccessToken:     token.Value,
		TokenType:       machinegen.DPoP,
		ExpiresIn:       int(bot.BotTokenTTL.Seconds()),
		IssuedTokenType: &issued,
	})
}

// botExchange answers the refusal the first defect of form, read as
// the bot token exchange's, earns, or nil. RFC 8693 §2.2.2 makes a
// request that is not valid invalid_request, and a resource garam does
// not serve invalid_target.
func botExchange(form url.Values) *oauthRefusal {
	for _, name := range []string{"client_id", "client_assertion", "client_assertion_type", "client_secret"} {
		if _, ok := form[name]; ok {
			return &oauthRefusal{machinegen.InvalidRequest, "the bot token exchange takes no " + name}
		}
	}
	if _, ok := form["resource"]; ok {
		return &oauthRefusal{machinegen.InvalidTarget, "a bot's token is garam's: name no resource"}
	}
	if requested, ok := form["requested_token_type"]; ok && requested[0] != bot.TokenTypeAccessToken {
		return &oauthRefusal{machinegen.InvalidRequest, "requested_token_type is " + bot.TokenTypeAccessToken}
	}
	switch {
	case form.Get("subject_token_type") != bot.TokenTypeBot:
		return &oauthRefusal{machinegen.InvalidRequest, "subject_token_type is " + bot.TokenTypeBot}
	case form.Get("subject_token") == "":
		return &oauthRefusal{machinegen.InvalidRequest, "subject_token is required"}
	case form.Get("actor_token_type") != bot.TokenTypeAccessToken:
		return &oauthRefusal{machinegen.InvalidRequest, "actor_token_type is " + bot.TokenTypeAccessToken}
	case form.Get("actor_token") == "":
		return &oauthRefusal{machinegen.InvalidRequest, "actor_token is required"}
	}
	return nil
}
