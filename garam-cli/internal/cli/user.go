package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/garamsh/garam/garam-cli/internal/session"
	"github.com/garamsh/garam/lib/errorx"
	apigen "github.com/garamsh/garam/lib/platform-sdk-go"
)

// userRequestTimeout bounds one call a user command makes to the API.
const userRequestTimeout = 30 * time.Second

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
		"the garam to talk to (default $"+string(envEndpoint)+", or "+session.DefaultEndpoint+")")
	cmd.PersistentFlags().StringVar(&f.org, "org", "",
		"the organization, by identifier (default the one `garam orgs use` saved)")
	cmd.PersistentFlags().StringVarP(&f.output, "output", "o", outputTable, "table or json")
}

// userCommand is one run of a user command: the session it calls as,
// and where it prints.
type userCommand struct {
	flags   *userFlags
	session *session.Session
	out     io.Writer
}

// newUserCommand resolves the run's endpoint and credentials file.
func newUserCommand(cmd *cobra.Command, flags *userFlags) (*userCommand, error) {
	if flags.output != outputTable && flags.output != outputJSON {
		return nil, errorx.New(errorx.KindInvalidArgument, "--output is table or json")
	}
	raw := flags.endpoint
	if raw == "" {
		raw = envOr(envEndpoint, session.DefaultEndpoint)
	}
	endpoint, err := session.ParseEndpoint(raw)
	if err != nil {
		return nil, err
	}
	path, err := session.Path(envOr(envXDGConfigHome, ""))
	if err != nil {
		return nil, err
	}
	return &userCommand{
		flags: flags, session: session.New(endpoint, path, &http.Client{Timeout: userRequestTimeout}),
		out: cmd.OutOrStdout(),
	}, nil
}

// org is the organization a command acts in: --org, or the saved
// default.
func (c *userCommand) org() (string, error) {
	if c.flags.org != "" {
		return c.flags.org, nil
	}
	org, err := c.session.Org()
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
