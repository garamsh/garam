package machineapi

import (
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/operator"
)

// maxAssertionRequest bounds the form the exchange reads: the
// assertion's own bound and room for the other parameters.
const maxAssertionRequest = operator.MaxAssertionLength + 4<<10

// oauthRefusal is a refusal this handler states itself, before the
// domain is asked: its code and the description it answers.
type oauthRefusal struct {
	code        machinegen.OAuthErrorError
	description string
}

func (e *oauthRefusal) Error() string { return string(e.code) + ": " + e.description }

// ExchangeClientAssertion answers a token for the operator whose live
// key signed the client assertion, under machine-token.v2 (ADR-0102):
// the client credentials grant authenticated by private_key_jwt.
//
// It reads no identity off the request: an Authorization header
// refuses rather than combining with the assertion, so a caller holds
// one credential here and never falls back to another.
func (s *Server) ExchangeClientAssertion(w http.ResponseWriter, r *http.Request) {
	header := w.Header()
	header.Set(operator.ContractVersionHeader, operator.AssertionContractVersion)
	header.Set("Cache-Control", "no-store")
	header.Set("Pragma", "no-cache")

	form, refusal := clientCredentialsForm(r)
	if refusal != nil {
		writeOAuthError(w, refusal)
		return
	}
	client, err := grn.Parse(form.Get("client_id"))
	if err != nil {
		writeOAuthError(w, &oauthRefusal{machinegen.InvalidRequest, "client_id is not a GRN"})
		return
	}
	var audience *grn.GRN
	if raw, ok := form["audience"]; ok {
		g, err := grn.Parse(raw[0])
		if err != nil {
			writeOAuthError(w, &oauthRefusal{machinegen.InvalidRequest, "audience is not a GRN"})
			return
		}
		audience = &g
	}
	token, err := s.operators.ExchangeAssertion(r.Context(), sourceOf(r), client, form.Get("client_assertion"), audience)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	machine.ReportIssuance(r, machine.Issuance{Subject: token.Operator, Audience: token.Audience != nil, Until: token.ExpiresAt})
	writeJSON(w, http.StatusOK, machinegen.OAuthToken{
		AccessToken: token.Value,
		TokenType:   machinegen.Bearer,
		ExpiresIn:   int(operator.TokenTTL.Seconds()),
	})
}

// sourceOf answers the address the request's connection came from,
// which bounds how many unverified assertions one address may send.
// The listener terminates TLS itself, so the peer is the client and
// not a proxy; a NAT or load balancer in front of it makes its egress
// the address every client behind it shares.
func sourceOf(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// clientCredentialsForm reads the request as this profile's form, or
// answers the refusal its first defect earns. Nothing it refuses for
// quotes the body: a parse error can carry part of the assertion.
func clientCredentialsForm(r *http.Request) (url.Values, *oauthRefusal) {
	if _, present := r.Header["Authorization"]; present {
		return nil, &oauthRefusal{machinegen.InvalidRequest,
			"the exchange is authenticated by its client assertion alone: send no Authorization header"}
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		return nil, &oauthRefusal{machinegen.InvalidRequest, "the body is application/x-www-form-urlencoded"}
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxAssertionRequest+1))
	if err != nil || len(body) > maxAssertionRequest {
		return nil, &oauthRefusal{machinegen.InvalidRequest, "the body could not be read within its bound"}
	}
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, &oauthRefusal{machinegen.InvalidRequest, "the body is not a well-formed form"}
	}
	for name, values := range form {
		if len(values) > 1 {
			return nil, &oauthRefusal{machinegen.InvalidRequest, "parameter " + strconv.Quote(name) + " appears more than once"}
		}
	}
	switch grant, ok := form["grant_type"]; {
	case !ok:
		return nil, &oauthRefusal{machinegen.InvalidRequest, "grant_type is required"}
	case grant[0] != operator.GrantTypeClientCredentials:
		return nil, &oauthRefusal{machinegen.UnsupportedGrantType, "the grant is client_credentials"}
	}
	if _, ok := form["scope"]; ok {
		return nil, &oauthRefusal{machinegen.InvalidScope, "this profile accepts no scope"}
	}
	if _, ok := form["client_secret"]; ok {
		return nil, &oauthRefusal{machinegen.InvalidRequest, "this profile accepts no client secret"}
	}
	if form.Get("client_assertion_type") != operator.ClientAssertionTypeJWT {
		return nil, &oauthRefusal{machinegen.InvalidRequest,
			"client_assertion_type is " + operator.ClientAssertionTypeJWT}
	}
	for _, name := range []string{"client_id", "client_assertion"} {
		if form.Get(name) == "" {
			return nil, &oauthRefusal{machinegen.InvalidRequest, name + " is required"}
		}
	}
	return form, nil
}

// writeOAuthError answers err in RFC 6749's error form. It is this
// route's translator from a domain error to a status and code, as
// writeError is every other route's: the exchange's caller is a stock
// OAuth client, which reads this body and not Error.
//
// The description is fixed per code, so no refusal reveals which part
// of an assertion failed or whether a registration exists; the cause
// reaches the request log instead.
func writeOAuthError(w http.ResponseWriter, err error) {
	status, body := http.StatusServiceUnavailable, machinegen.OAuthError{Error: machinegen.TemporarilyUnavailable}
	var refusal *oauthRefusal
	var exceeded *operator.Exceeded
	switch {
	case errors.As(err, &refusal):
		status, body.Error = http.StatusBadRequest, refusal.code
		body.ErrorDescription = &refusal.description
	case errors.As(err, &exceeded):
		status = http.StatusTooManyRequests
		w.Header().Set("Retry-After", strconv.Itoa(exceeded.RetryAfterSeconds()))
	case errors.Is(err, operator.ErrAudienceInvalid):
		status, body.Error = http.StatusBadRequest, machinegen.InvalidRequest
		description := "audience is another operator the organization registered"
		body.ErrorDescription = &description
	case errorx.IsKind(err, errorx.KindUnauthenticated):
		status, body.Error = http.StatusBadRequest, machinegen.InvalidClient
	case errorx.IsKind(err, errorx.KindInvalidArgument):
		status, body.Error = http.StatusBadRequest, machinegen.InvalidRequest
	case errorx.IsKind(err, errorx.KindInternal):
		status, body.Error = http.StatusInternalServerError, machinegen.ServerError
	}
	if recorder, ok := w.(causeRecorder); ok {
		recorder.RecordCause(err)
	}
	writeJSON(w, status, body)
}
