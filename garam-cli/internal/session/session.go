package session

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	platformsdk "github.com/garamsh/garam/lib/platform-sdk-go"
)

// DefaultEndpoint is the platform the CLI signs in to where nothing
// names another (ADR-0120).
const DefaultEndpoint = "https://garam.sh"

// refreshAhead is how long before its access token lapses a session
// is refreshed, so a call does not carry a token that lapses in flight.
const refreshAhead = time.Minute

// slowDownStep is what a slow_down adds to the polling interval: five
// seconds, which RFC 8628 §3.5 fixes for the client as for the server.
const slowDownStep = 5 * time.Second

// ParseEndpoint reads raw as an origin: https, or http to a loopback
// address alone, since a session's tokens cross it.
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

// Session is the CLI's session with one platform endpoint, kept in a
// store.
type Session struct {
	Endpoint string
	Store    Store
	HTTP     *http.Client
	Now      func() time.Time
	// SlowDown is what a slow_down adds to the polling interval.
	SlowDown time.Duration
}

// New is the session with endpoint kept in the file at path.
func New(endpoint, path string, h *http.Client) *Session {
	return &Session{Endpoint: endpoint, Store: Store{Path: path}, HTTP: h, Now: time.Now, SlowDown: slowDownStep}
}

// APIBase is where the platform's browser API answers under the
// endpoint: behind the console's /api rewrite.
func (s *Session) APIBase() string { return s.Endpoint + "/api" }

// Anonymous is a client of the platform's API presenting no session.
func (s *Session) Anonymous() (*platformsdk.ClientWithResponses, error) {
	client, err := platformsdk.NewClientWithResponses(s.APIBase(), platformsdk.WithHTTPClient(s.HTTP))
	if err != nil {
		return nil, errorx.Wrap(err, "api client")
	}
	return client, nil
}

// SignedIn is a client of the platform's API presenting the session's
// bearer.
func (s *Session) SignedIn(ctx context.Context) (*platformsdk.ClientWithResponses, error) {
	bearer, err := s.Bearer(ctx)
	if err != nil {
		return nil, err
	}
	client, err := platformsdk.NewClientWithResponses(s.APIBase(), platformsdk.WithHTTPClient(s.HTTP),
		platformsdk.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+bearer)
			return nil
		}))
	if err != nil {
		return nil, errorx.Wrap(err, "api client")
	}
	return client, nil
}

func (s *Session) notSignedIn() error {
	return errorx.New(errorx.KindUnauthenticated,
		fmt.Sprintf("not signed in to %s; run garam login", s.Endpoint))
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
		tokens, err := s.refresh(ctx, e.RefreshToken)
		if err != nil {
			return false, err
		}
		keep(e, tokens, now)
		access = e.AccessToken
		return true, nil
	})
	return access, err
}

// refresh spends a refresh token for the next pair.
func (s *Session) refresh(ctx context.Context, refresh string) (*platformsdk.SessionToken, error) {
	client, err := s.Anonymous()
	if err != nil {
		return nil, err
	}
	resp, err := client.DeviceTokenWithFormdataBodyWithResponse(ctx, platformsdk.DeviceTokenFormdataRequestBody{
		ClientId: platformsdk.DeviceTokenRequestClientIdGaramCli, GrantType: platformsdk.RefreshToken, RefreshToken: &refresh,
	})
	if err != nil {
		return nil, errorx.Wrap(err, "refresh", errorx.KindUnavailable)
	}
	switch {
	case resp.JSON200 != nil:
		return resp.JSON200, nil
	case resp.JSON400 != nil && resp.JSON400.Error == platformsdk.InvalidGrant:
		return nil, s.notSignedIn()
	case resp.JSON400 != nil:
		return nil, errorx.New(errorx.KindInvalidArgument, "the token endpoint refused the refresh: "+string(resp.JSON400.Error))
	default:
		return nil, unexpected(resp.HTTPResponse)
	}
}

// keep writes tokens into e as answered at now.
func keep(e *Entry, t *platformsdk.SessionToken, now time.Time) {
	e.AccessToken, e.AccessExpiresAt = t.AccessToken, now.Add(time.Duration(t.ExpiresIn)*time.Second)
	e.RefreshToken = t.RefreshToken
	e.RefreshExpiresAt = now.Add(time.Duration(t.RefreshTokenExpiresIn) * time.Second)
}

// unexpected is an answer of the token endpoints no other case reads.
func unexpected(resp *http.Response) error {
	return errorx.New(errorx.KindUnavailable, "the platform answered "+resp.Status)
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

// SetOrg saves org as the one later commands act in.
func (s *Session) SetOrg(org string) error {
	return s.Store.Update(func(f *File) (bool, error) {
		e := f.Endpoints[s.Endpoint]
		if e == nil {
			e = &Entry{}
			f.Endpoints[s.Endpoint] = e
		}
		e.Org = org
		return true, nil
	})
}

// Logout revokes the session and forgets its tokens, keeping the
// saved organization. A session already revoked elsewhere, or none at
// all, is what logout asks for, and is forgotten the same; signedIn
// reports which it was.
func (s *Session) Logout(ctx context.Context) (signedIn bool, err error) {
	client, err := s.SignedIn(ctx)
	switch {
	case errorx.IsKind(err, errorx.KindUnauthenticated):
		return false, s.forget()
	case err != nil:
		return false, err
	}
	resp, err := client.LogoutWithResponse(ctx)
	if err != nil {
		return false, errorx.Wrap(err, "logout", errorx.KindUnavailable)
	}
	// 401 is a session already revoked elsewhere.
	if code := resp.StatusCode(); code != http.StatusNoContent && code != http.StatusUnauthorized {
		return false, unexpected(resp.HTTPResponse)
	}
	return true, s.forget()
}

// forget drops this endpoint's tokens, keeping its saved organization.
func (s *Session) forget() error {
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
