// Package http implements platform.Identity against the platform's
// browser API, presenting each caller's own credential: `me` for who
// it is, and `listOrgs` for the organizations it has.
package http

import (
	"context"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"net/url"
	"time"

	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// sessionCookie is the platform's session cookie (`api/openapi.yaml`,
// `sessionCookie`).
const sessionCookie = "garam_session"

// requestTimeout bounds one question to the platform. An answer that
// has not arrived is refused like one that said no.
const requestTimeout = 10 * time.Second

// Client asks the platform at base, the browser API's root.
type Client struct {
	base   *url.URL
	client *nethttp.Client
}

var _ platform.Identity = (*Client)(nil)

// New answers a client of the platform at base.
func New(base string) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, fmt.Sprintf("platform: %q is not an absolute URL", base))
	}
	return &Client{base: u, client: &nethttp.Client{Timeout: requestTimeout}}, nil
}

// Identify implements platform.Identity.
func (c *Client) Identify(ctx context.Context, cred platform.Credential) (platform.Caller, error) {
	if cred.IsZero() {
		return platform.Caller{}, errorx.New(errorx.KindUnauthenticated, "no credential")
	}
	var me struct {
		GRN string `json:"grn"`
	}
	if err := c.do(ctx, cred, "/me", &me); err != nil {
		return platform.Caller{}, err
	}
	g, err := grn.Parse(me.GRN)
	if err != nil {
		return platform.Caller{}, errorx.Wrap(err, "platform: me answered no GRN", errorx.KindUnavailable)
	}
	return platform.Caller{GRN: g, Credential: cred}, nil
}

// Organizations implements platform.Identity with `listOrgs`.
func (c *Client) Organizations(ctx context.Context, caller platform.Caller) ([]platform.Organization, error) {
	var orgs []struct {
		GRN  string `json:"grn"`
		Name string `json:"name"`
	}
	if err := c.do(ctx, caller.Credential, "/orgs", &orgs); err != nil {
		return nil, err
	}
	out := make([]platform.Organization, 0, len(orgs))
	for _, o := range orgs {
		g, err := grn.Parse(o.GRN)
		if err != nil {
			return nil, errorx.Wrap(err, "platform: listOrgs answered a malformed GRN", errorx.KindUnavailable)
		}
		out = append(out, platform.Organization{ID: g.Identifier, Name: o.Name})
	}
	return out, nil
}

// do sends one GET as cred and decodes a 200 into out. The
// platform's refusals keep their meaning: 401 unauthenticated, and 403
// and 404, which it answers a caller outside the organization,
// permission denied. Anything else is the platform being unavailable.
func (c *Client) do(ctx context.Context, cred platform.Credential, path string, out any) error {
	req, err := nethttp.NewRequestWithContext(ctx, nethttp.MethodGet, c.base.JoinPath(path).String(), nil)
	if err != nil {
		return errorx.Wrap(err, "platform: request", errorx.KindInternal)
	}
	if cred.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+cred.Bearer)
	} else {
		req.AddCookie(&nethttp.Cookie{Name: sessionCookie, Value: cred.Session})
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return errorx.Wrap(err, "platform: unreachable", errorx.KindUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case nethttp.StatusOK:
	case nethttp.StatusUnauthorized:
		return errorx.New(errorx.KindUnauthenticated, "the platform does not accept this credential")
	case nethttp.StatusForbidden, nethttp.StatusNotFound:
		return errorx.New(errorx.KindPermissionDenied, "not a member of this organization")
	default:
		return errorx.New(errorx.KindUnavailable, fmt.Sprintf("platform: GET %s answered %s", path, resp.Status))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return errorx.Wrap(err, "platform: decode answer", errorx.KindUnavailable)
	}
	return nil
}
