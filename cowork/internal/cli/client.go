package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/garamsh/garam/cowork/internal/mcp"
	"github.com/garamsh/garam/lib/clisession"
	"github.com/garamsh/garam/lib/errorx"
)

// The environment a client command reads.
const (
	envEndpoint      = "COWORK_ENDPOINT"
	envGaramEndpoint = "GARAM_ENDPOINT"
)

// defaultEndpoint is the cowork a client command talks to where
// neither --endpoint nor COWORK_ENDPOINT names one.
const defaultEndpoint = "https://cowork.garam.sh"

// requestTimeout bounds one call a client command makes.
const requestTimeout = 30 * time.Second

// The --output values.
const (
	outputTable = "table"
	outputJSON  = "json"
)

// clientAnnotation marks a client command, which exits with ADR-0122's
// codes rather than a server command's 1.
const clientAnnotation = "cowork/client"

// clientFlags are the flags every client command shares.
type clientFlags struct {
	endpoint string
	platform string
	org      string
	output   string
}

func (f *clientFlags) bind(cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&f.endpoint, "endpoint", "",
		"the cowork to talk to (default $"+envEndpoint+", or "+defaultEndpoint+")")
	cmd.PersistentFlags().StringVar(&f.platform, "platform", "",
		"the garam whose session the CLI holds (default $"+envGaramEndpoint+", or "+clisession.DefaultEndpoint+")")
	cmd.PersistentFlags().StringVar(&f.org, "org", "",
		"the workspace, by identifier (default the one `garam orgs use` saved)")
	cmd.PersistentFlags().StringVarP(&f.output, "output", "o", outputTable, "table or json")
}

// client is one run of a client command: the cowork it calls, and the
// platform session it calls as. The session is garam's CLI session
// (ADR-0120), kept in garam's credentials file, so `garam login` and
// `cowork login` sign the same session in.
type client struct {
	flags    *clientFlags
	endpoint string
	session  *clisession.Session
	out      io.Writer
}

func newClient(cmd *cobra.Command, flags *clientFlags) (*client, error) {
	if flags.output != outputTable && flags.output != outputJSON {
		return nil, errorx.New(errorx.KindInvalidArgument, "--output is table or json")
	}
	endpoint, err := clisession.ParseEndpoint(firstOf(flags.endpoint, os.Getenv(envEndpoint), defaultEndpoint))
	if err != nil {
		return nil, err
	}
	platform, err := clisession.ParseEndpoint(firstOf(flags.platform, os.Getenv(envGaramEndpoint), clisession.DefaultEndpoint))
	if err != nil {
		return nil, err
	}
	path, err := clisession.Path()
	if err != nil {
		return nil, err
	}
	return &client{
		flags: flags, endpoint: endpoint, out: cmd.OutOrStdout(),
		session: &clisession.Session{
			Endpoint: platform, Store: clisession.Store{Path: path},
			HTTP: &http.Client{Timeout: requestTimeout}, Now: time.Now, Login: "cowork login",
		},
	}, nil
}

func firstOf(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

// org is the workspace a command acts in: --org, or the saved one.
func (c *client) org() (string, error) {
	if c.flags.org != "" {
		return c.flags.org, nil
	}
	org, err := c.session.Org()
	if err != nil {
		return "", err
	}
	if org == "" {
		return "", errorx.New(errorx.KindInvalidArgument, "name a workspace with --org, or save one with `garam orgs use`")
	}
	return org, nil
}

// bearer sends every request with the session's bearer, refreshed
// where it lapses.
type bearer struct {
	session *clisession.Session
	next    http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	token, err := b.session.Bearer(r.Context())
	if err != nil {
		return nil, err
	}
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+token)
	return b.next.RoundTrip(r)
}

// call calls one of cowork's tools and decodes what it answers into
// out. A tool's error comes back with the kind the server named, so
// the command exits with its code.
func (c *client) call(ctx context.Context, tool string, args map[string]any, out any) error {
	// The session is checked first, so a person not signed in is told
	// so rather than that the connection failed.
	if _, err := c.session.Bearer(ctx); err != nil {
		return err
	}
	cl := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "cowork-cli", Version: "1"}, nil)
	session, err := cl.Connect(ctx, &sdkmcp.StreamableClientTransport{
		Endpoint:   c.endpoint + mcp.Path,
		HTTPClient: &http.Client{Timeout: requestTimeout, Transport: bearer{c.session, http.DefaultTransport}},
	}, nil)
	if err != nil {
		return errorx.Wrap(err, "connecting to "+c.endpoint, errorx.KindUnavailable)
	}
	defer func() { _ = session.Close() }()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return errorx.Wrap(err, tool, errorx.KindUnavailable)
	}
	if res.IsError {
		kind, _ := res.Meta[mcp.KindKey].(string)
		return errorx.New(kindNamed(kind), textOf(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return errorx.Wrap(err, tool)
	}
	return json.Unmarshal(raw, out)
}

// kindNamed is the errorx kind name names, unknown for none.
func kindNamed(name string) errorx.Kind {
	for k := errorx.KindUnknown; k <= errorx.KindUnauthenticated; k++ {
		if k.String() == name {
			return k
		}
	}
	return errorx.KindUnknown
}

func textOf(res *sdkmcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if t, ok := c.(*sdkmcp.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// render prints value as JSON under -o json, and rows under header
// otherwise.
func (c *client) render(value any, header []string, rows [][]string) error {
	if c.flags.output == outputJSON {
		enc := json.NewEncoder(c.out)
		enc.SetIndent("", "  ")
		return enc.Encode(value)
	}
	w := tabwriter.NewWriter(c.out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, strings.Join(header, "\t"))
	for _, row := range rows {
		_, _ = fmt.Fprintln(w, strings.Join(row, "\t"))
	}
	return w.Flush()
}

// clientCommand is a client command whose run gets its client.
func clientCommand(cmd *cobra.Command, flags *clientFlags, run func(*cobra.Command, *client, []string) error) *cobra.Command {
	cmd.Annotations = map[string]string{clientAnnotation: "true"}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newClient(cmd, flags)
		if err != nil {
			return err
		}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()
		cmd.SetContext(ctx)
		return run(cmd, c, args)
	}
	return cmd
}

func newLoginCommand(flags *clientFlags) *cobra.Command {
	var noBrowser bool
	cmd := clientCommand(&cobra.Command{
		Use:   "login",
		Short: "Sign in to garam, approving the CLI in a browser",
		Long: "Sign in to garam by the device grant. The session is the one `garam login` keeps, " +
			"so either command signs both CLIs in.",
		Args: cobra.NoArgs,
	}, flags, func(cmd *cobra.Command, c *client, _ []string) error {
		code, err := c.session.Start(cmd.Context(), clisession.ClientName("cowork CLI"))
		if err != nil {
			return err
		}
		page := c.session.VerificationURL(code)
		prompt := cmd.ErrOrStderr()
		_, _ = fmt.Fprintf(prompt, "Your code is %s. Approve it at\n\n  %s\n\n", code.UserCode, page)
		if !noBrowser && openBrowser(page) == nil {
			_, _ = fmt.Fprintln(prompt, "Opened that page in your browser; waiting for the approval.")
		} else {
			_, _ = fmt.Fprintln(prompt, "Open that page in a browser; waiting for the approval.")
		}
		if err := c.session.Wait(cmd.Context(), code); err != nil {
			return err
		}
		who, err := c.me(cmd.Context())
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(prompt, "Signed in to %s as %s (%s).\n", c.session.Endpoint, who.Nickname, who.GRN)
		return nil
	})
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "print the URL to open rather than opening it")
	return cmd
}

// openBrowser asks the desktop to open page, and reports whether it
// could ask; whether a browser came up is not something it can see.
func openBrowser(page string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	cmd := exec.Command(opener, page)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

type user struct {
	GRN      string `json:"grn"`
	Nickname string `json:"nickname"`
}

func (c *client) me(ctx context.Context) (user, error) {
	body, err := c.session.Do(ctx, http.MethodGet, "/me", http.StatusOK)
	if err != nil {
		return user{}, err
	}
	var u user
	if err := json.Unmarshal(body, &u); err != nil {
		return user{}, errorx.Wrap(err, "me", errorx.KindUnavailable)
	}
	return u, nil
}

func newLogoutCommand(flags *clientFlags) *cobra.Command {
	return clientCommand(&cobra.Command{
		Use:   "logout",
		Short: "Revoke the session and forget its tokens, for cowork and garam's CLI alike",
		Args:  cobra.NoArgs,
	}, flags, func(cmd *cobra.Command, c *client, _ []string) error {
		if err := c.session.Logout(cmd.Context()); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Signed out of %s.\n", c.session.Endpoint)
		return nil
	})
}

func newWhoamiCommand(flags *clientFlags) *cobra.Command {
	return clientCommand(&cobra.Command{
		Use:   "whoami",
		Short: "Show who the CLI is signed in as",
		Args:  cobra.NoArgs,
	}, flags, func(cmd *cobra.Command, c *client, _ []string) error {
		who, err := c.me(cmd.Context())
		if err != nil {
			return err
		}
		return c.render(who, []string{"NICKNAME", "GRN", "PLATFORM"}, [][]string{{who.Nickname, who.GRN, c.session.Endpoint}})
	})
}
