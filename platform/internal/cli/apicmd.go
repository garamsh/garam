package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	apigen "github.com/garamsh/garam/api"
	"github.com/garamsh/garam/internal/errorx"
)

// apiOperation is one browser API operation the CLI serves as a
// command, as cligen derives it from api/openapi.yaml (ADR-0122).
type apiOperation struct {
	ID     string
	Method string
	Path   string
	// Words and Verb name the command: `garam <words...> <verb>`.
	Words []string
	Verb  string
	Short string
	// Args are the path parameters a caller passes in order, after the
	// org where FromOrg reads it from --org or the saved default.
	Args     []string
	FromOrg  bool
	Fields   []apiField
	BodyType string
	Headers  map[string]string
	// Columns are the scalar properties a table shows, in declared
	// order; parent.child reaches into an object property.
	Columns []string
	// Rows is the answer's property whose array is the table's rows,
	// empty where the answer is the array or one row.
	Rows string
	// Secrets are fields of the answer that never reach stdout.
	Secrets []string
	// Public is an operation that asks for no session, so the command
	// sends none and runs signed out.
	Public bool
	call   func(ctx context.Context, c apigen.ClientInterface, path []string, contentType string, body io.Reader, edit apigen.RequestEditorFn) (*http.Response, error)
}

// apiField is a query or header parameter, or a body property, as a
// flag carries it.
type apiField struct {
	Name     string
	In       string
	Kind     string // string, integer, boolean, strings or json
	Required bool
	Help     string
}

// flagName is a field's flag: its name in kebab case.
func (f apiField) flagName() string {
	var b strings.Builder
	for i, r := range f.Name {
		switch {
		case r == '_':
			b.WriteByte('-')
		case r >= 'A' && r <= 'Z':
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + 'a' - 'A')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// addAPICommands places every derived command under root, beside the
// hand-written ones, sharing a noun command where one already exists.
func addAPICommands(root *cobra.Command, ops []apiOperation) {
	for _, op := range ops {
		parent := root
		for i, word := range op.Words {
			parent = nounCommand(parent, word, i == 0)
		}
		parent.AddCommand(newAPICommand(op, flagsOf(parent)))
	}
}

// nounCommand finds or makes the noun command word under parent. A
// top-level noun binds the user flags its commands share.
func nounCommand(parent *cobra.Command, word string, top bool) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == word {
			return c
		}
	}
	c := &cobra.Command{Use: word, Short: "The browser API's " + word}
	if top {
		(&userFlags{}).bind(c)
	}
	parent.AddCommand(c)
	return c
}

// userCommandNames are the top-level commands that talk to the browser
// API: every one that binds the user flags.
func userCommandNames(root *cobra.Command) []string {
	var names []string
	for _, c := range root.Commands() {
		if c.PersistentFlags().Lookup("endpoint") != nil || c.Flags().Lookup("endpoint") != nil {
			names = append(names, c.Name())
		}
	}
	return names
}

// userFlagsOf maps a top-level noun command to the flags it bound.
var userFlagsOf = map[*cobra.Command]*userFlags{}

// flagsOf answers the user flags the command's top-level noun bound.
func flagsOf(c *cobra.Command) *userFlags {
	for ; c != nil; c = c.Parent() {
		if f, ok := userFlagsOf[c]; ok {
			return f
		}
	}
	return &userFlags{}
}

// newAPICommand is one derived command.
func newAPICommand(op apiOperation, flags *userFlags) *cobra.Command {
	use := op.Verb
	for _, a := range op.Args {
		use += " <" + a + ">"
	}
	var body, out string
	cmd := &cobra.Command{
		Use:   use,
		Short: op.Short,
		Args:  cobra.ExactArgs(len(op.Args)),
	}
	if len(op.Args) > 0 {
		cmd.Long = op.Short + "\n\nAn argument that begins with \"-\" follows \"--\"."
	}
	values := map[string]any{}
	for _, f := range op.Fields {
		help := f.Help
		if f.Required {
			help = "(required) " + help
		}
		switch f.Kind {
		case "integer":
			values[f.Name] = cmd.Flags().Int(f.flagName(), 0, help)
		case "boolean":
			values[f.Name] = cmd.Flags().Bool(f.flagName(), false, help)
		case "strings":
			values[f.Name] = cmd.Flags().StringArray(f.flagName(), nil, help+" (repeat for more)")
		case "json":
			values[f.Name] = cmd.Flags().String(f.flagName(), "", help+" (JSON, or @file)")
		default:
			values[f.Name] = cmd.Flags().String(f.flagName(), "", help)
		}
	}
	if op.BodyType != "" {
		cmd.Flags().StringVar(&body, "body", "", "the whole request body as JSON, or @file, in place of the flags")
	}
	if len(op.Secrets) > 0 {
		cmd.Flags().StringVar(&out, "out", "", "(required) the file the answer is written to, created at mode 0600; its secret never reaches stdout")
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		c, err := newUserCommand(cmd, flags)
		if err != nil {
			return err
		}
		if len(op.Secrets) > 0 && out == "" {
			return errorx.New(errorx.KindInvalidArgument, "--out names the file the answer is written to; it carries a secret, which is never printed")
		}
		req, err := op.request(cmd, args, values, body)
		if err != nil {
			return err
		}
		path := args
		if op.FromOrg {
			org, err := c.org()
			if err != nil {
				return err
			}
			path = append([]string{org}, args...)
		}
		var client *apigen.ClientWithResponses
		if op.Public {
			client, err = c.anonymous()
		} else {
			client, err = c.signedIn(cmd.Context())
		}
		if err != nil {
			return err
		}
		// The file is created before anything is sent, so an answer
		// carrying a secret, an issued key among them, is never
		// discarded for want of somewhere to put it. Any failure from
		// here on removes it.
		var secret *secretFile
		if len(op.Secrets) > 0 {
			if secret, err = createSecretFile(out); err != nil {
				return err
			}
			defer secret.discard()
		}
		resp, err := op.call(cmd.Context(), client.ClientInterface, path, op.BodyType, req.body, req.edit)
		if err != nil {
			return errorx.Wrap(err, op.ID, errorx.KindUnavailable)
		}
		defer func() { _ = resp.Body.Close() }()
		answer, err := io.ReadAll(resp.Body)
		if err != nil {
			return errorx.Wrap(err, op.ID, errorx.KindUnavailable)
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return apiFailure(resp, answer)
		}
		if secret != nil {
			if answer, err = secret.keep(answer, op.Secrets); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Wrote the answer to %s (mode 0600).\n", out)
		}
		return c.renderAnswer(answer, op.Columns, op.Rows)
	}
	return cmd
}

// apiRequest is what a command sends besides its path.
type apiRequest struct {
	body io.Reader
	edit apigen.RequestEditorFn
}

// request reads the command's flags into the query, the headers and
// the body.
func (op apiOperation) request(cmd *cobra.Command, args []string, values map[string]any, raw string) (apiRequest, error) {
	query := map[string][]string{}
	headers := map[string]string{}
	doc := map[string]any{}
	for _, f := range op.Fields {
		changed := cmd.Flags().Changed(f.flagName())
		if !changed {
			if f.Required && !(f.In == "body" && raw != "") {
				return apiRequest{}, errorx.New(errorx.KindInvalidArgument, "--"+f.flagName()+" is required")
			}
			continue
		}
		v, err := f.value(values[f.Name])
		if err != nil {
			return apiRequest{}, err
		}
		switch t := v.(type) {
		case []string:
			if f.In == "query" {
				query[f.Name] = t
				continue
			}
		default:
			switch f.In {
			case "query":
				query[f.Name] = []string{fmt.Sprint(t)}
				continue
			case "header":
				headers[f.Name] = fmt.Sprint(t)
				continue
			}
		}
		doc[f.Name] = v
	}
	req := apiRequest{}
	if op.BodyType != "" {
		if raw != "" {
			if len(doc) > 0 {
				return apiRequest{}, errorx.New(errorx.KindInvalidArgument, "--body replaces the body's flags; pass one or the other")
			}
			b, err := readValue(raw)
			if err != nil {
				return apiRequest{}, err
			}
			req.body = bytes.NewReader(b)
		} else {
			b, err := json.Marshal(doc)
			if err != nil {
				return apiRequest{}, errorx.Wrap(err, "body", errorx.KindInternal)
			}
			req.body = bytes.NewReader(b)
		}
	}
	req.edit = func(_ context.Context, r *http.Request) error {
		q := r.URL.Query()
		for n, v := range query {
			q[n] = v
		}
		r.URL.RawQuery = q.Encode()
		for k, v := range op.Headers {
			r.Header.Set(k, v)
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return nil
	}
	return req, nil
}

// value reads a flag's value as the field's type.
func (f apiField) value(v any) (any, error) {
	switch p := v.(type) {
	case *int:
		return *p, nil
	case *bool:
		return *p, nil
	case *[]string:
		return *p, nil
	case *string:
		if f.Kind != "json" {
			return *p, nil
		}
		raw, err := readValue(*p)
		if err != nil {
			return nil, err
		}
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, errorx.Wrap(err, "--"+f.flagName()+" is JSON", errorx.KindInvalidArgument)
		}
		return doc, nil
	}
	return nil, errorx.New(errorx.KindInternal, "flag "+f.Name+" has no value")
}

// readValue reads a value given inline, or from the file @path names.
func readValue(v string) ([]byte, error) {
	if path, ok := strings.CutPrefix(v, "@"); ok {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, errorx.Wrap(err, path, errorx.KindInvalidArgument)
		}
		return raw, nil
	}
	return []byte(v), nil
}

// secretFile is the file an answer carrying a secret is written to:
// created at mode 0600, and never over an existing file, before the
// request is sent.
type secretFile struct {
	path string
	f    *os.File
	kept bool
}

// createSecretFile creates path for an answer not yet asked for.
func createSecretFile(path string) (*secretFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, errorx.Wrap(err, path, errorx.KindFailedPrecondition)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, errorx.New(errorx.KindAlreadyExists, path+" exists; the answer is written to a new file only, and nothing was sent")
		}
		return nil, errorx.Wrap(err, path, errorx.KindFailedPrecondition)
	}
	return &secretFile{path: path, f: f}, nil
}

// keep writes the whole answer to the file and answers it without the
// secret fields, which is all that reaches stdout.
func (s *secretFile) keep(answer []byte, secrets []string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(answer, &doc); err != nil {
		return nil, errorx.Wrap(err, "the answer is not a JSON object", errorx.KindInternal)
	}
	if _, err := s.f.Write(answer); err != nil {
		return nil, errorx.Wrap(err, s.path, errorx.KindFailedPrecondition)
	}
	if err := s.f.Close(); err != nil {
		return nil, errorx.Wrap(err, s.path, errorx.KindFailedPrecondition)
	}
	s.kept = true
	for _, field := range secrets {
		delete(doc, field)
	}
	return json.Marshal(doc)
}

// discard removes the file unless keep wrote the answer to it.
func (s *secretFile) discard() {
	if s.kept {
		return
	}
	_ = s.f.Close()
	_ = os.Remove(s.path)
}

// renderAnswer prints an answer: as JSON it is the API's own, for
// people a table of the columns the specification declares.
func (c *userCommand) renderAnswer(answer []byte, columns []string, rowsIn string) error {
	if len(bytes.TrimSpace(answer)) == 0 {
		return nil
	}
	// An answer that declares nothing scalar has no table, so a person
	// reads it as a program does.
	if c.flags.output == outputJSON || len(columns) == 0 {
		var buf bytes.Buffer
		if err := json.Indent(&buf, answer, "", "  "); err != nil {
			return errorx.Wrap(err, "answer", errorx.KindInternal)
		}
		buf.WriteByte('\n')
		_, err := c.out.Write(buf.Bytes())
		return err
	}
	var rows []map[string]any
	var one map[string]any
	if err := json.Unmarshal(answer, &rows); err != nil {
		if err := json.Unmarshal(answer, &one); err != nil {
			_, err := c.out.Write(append(answer, '\n'))
			return err
		}
	}
	if rowsIn != "" && one != nil {
		list, _ := one[rowsIn].([]any)
		rows, one = make([]map[string]any, 0, len(list)), nil
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				rows = append(rows, m)
			}
		}
	}
	w := tabwriter.NewWriter(c.out, 0, 0, 2, ' ', 0)
	if one != nil {
		for _, col := range columns {
			_, _ = fmt.Fprintf(w, "%s\t%s\n", strings.ToUpper(col), cell(lookup(one, col)))
		}
		return w.Flush()
	}
	header := make([]string, len(columns))
	for i, col := range columns {
		header[i] = strings.ToUpper(col)
	}
	_, _ = fmt.Fprintln(w, strings.Join(header, "\t"))
	for _, row := range rows {
		cells := make([]string, len(columns))
		for i, col := range columns {
			cells[i] = cell(lookup(row, col))
		}
		_, _ = fmt.Fprintln(w, strings.Join(cells, "\t"))
	}
	return w.Flush()
}

// lookup reads a column from an answer object, following parent.child.
func lookup(m map[string]any, col string) any {
	var v any = m
	for _, part := range strings.Split(col, ".") {
		obj, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = obj[part]
	}
	return v
}

// cell renders a scalar for a table; an absent value is a dash.
func cell(v any) string {
	switch t := v.(type) {
	case nil:
		return "-"
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}
