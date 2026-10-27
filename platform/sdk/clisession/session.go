package clisession

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/garamsh/garam/platform/sdk/errorx"
)

// DefaultEndpoint is the platform a CLI signs in to where nothing
// names another.
const DefaultEndpoint = "https://garam.sh"

// ClientID is the one client the platform's device grant serves.
const ClientID = "garam-cli"

// refreshAhead is how long before its access token lapses a session
// is refreshed, so a call does not carry a token that lapses in flight.
const refreshAhead = time.Minute

// The OAuth errors the token endpoint answers (RFC 6749 §5.2, RFC
// 8628 §3.5).
const (
	errPending   = "authorization_pending"
	errSlowDown  = "slow_down"
	errDenied    = "access_denied"
	errExpired   = "expired_token"
	errBadGrant  = "invalid_grant"
	slowDownStep = 5 * time.Second
)

// ParseEndpoint answers raw as an origin, refusing one that would send
// a session's tokens in the clear: http only to a loopback host.
func ParseEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return "", errorx.New(errorx.KindInvalidArgument,
			fmt.Sprintf("endpoint %q is not an origin such as %s", raw, DefaultEndpoint))
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopback(u.Hostname()) {
			return "", errorx.New(errorx.KindInvalidArgument,
				fmt.Sprintf("endpoint %q is http to a host that is not loopback; a session's tokens would cross it in the clear", raw))
		}
	default:
		return "", errorx.New(errorx.KindInvalidArgument, fmt.Sprintf("endpoint %q is not https", raw))
	}
	return u.Scheme + "://" + u.Host, nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ClientName is what the platform's session list calls a CLI: name on
// the host it runs on, bounded as the platform bounds it.
func ClientName(name string) string {
	if host, err := os.Hostname(); err == nil && host != "" {
		name += " on " + host
	}
	for utf8.RuneCountInString(name) > 100 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}

// Session is a CLI's session with one platform endpoint, kept in a
// store.
type Session struct {
	Endpoint string
	Store    Store
	HTTP     *http.Client
	Now      func() time.Time
	// Login is the command that signs this CLI in, named when it is
	// not signed in.
	Login string
}

// DeviceCode is what a device sign-in starts with (RFC 8628 §3.2).
type DeviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// tokens are a session's, as the token endpoint answers them.
type tokens struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
}

type oauthError struct {
	Error       string `json:"error"`
	Description string `json:"error_description"`
	Interval    int    `json:"interval"`
}

// api is the platform's browser API under the endpoint.
func (s *Session) api(path string) string { return s.Endpoint + "/api" + path }

// form posts a form to the token endpoints and decodes a 200 into ok
// and a 400 into an OAuth error.
func (s *Session) form(ctx context.Context, path string, values url.Values, ok any) (*oauthError, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.api(path), strings.NewReader(values.Encode()))
	if err != nil {
		return nil, errorx.Wrap(err, path)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, errorx.Wrap(err, path, errorx.KindUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, errorx.Wrap(err, path, errorx.KindUnavailable)
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return nil, json.Unmarshal(body, ok)
	case http.StatusBadRequest, http.StatusUnauthorized:
		var e oauthError
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return &e, nil
		}
	}
	return nil, Failure(resp.StatusCode, body)
}

// Failure is a response the CLI did not expect, by its status: the
// kind ADR-0122's exit codes are read from, and the platform's own
// message where it sent one.
func Failure(status int, body []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	msg := http.StatusText(status)
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		msg = e.Message
	}
	kind := errorx.KindUnknown
	switch {
	case status == http.StatusUnauthorized:
		kind = errorx.KindUnauthenticated
	case status == http.StatusForbidden:
		kind = errorx.KindPermissionDenied
	case status == http.StatusNotFound:
		kind = errorx.KindNotFound
	case status == http.StatusConflict:
		kind = errorx.KindFailedPrecondition
	case status == http.StatusBadRequest:
		kind = errorx.KindInvalidArgument
	case status == http.StatusTooManyRequests, status >= 500:
		kind = errorx.KindUnavailable
	}
	return errorx.New(kind, fmt.Sprintf("%d: %s", status, msg))
}

// Start begins a device sign-in under name.
func (s *Session) Start(ctx context.Context, name string) (DeviceCode, error) {
	var code DeviceCode
	e, err := s.form(ctx, "/auth/device", url.Values{"client_id": {ClientID}, "client_name": {name}}, &code)
	if err != nil {
		return DeviceCode{}, err
	}
	if e != nil {
		return DeviceCode{}, errorx.New(errorx.KindInvalidArgument, "the platform refused the sign-in: "+e.Error)
	}
	return code, nil
}

// VerificationURL is the page to approve code on, its path joined to
// the endpoint as the platform answers a path (ADR-0120).
func (s *Session) VerificationURL(code DeviceCode) string {
	if strings.HasPrefix(code.VerificationURIComplete, "/") {
		return s.Endpoint + code.VerificationURIComplete
	}
	return code.VerificationURIComplete
}

// Wait polls the token endpoint at the interval it sets until code is
// approved, denied or lapses, and keeps the approved session.
func (s *Session) Wait(ctx context.Context, code DeviceCode) error {
	interval := time.Duration(code.Interval) * time.Second
	deadline := s.Now().Add(time.Duration(code.ExpiresIn) * time.Second)
	for {
		select {
		case <-ctx.Done():
			return errorx.Wrap(ctx.Err(), "waiting for the approval", errorx.KindCanceled)
		case <-time.After(interval):
		}
		var t tokens
		e, err := s.form(ctx, "/auth/device/token", url.Values{
			"client_id": {ClientID}, "grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "device_code": {code.DeviceCode},
		}, &t)
		switch {
		case errorx.IsKind(err, errorx.KindUnavailable):
			// RFC 8628 §3.5: a client that cannot reach the endpoint
			// keeps polling, backing off, until the code lapses.
			if s.Now().After(deadline) {
				return err
			}
			interval += slowDownStep
			continue
		case err != nil:
			return err
		case e == nil:
			return s.keep(t)
		}
		switch e.Error {
		case errPending:
		case errSlowDown:
			interval = max(interval+slowDownStep, time.Duration(e.Interval)*time.Second)
		case errDenied:
			return errorx.New(errorx.KindPermissionDenied, "the sign-in was denied")
		case errExpired:
			return errorx.New(errorx.KindDeadlineExceeded, "the code lapsed before it was approved; run "+s.Login+" again")
		default:
			return errorx.New(errorx.KindInvalidArgument, "the token endpoint refused the poll: "+e.Error)
		}
	}
}

// keep writes t as the endpoint's session, keeping its organization.
func (s *Session) keep(t tokens) error {
	now := s.Now()
	return s.Store.Update(func(f *File) (bool, error) {
		e := f.Endpoints[s.Endpoint]
		if e == nil {
			e = &Entry{}
			f.Endpoints[s.Endpoint] = e
		}
		set(e, t, now)
		return true, nil
	})
}

func set(e *Entry, t tokens, now time.Time) {
	e.AccessToken, e.AccessExpiresAt = t.AccessToken, now.Add(time.Duration(t.ExpiresIn)*time.Second)
	e.RefreshToken, e.RefreshExpiresAt = t.RefreshToken, now.Add(time.Duration(t.RefreshTokenExpiresIn)*time.Second)
}

func (s *Session) notSignedIn() error {
	return errorx.New(errorx.KindUnauthenticated, fmt.Sprintf("not signed in to %s; run %s", s.Endpoint, s.Login))
}

// Bearer answers the session's access token, refreshed first where it
// lapses within refreshAhead, and unauthenticated where there is no
// live session.
func (s *Session) Bearer(ctx context.Context) (string, error) {
	var access string
	err := s.Store.Update(func(f *File) (bool, error) {
		e := f.Endpoints[s.Endpoint]
		now := s.Now()
		if !e.SignedIn(now) {
			return false, s.notSignedIn()
		}
		if e.AccessToken != "" && now.Add(refreshAhead).Before(e.AccessExpiresAt) {
			access = e.AccessToken
			return false, nil
		}
		var t tokens
		oe, err := s.form(ctx, "/auth/device/token", url.Values{
			"client_id": {ClientID}, "grant_type": {"refresh_token"}, "refresh_token": {e.RefreshToken},
		}, &t)
		switch {
		case err != nil:
			return false, err
		case oe != nil && oe.Error == errBadGrant:
			return false, s.notSignedIn()
		case oe != nil:
			return false, errorx.New(errorx.KindInvalidArgument, "the token endpoint refused the refresh: "+oe.Error)
		}
		set(e, t, now)
		access = e.AccessToken
		return true, nil
	})
	return access, err
}

// Org is the organization `garam orgs use` saved for the endpoint,
// empty where none is.
func (s *Session) Org() (string, error) {
	var org string
	err := s.Store.Update(func(f *File) (bool, error) {
		if e := f.Endpoints[s.Endpoint]; e != nil {
			org = e.Org
		}
		return false, nil
	})
	return org, err
}

// Do sends a request to the platform's browser API at path with the
// session's bearer, and answers the response when it is ok.
func (s *Session) Do(ctx context.Context, method, path string, ok int) ([]byte, error) {
	bearer, err := s.Bearer(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, s.api(path), nil)
	if err != nil {
		return nil, errorx.Wrap(err, path)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, errorx.Wrap(err, path, errorx.KindUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, errorx.Wrap(err, path, errorx.KindUnavailable)
	}
	if resp.StatusCode != ok {
		return body, Failure(resp.StatusCode, body)
	}
	return body, nil
}

// Logout revokes the session and forgets its tokens, keeping the
// saved organization. A session already revoked elsewhere is what
// logout asks for, and is forgotten the same.
func (s *Session) Logout(ctx context.Context) error {
	_, err := s.Do(ctx, http.MethodPost, "/auth/logout", http.StatusNoContent)
	if err != nil && !errorx.IsKind(err, errorx.KindUnauthenticated) {
		return err
	}
	return s.Store.Update(func(f *File) (bool, error) {
		e := f.Endpoints[s.Endpoint]
		if e == nil {
			return false, nil
		}
		if e.Org == "" {
			delete(f.Endpoints, s.Endpoint)
		} else {
			f.Endpoints[s.Endpoint] = &Entry{Org: e.Org}
		}
		return true, nil
	})
}
