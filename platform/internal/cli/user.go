package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/authn"
	"github.com/garamsh/garam/platform/internal/errorx"
)

// defaultEndpoint is the garam a command talks to where neither
// --endpoint nor GARAM_ENDPOINT names one (ADR-0120).
const defaultEndpoint = "https://garam.sh"

// userRequestTimeout bounds one call a user command makes to the API.
const userRequestTimeout = 30 * time.Second

// refreshAhead is how long before its access token lapses a command
// refreshes it, so a call does not carry a token that lapses in flight.
const refreshAhead = time.Minute

// The --output values.
const (
	outputTable = "table"
	outputJSON  = "json"
)

// userFlags are the flags every user command shares.
type userFlags struct {
	endpoint string
	org      string
	output   string
}

// bind adds the flags to cmd and every command under it, and records
// them as cmd's, so a derived command placed under it reads them.
func (f *userFlags) bind(cmd *cobra.Command) {
	userFlagsOf[cmd] = f
	cmd.PersistentFlags().StringVar(&f.endpoint, "endpoint", "",
		"the garam to talk to (default $"+string(envEndpoint)+", or "+defaultEndpoint+")")
	cmd.PersistentFlags().StringVar(&f.org, "org", "",
		"the organization, by identifier (default the one `garam orgs use` saved)")
	cmd.PersistentFlags().StringVarP(&f.output, "output", "o", outputTable, "table or json")
}

// userCommand is one run of a user command: where it talks, and the
// file it keeps its session in.
type userCommand struct {
	flags    *userFlags
	endpoint string
	store    credentialStore
	http     *http.Client
	out      io.Writer
	now      func() time.Time
	// slowDown is what a slow_down adds to the polling interval.
	slowDown time.Duration
}

// newUserCommand resolves the run's endpoint and credentials file.
func newUserCommand(cmd *cobra.Command, flags *userFlags) (*userCommand, error) {
	if flags.output != outputTable && flags.output != outputJSON {
		return nil, errorx.New(errorx.KindInvalidArgument, "--output is table or json")
	}
	raw := flags.endpoint
	if raw == "" {
		raw = envOr(envEndpoint, defaultEndpoint)
	}
	endpoint, err := parseEndpoint(raw)
	if err != nil {
		return nil, err
	}
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	return &userCommand{
		flags: flags, endpoint: endpoint, store: credentialStore{path: path},
		http: &http.Client{Timeout: userRequestTimeout}, out: cmd.OutOrStdout(), now: time.Now,
		slowDown: authn.DeviceSlowDownStep,
	}, nil
}

// parseEndpoint reads an endpoint as an origin: https, or http to a
// loopback address alone, since a session's tokens cross it.
func parseEndpoint(raw string) (string, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") {
		return "", errorx.New(errorx.KindInvalidArgument,
			fmt.Sprintf("endpoint %q is not an origin such as %s", raw, defaultEndpoint))
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !loopback(u.Hostname()) {
			return "", errorx.New(errorx.KindInvalidArgument,
				fmt.Sprintf("endpoint %q is http to a host that is not loopback; a session's tokens would cross it in the clear", raw))
		}
	default:
		return "", errorx.New(errorx.KindInvalidArgument,
			fmt.Sprintf("endpoint %q is not https", raw))
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

// apiBase is where the browser API answers under an endpoint: behind
// the console's /api rewrite.
func (c *userCommand) apiBase() string { return c.endpoint + "/api" }

// anonymous is a client presenting no session, for the device grant.
func (c *userCommand) anonymous() (*apigen.ClientWithResponses, error) {
	client, err := apigen.NewClientWithResponses(c.apiBase(), apigen.WithHTTPClient(c.http))
	if err != nil {
		return nil, errorx.Wrap(err, "api client")
	}
	return client, nil
}

// signedIn answers a client presenting this endpoint's session, its
// access token refreshed first where it lapses within refreshAhead.
func (c *userCommand) signedIn(ctx context.Context) (*apigen.ClientWithResponses, error) {
	var access string
	err := c.store.update(func(file *credentialsFile) (bool, error) {
		entry := file.Endpoints[c.endpoint]
		now := c.now()
		if !entry.signedIn(now) {
			return false, c.notSignedIn()
		}
		if entry.AccessToken != "" && now.Add(refreshAhead).Before(entry.AccessExpiresAt) {
			access = entry.AccessToken
			return false, nil
		}
		tokens, err := c.refresh(ctx, entry.RefreshToken)
		if err != nil {
			return false, err
		}
		entry.AccessToken, entry.AccessExpiresAt = tokens.AccessToken, now.Add(time.Duration(tokens.ExpiresIn)*time.Second)
		entry.RefreshToken = tokens.RefreshToken
		entry.RefreshExpiresAt = now.Add(time.Duration(tokens.RefreshTokenExpiresIn) * time.Second)
		access = entry.AccessToken
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	client, err := apigen.NewClientWithResponses(c.apiBase(), apigen.WithHTTPClient(c.http),
		apigen.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+access)
			return nil
		}))
	if err != nil {
		return nil, errorx.Wrap(err, "api client")
	}
	return client, nil
}

// refresh spends a refresh token for the next pair.
func (c *userCommand) refresh(ctx context.Context, refresh string) (*apigen.SessionToken, error) {
	client, err := c.anonymous()
	if err != nil {
		return nil, err
	}
	resp, err := client.DeviceTokenWithFormdataBodyWithResponse(ctx, apigen.DeviceTokenFormdataRequestBody{
		ClientId: apigen.DeviceTokenRequestClientIdGaramCli, GrantType: apigen.RefreshToken, RefreshToken: &refresh,
	})
	if err != nil {
		return nil, errorx.Wrap(err, "refresh", errorx.KindUnavailable)
	}
	switch {
	case resp.JSON200 != nil:
		return resp.JSON200, nil
	case resp.JSON400 != nil && resp.JSON400.Error == apigen.InvalidGrant:
		return nil, c.notSignedIn()
	default:
		return nil, apiFailure(resp.HTTPResponse, resp.Body)
	}
}

func (c *userCommand) notSignedIn() error {
	return errorx.New(errorx.KindUnauthenticated,
		fmt.Sprintf("not signed in to %s; run garam login", c.endpoint))
}

// org is the organization a command acts in: --org, or the saved
// default.
func (c *userCommand) org() (string, error) {
	if c.flags.org != "" {
		return c.flags.org, nil
	}
	var org string
	err := c.store.update(func(file *credentialsFile) (bool, error) {
		if entry := file.Endpoints[c.endpoint]; entry != nil {
			org = entry.Org
		}
		return false, nil
	})
	if err != nil {
		return "", err
	}
	if org == "" {
		return "", errorx.New(errorx.KindInvalidArgument, "no organization: pass --org, or save one with garam orgs use")
	}
	return org, nil
}

// apiFailure reads a refusal the API answered into an error carrying
// its kind and message.
func apiFailure(resp *http.Response, body []byte) error {
	var refusal apigen.Error
	if err := json.Unmarshal(body, &refusal); err != nil || refusal.Message == "" {
		return errorx.New(errorx.KindUnavailable, fmt.Sprintf("the API answered %s", resp.Status))
	}
	kind := errorx.KindUnknown
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		kind = errorx.KindUnauthenticated
	case http.StatusForbidden:
		kind = errorx.KindPermissionDenied
	case http.StatusNotFound:
		kind = errorx.KindNotFound
	case http.StatusBadRequest:
		kind = errorx.KindInvalidArgument
	}
	return errorx.New(kind, refusal.Message)
}

// render writes value as JSON, or as the table rows answers under
// header.
func (c *userCommand) render(value any, header []string, rows func() [][]string) error {
	if c.flags.output == outputJSON {
		enc := json.NewEncoder(c.out)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	w := tabwriter.NewWriter(c.out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, strings.Join(header, "\t"))
	for _, row := range rows() {
		_, _ = fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return w.Flush()
}

// lastSegment is a GRN's identifier, which is what --org and a
// command's argument name an object by.
func lastSegment(g string) string {
	return g[strings.LastIndex(g, ":")+1:]
}
