package machineapi

import (
	"net/http"
	"net/url"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/operator"
)

// exchangeBotToken answers a token for the bot a connector's RFC 8693
// exchange names (ADR-0189 decision 2). The connector's own token, the
// actor, is what authenticates the request; the subject is the bot's
// GRN, which a live declaration must place on the actor's record
// (AUTH-5). The answer's audience is garam's, so a request naming one
// is refused.
func (s *Server) exchangeBotToken(w http.ResponseWriter, r *http.Request, form url.Values) {
	if refusal := botExchange(form); refusal != nil {
		writeOAuthError(w, refusal)
		return
	}
	if s.clusters == nil {
		writeOAuthError(w, operator.ErrAssertionProfileUnavailable)
		return
	}
	token, err := s.clusters.MintBotToken(r.Context(), cluster.BotExchange{
		ActorToken: form.Get("actor_token"), Subject: form.Get("subject_token"), Scope: form.Get("scope"),
	})
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	machine.ReportIssuance(r, machine.Issuance{Subject: token.Bot, Until: token.ExpiresAt})
	issued := machinegen.OAuthTokenIssuedTokenType(cluster.TokenTypeAccessToken)
	writeJSON(w, http.StatusOK, machinegen.OAuthToken{
		AccessToken:     token.Value,
		TokenType:       machinegen.Bearer,
		ExpiresIn:       int(cluster.BotTokenTTL.Seconds()),
		IssuedTokenType: &issued,
	})
}

// botExchange answers the refusal the first defect of form, read as
// the bot token exchange's, earns, or nil.
func botExchange(form url.Values) *oauthRefusal {
	for _, name := range []string{"client_id", "client_assertion", "client_assertion_type", "client_secret",
		"resource", "requested_token_type"} {
		if _, ok := form[name]; ok {
			return &oauthRefusal{machinegen.InvalidRequest, "the bot token exchange takes no " + name}
		}
	}
	if _, ok := form["audience"]; ok {
		return &oauthRefusal{machinegen.InvalidTarget, "a bot's token is garam's: name no audience"}
	}
	switch {
	case form.Get("subject_token_type") != cluster.TokenTypeBot:
		return &oauthRefusal{machinegen.InvalidRequest, "subject_token_type is " + cluster.TokenTypeBot}
	case form.Get("subject_token") == "":
		return &oauthRefusal{machinegen.InvalidRequest, "subject_token is required"}
	case form.Get("actor_token_type") != cluster.TokenTypeAccessToken:
		return &oauthRefusal{machinegen.InvalidRequest, "actor_token_type is " + cluster.TokenTypeAccessToken}
	case form.Get("actor_token") == "":
		return &oauthRefusal{machinegen.InvalidRequest, "actor_token is required"}
	}
	return nil
}
