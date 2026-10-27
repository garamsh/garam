// Package http implements platform.Platform against the platform's
// browser API, presenting each caller's own credential: `me` for who
// it is, and `checkPermissions` for what it may do.
package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"net/url"
	"time"

	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/platform/sdk/errorx"
	"github.com/garamsh/garam/platform/sdk/grn"
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

var _ platform.Platform = (*Client)(nil)

// New answers a client of the platform at base.
func New(base string) (*Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, fmt.Sprintf("platform: %q is not an absolute URL", base))
	}
	return &Client{base: u, client: &nethttp.Client{Timeout: requestTimeout}}, nil
}

// Identify implements platform.Platform.
func (c *Client) Identify(ctx context.Context, cred platform.Credential) (platform.Caller, error) {
	if cred.IsZero() {
		return platform.Caller{}, errorx.New(errorx.KindUnauthenticated, "no credential")
	}
	var me struct {
		GRN string `json:"grn"`
	}
	if err := c.do(ctx, cred, nethttp.MethodGet, "/me", nil, &me); err != nil {
		return platform.Caller{}, err
	}
	g, err := grn.Parse(me.GRN)
	if err != nil {
		return platform.Caller{}, errorx.Wrap(err, "platform: me answered no GRN", errorx.KindUnavailable)
	}
	return platform.Caller{GRN: g, Credential: cred}, nil
}

type permissionResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type permissionCheck struct {
	Action   string              `json:"action"`
	Resource *permissionResource `json:"resource,omitempty"`
}

// Allowed implements platform.Platform. A decision other than allow is
// not allowed: the platform answers `unknown` only for actions a
// delegation grant decides, and no cowork action is one.
func (c *Client) Allowed(ctx context.Context, caller platform.Caller, org string, checks []platform.Check) ([]bool, error) {
	req := struct {
		Checks []permissionCheck `json:"checks"`
	}{Checks: make([]permissionCheck, len(checks))}
	for i, ch := range checks {
		req.Checks[i] = permissionCheck{Action: ch.Action}
		if ch.Page != "" {
			req.Checks[i].Resource = &permissionResource{Type: "cowork-page", ID: ch.Page}
		}
	}
	var answer struct {
		Results []struct {
			Decision string `json:"decision"`
		} `json:"results"`
	}
	if err := c.do(ctx, caller.Credential, nethttp.MethodPost, "/orgs/"+url.PathEscape(org)+"/permission-checks", req, &answer); err != nil {
		return nil, err
	}
	if len(answer.Results) != len(checks) {
		return nil, errorx.New(errorx.KindUnavailable,
			fmt.Sprintf("platform: %d checks answered %d decisions", len(checks), len(answer.Results)))
	}
	allowed := make([]bool, len(checks))
	for i, r := range answer.Results {
		allowed[i] = r.Decision == "allow"
	}
	return allowed, nil
}

// do sends one request as cred and decodes a 200 into out. The
// platform's refusals keep their meaning: 401 unauthenticated, and 403
// and 404, which it answers a caller outside the organization,
// permission denied. Anything else is the platform being unavailable.
func (c *Client) do(ctx context.Context, cred platform.Credential, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return errorx.Wrap(err, "platform: encode request", errorx.KindInternal)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := nethttp.NewRequestWithContext(ctx, method, c.base.JoinPath(path).String(), reader)
	if err != nil {
		return errorx.Wrap(err, "platform: request", errorx.KindInternal)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
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
		return errorx.New(errorx.KindUnavailable, fmt.Sprintf("platform: %s %s answered %s", method, path, resp.Status))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return errorx.Wrap(err, "platform: decode answer", errorx.KindUnavailable)
	}
	return nil
}
