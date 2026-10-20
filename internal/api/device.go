package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/errorx"
)

// cliClientID is the one client the device grant serves (ADR-0120).
const cliClientID = "garam-cli"

// devicePage is the console's approval page, answered as a path: the
// API sits behind the console's rewrite and is not told the public
// origin, and the CLI joins the path to the endpoint it was pointed at
// (ADR-0120).
const devicePage = "/device"

// maxClientName bounds what a CLI says it runs on, in characters, as
// sessions.name does.
const maxClientName = 100

// WithDeviceGrant gives the server the store device sign-ins are kept
// in (ADR-0120). A server without one answers the device grant's
// endpoints unavailable.
func (s *Server) WithDeviceGrant(store authn.DeviceStore) *Server {
	s.devices = store
	return s
}

// RequestDeviceCode starts a device sign-in for the CLI: RFC 8628's
// device authorization request.
func (s *Server) RequestDeviceCode(w http.ResponseWriter, r *http.Request) {
	form, refusal := oauthForm(r)
	if refusal != nil {
		writeOAuthError(w, *refusal)
		return
	}
	if form.Get("client_id") != cliClientID {
		writeOAuthError(w, oauthRefusal(apigen.InvalidClient, "the client is garam-cli"))
		return
	}
	name, ok := clientName(form.Get("client_name"))
	if !ok {
		writeOAuthError(w, oauthRefusal(apigen.InvalidRequest, "client_name is text of at most 100 characters"))
		return
	}
	if s.devices == nil {
		writeError(w, errorx.New(errorx.KindUnavailable, "device sign-in is not configured"))
		return
	}
	grant, err := authn.NewDeviceGrant()
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.devices.Issue(r.Context(), grant, name); err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, apigen.DeviceCode{
		DeviceCode:              grant.DeviceCode,
		UserCode:                grant.UserCode,
		VerificationUri:         devicePage,
		VerificationUriComplete: devicePage + "?" + url.Values{"code": {grant.UserCode}}.Encode(),
		ExpiresIn:               int(authn.DeviceCodeTTL / time.Second),
		Interval:                int(authn.DevicePollInterval / time.Second),
	})
}

// DeviceToken is the device grant's token endpoint: a poll under the
// device code grant, or a CLI session's refresh.
func (s *Server) DeviceToken(w http.ResponseWriter, r *http.Request) {
	form, refusal := oauthForm(r)
	if refusal != nil {
		writeOAuthError(w, *refusal)
		return
	}
	if form.Get("client_id") != cliClientID {
		writeOAuthError(w, oauthRefusal(apigen.InvalidClient, "the client is garam-cli"))
		return
	}
	if s.devices == nil {
		writeError(w, errorx.New(errorx.KindUnavailable, "device sign-in is not configured"))
		return
	}
	switch apigen.DeviceTokenRequestGrantType(form.Get("grant_type")) {
	case apigen.UrnIetfParamsOauthGrantTypeDeviceCode:
		s.pollDevice(w, r, form.Get("device_code"))
	case apigen.RefreshToken:
		s.refreshSession(w, r, form.Get("refresh_token"))
	default:
		writeOAuthError(w, oauthRefusal(apigen.UnsupportedGrantType, "the grant is the device code or a refresh"))
	}
}

// pollDevice answers one poll of a device code.
func (s *Server) pollDevice(w http.ResponseWriter, r *http.Request, deviceCode string) {
	if deviceCode == "" {
		writeOAuthError(w, oauthRefusal(apigen.InvalidRequest, "device_code is required"))
		return
	}
	tokens, err := authn.NewCLITokens(time.Now())
	if err != nil {
		writeError(w, err)
		return
	}
	result, err := s.devices.Poll(r.Context(), deviceCode, tokens)
	switch {
	case errorx.IsKind(err, errorx.KindInvalidArgument):
		writeOAuthError(w, oauthRefusal(apigen.InvalidGrant, "the device code is not one garam holds"))
		return
	case err != nil:
		writeError(w, err)
		return
	}
	switch result.Outcome {
	case authn.PollIssued:
		writeSessionToken(w, tokens)
	case authn.PollSlowDown:
		interval := int(result.Interval / time.Second)
		refusal := oauthRefusal(apigen.SlowDown, "poll less often")
		refusal.Interval = &interval
		writeOAuthError(w, refusal)
	case authn.PollPending:
		writeOAuthError(w, oauthRefusal(apigen.AuthorizationPending, "the code is not yet decided"))
	case authn.PollDenied:
		writeOAuthError(w, oauthRefusal(apigen.AccessDenied, "the code was denied"))
	default:
		writeOAuthError(w, oauthRefusal(apigen.ExpiredToken, "the code has lapsed; sign in again"))
	}
}

// refreshSession spends a CLI session's refresh token for the next
// pair.
func (s *Server) refreshSession(w http.ResponseWriter, r *http.Request, refresh string) {
	if refresh == "" {
		writeOAuthError(w, oauthRefusal(apigen.InvalidRequest, "refresh_token is required"))
		return
	}
	tokens, err := authn.NewCLITokens(time.Now())
	if err != nil {
		writeError(w, err)
		return
	}
	if _, err := s.sessions.Refresh(r.Context(), refresh, tokens); err != nil {
		if errorx.IsKind(err, errorx.KindUnauthenticated) {
			writeOAuthError(w, oauthRefusal(apigen.InvalidGrant, "the refresh token is not live; sign in again"))
			return
		}
		writeError(w, err)
		return
	}
	writeSessionToken(w, tokens)
}

// DecideDeviceCode approves or denies a device sign-in as the
// signed-in user.
func (s *Server) DecideDeviceCode(w http.ResponseWriter, r *http.Request) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	var body apigen.DeviceDecision
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, err)
		return
	}
	if s.devices == nil {
		writeError(w, errorx.New(errorx.KindUnavailable, "device sign-in is not configured"))
		return
	}
	if err := s.devices.Decide(r.Context(), body.UserCode, u.GRN, body.Approve); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListMySessions answers the signed-in user's sessions.
func (s *Server) ListMySessions(w http.ResponseWriter, r *http.Request) {
	current, err := s.currentSession(r)
	if err != nil {
		writeError(w, err)
		return
	}
	sessions, err := s.sessions.List(r.Context(), current.session.User)
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]apigen.Session, 0, len(sessions))
	for _, sess := range sessions {
		expires := sess.ExpiresAt
		if sess.Kind == authn.SessionCLI {
			expires = sess.RefreshExpiresAt
		}
		item := apigen.Session{
			Id: sess.ID, Kind: apigen.SessionKind(sess.Kind), CreatedAt: sess.CreatedAt,
			ExpiresAt: expires, Current: sess.ID == current.session.ID,
		}
		if name := sess.Name; name != "" {
			item.Name = &name
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, out)
}

// RevokeMySession revokes one of the signed-in user's sessions.
func (s *Server) RevokeMySession(w http.ResponseWriter, r *http.Request, session string) {
	u, err := s.currentUser(r)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := s.sessions.Revoke(r.Context(), u.GRN, session); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// oauthForm reads an RFC 6749 form: each parameter at most once, as
// §3.2 has a token endpoint require.
func oauthForm(r *http.Request) (url.Values, *apigen.OAuthError) {
	if err := r.ParseForm(); err != nil {
		refusal := oauthRefusal(apigen.InvalidRequest, "the body is application/x-www-form-urlencoded")
		return nil, &refusal
	}
	for name, values := range r.PostForm {
		if len(values) > 1 {
			refusal := oauthRefusal(apigen.InvalidRequest, name+" is sent once")
			return nil, &refusal
		}
	}
	return r.PostForm, nil
}

// clientName reads what a CLI says it runs on, trimmed: absent, or
// text of at most maxClientName characters with no control character.
func clientName(sent string) (string, bool) {
	name := strings.TrimSpace(sent)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxClientName {
		return "", false
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return "", false
	}
	return name, true
}

func oauthRefusal(code apigen.OAuthErrorError, description string) apigen.OAuthError {
	return apigen.OAuthError{Error: code, ErrorDescription: &description}
}

// writeOAuthError answers RFC 6749's error response, which is 400 for
// every code it and RFC 8628 define here.
func writeOAuthError(w http.ResponseWriter, refusal apigen.OAuthError) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusBadRequest, refusal)
}

func writeSessionToken(w http.ResponseWriter, tokens authn.CLITokens) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, apigen.SessionToken{
		AccessToken:           tokens.Access,
		TokenType:             apigen.Bearer,
		ExpiresIn:             int(authn.CLIAccessTTL / time.Second),
		RefreshToken:          tokens.Refresh,
		RefreshTokenExpiresIn: int(authn.CLIRefreshTTL / time.Second),
	})
}
