// Command cligen derives the garam CLI's API commands from the browser
// API's specification (ADR-0122). It reads api/openapi.yaml and
// internal/cli/commands.yaml, places every operation, and writes
// internal/cli/commands.gen.go: a table the CLI builds its commands
// from, and one call per operation into the client oapi-codegen
// generates from the same specification.
//
// An operation the two files leave unplaced, a name commands.yaml
// gives that is no operation, and two operations deriving one command
// each fail the run, so the CLI cannot fall behind the API unnoticed.
//
//	go run ./internal/cli/cligen
package main

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

const (
	specPath   = "api/openapi.yaml"
	configPath = "internal/cli/commands.yaml"
	outPath    = "internal/cli/commands.gen.go"
)

// config is commands.yaml.
type config struct {
	Exclude map[string]string   `yaml:"exclude"`
	Hand    map[string]string   `yaml:"hand"`
	Secrets map[string][]string `yaml:"secrets"`
	Names   map[string]string   `yaml:"names"`
	Headers map[string]string   `yaml:"headers"`
	Pending []string            `yaml:"pending"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "cligen:", err)
		os.Exit(1)
	}
}

func run() error {
	var doc yaml.Node
	raw, err := os.ReadFile(specPath)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%s: %w", specPath, err)
	}
	var cfg config
	rawCfg, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(rawCfg, &cfg); err != nil {
		return fmt.Errorf("%s: %w", configPath, err)
	}
	s := spec{root: doc.Content[0]}
	ops, err := s.operations()
	if err != nil {
		return err
	}
	cmds, err := place(ops, cfg)
	if err != nil {
		return err
	}
	src, err := render(cmds)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, src, 0o644)
}

// spec reads what cligen needs out of the specification's node tree,
// keeping the order properties are declared in, which a table's
// columns follow.
type spec struct{ root *yaml.Node }

// field is a parameter or a body property.
type field struct {
	Name     string
	In       string // path, query, header or body
	Kind     string // string, integer, boolean, strings, json
	Required bool
	Format   string
	Help     string
}

// operation is one operation as cligen reads it.
type operation struct {
	ID       string
	Method   string
	Path     string
	Summary  string
	Params   []field
	BodyType string
	Body     []field
	Columns  []string
	// Rows is the property of an answer object whose array is the
	// table's rows, empty where the answer itself is the array or one
	// row.
	Rows string
	// Public is an operation that declares `security: []`, which a
	// caller reaches with no session.
	Public bool
}

func get(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func (s spec) deref(n *yaml.Node) *yaml.Node {
	for n != nil {
		ref := get(n, "$ref")
		if ref == nil {
			return n
		}
		target := s.root
		for _, part := range strings.Split(strings.TrimPrefix(ref.Value, "#/"), "/") {
			target = get(target, part)
		}
		n = target
	}
	return nil
}

var methods = []string{"get", "post", "put", "patch", "delete"}

func (s spec) operations() ([]operation, error) {
	paths := get(s.root, "paths")
	var ops []operation
	for i := 0; i+1 < len(paths.Content); i += 2 {
		path, item := paths.Content[i].Value, paths.Content[i+1]
		for _, m := range methods {
			op := get(item, m)
			if op == nil {
				continue
			}
			o := operation{ID: get(op, "operationId").Value, Method: strings.ToUpper(m), Path: path}
			if sum := get(op, "summary"); sum != nil {
				o.Summary = sum.Value
			}
			params := get(op, "parameters")
			if params != nil {
				for _, p := range params.Content {
					p = s.deref(p)
					f := field{Name: get(p, "name").Value, In: get(p, "in").Value}
					if r := get(p, "required"); r != nil {
						f.Required = r.Value == "true"
					}
					if d := get(p, "description"); d != nil {
						f.Help = d.Value
					}
					f.Kind, f.Format = s.kind(get(p, "schema"))
					o.Params = append(o.Params, f)
				}
			}
			if rb := s.deref(get(op, "requestBody")); rb != nil {
				content := get(rb, "content")
				o.BodyType = content.Content[0].Value
				schema := s.deref(get(content.Content[1], "schema"))
				o.Body = s.properties(schema, "body")
			}
			o.Columns, o.Rows = s.columns(op)
			if sec := get(op, "security"); sec != nil && sec.Kind == yaml.SequenceNode && len(sec.Content) == 0 {
				o.Public = true
			}
			ops = append(ops, o)
		}
	}
	return ops, nil
}

// kind reads a schema as a flag can carry it.
func (s spec) kind(schema *yaml.Node) (kind, format string) {
	schema = s.deref(schema)
	if f := get(schema, "format"); f != nil {
		format = f.Value
	}
	switch t := typeOf(schema); t {
	case "string", "integer", "boolean":
		return t, format
	case "array":
		if typeOf(s.deref(get(schema, "items"))) == "string" {
			return "strings", format
		}
	}
	return "json", format
}

// typeOf reads a schema's type, taking the first non-null one of a
// list as OpenAPI 3.1 writes a nullable type.
func typeOf(schema *yaml.Node) string {
	t := get(schema, "type")
	if t == nil {
		return ""
	}
	if t.Kind == yaml.SequenceNode {
		for _, v := range t.Content {
			if v.Value != "null" {
				return v.Value
			}
		}
		return ""
	}
	return t.Value
}

func (s spec) properties(schema *yaml.Node, in string) []field {
	required := map[string]bool{}
	if r := get(schema, "required"); r != nil {
		for _, v := range r.Content {
			required[v.Value] = true
		}
	}
	props := get(schema, "properties")
	var out []field
	if props == nil {
		return out
	}
	for i := 0; i+1 < len(props.Content); i += 2 {
		name, p := props.Content[i].Value, props.Content[i+1]
		f := field{Name: name, In: in, Required: required[name]}
		f.Kind, f.Format = s.kind(p)
		if d := get(s.deref(p), "description"); d != nil {
			f.Help = d.Value
		}
		out = append(out, f)
	}
	return out
}

// columns are the scalar properties of a successful answer's object,
// or of the items of its array, in declared order, with an object
// property's own scalars one level down as parent.child. An answer
// object whose one array property holds objects is tabulated by those
// objects, and rows names that property.
func (s spec) columns(op *yaml.Node) (cols []string, rows string) {
	responses := get(op, "responses")
	for i := 0; i+1 < len(responses.Content); i += 2 {
		if !strings.HasPrefix(responses.Content[i].Value, "2") {
			continue
		}
		r := s.deref(responses.Content[i+1])
		content := get(r, "content")
		if content == nil {
			return nil, ""
		}
		schema := s.deref(get(content.Content[1], "schema"))
		if typeOf(schema) == "array" {
			schema = s.deref(get(schema, "items"))
		} else if name, items := s.onlyArrayOfObjects(schema); name != "" {
			schema, rows = items, name
		}
		return s.scalars(schema, "", 1), rows
	}
	return nil, ""
}

// scalars lists schema's scalar properties, and depth levels of its
// object properties' scalars beneath them.
func (s spec) scalars(schema *yaml.Node, prefix string, depth int) []string {
	var cols []string
	props := get(schema, "properties")
	if props == nil {
		return nil
	}
	for i := 0; i+1 < len(props.Content); i += 2 {
		name, p := props.Content[i].Value, s.deref(props.Content[i+1])
		switch typeOf(p) {
		case "string", "integer", "boolean":
			cols = append(cols, prefix+name)
		case "object":
			if depth > 0 {
				cols = append(cols, s.scalars(p, prefix+name+".", depth-1)...)
			}
		}
	}
	return cols
}

// onlyArrayOfObjects answers the one property of schema that is an
// array of objects, where it has exactly one and nothing else is an
// array.
func (s spec) onlyArrayOfObjects(schema *yaml.Node) (string, *yaml.Node) {
	props := get(schema, "properties")
	if props == nil {
		return "", nil
	}
	name, rowItems := "", (*yaml.Node)(nil)
	for i := 0; i+1 < len(props.Content); i += 2 {
		p := s.deref(props.Content[i+1])
		if typeOf(p) != "array" {
			continue
		}
		items := s.deref(get(p, "items"))
		if typeOf(items) != "object" || name != "" {
			return "", nil
		}
		name, rowItems = props.Content[i].Value, items
	}
	return name, rowItems
}

// command is an operation placed in the CLI.
type command struct {
	operation
	Words   []string
	Verb    string
	Args    []string
	FromOrg bool
	Secrets []string
	Headers map[string]string
}

var camel = regexp.MustCompile(`^[a-z]+`)

// place derives each generated operation's command and checks every
// operation is placed exactly once.
func place(ops []operation, cfg config) ([]command, error) {
	known := map[string]bool{}
	for _, o := range ops {
		known[o.ID] = true
	}
	pending := map[string]bool{}
	for _, id := range cfg.Pending {
		pending[id] = true
	}
	for list, names := range map[string][]string{
		"exclude": keys(cfg.Exclude), "hand": keys(cfg.Hand), "secrets": keys(cfg.Secrets),
		"names": keys(cfg.Names), "pending": cfg.Pending,
	} {
		for _, id := range names {
			if !known[id] {
				return nil, fmt.Errorf("%s names %s under %s, which %s does not declare", configPath, id, list, specPath)
			}
		}
	}
	var cmds []command
	seen := map[string]string{}
	for _, o := range ops {
		_, excluded := cfg.Exclude[o.ID]
		_, hand := cfg.Hand[o.ID]
		placed := 0
		for _, b := range []bool{excluded, hand, pending[o.ID]} {
			if b {
				placed++
			}
		}
		if placed > 1 {
			return nil, fmt.Errorf("%s is placed more than once in %s", o.ID, configPath)
		}
		if placed == 1 {
			continue
		}
		c := derive(o, cfg)
		name := strings.Join(append(append([]string{}, c.Words...), c.Verb), " ")
		if other, ok := seen[name]; ok {
			return nil, fmt.Errorf("%s and %s both derive the command %q; name one in %s", other, o.ID, name, configPath)
		}
		seen[name] = o.ID
		cmds = append(cmds, c)
	}
	return cmds, nil
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// derive names an operation's command: the nouns are the path's static
// segments, the verb is the operationId's first word, and the path
// parameters are its arguments. Under /orgs/{org}/..., the org is the
// one the command acts in, read from --org or the saved default.
func derive(o operation, cfg config) command {
	c := command{operation: o, Verb: camel.FindString(o.ID), Secrets: cfg.Secrets[o.ID], Headers: map[string]string{}}
	segments := strings.Split(strings.Trim(o.Path, "/"), "/")
	for i, seg := range segments {
		if strings.HasPrefix(seg, "{") {
			name := strings.Trim(seg, "{}")
			if name == "org" && len(segments) > i+1 {
				c.FromOrg = true
				continue
			}
			c.Args = append(c.Args, name)
			continue
		}
		if seg == "orgs" && len(segments) > i+2 {
			continue
		}
		c.Words = append(c.Words, seg)
	}
	// A path that ends in its own verb, as /invitations/{token}/accept
	// does, names the verb once.
	if n := len(c.Words); n > 1 && c.Words[n-1] == c.Verb {
		c.Words = c.Words[:n-1]
	}
	if name, ok := cfg.Names[o.ID]; ok {
		words := strings.Fields(name)
		c.Words, c.Verb = words[:len(words)-1], words[len(words)-1]
	}
	for _, p := range o.Params {
		if p.In == "header" {
			if v, ok := cfg.Headers[p.Name]; ok {
				c.Headers[p.Name] = v
			}
		}
	}
	return c
}

// goName is the name oapi-codegen gives an operation's method.
func goName(id string) string {
	r := []rune(id)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func render(cmds []command) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`// Code generated by cligen from api/openapi.yaml and internal/cli/commands.yaml. DO NOT EDIT.

package cli

import (
	"context"
	"io"
	"net/http"

	"github.com/google/uuid"

	apigen "github.com/garamsh/garam/platform/api"
)

var _ = uuid.Parse

// apiOperations are the browser API operations the CLI derives a
// command for (ADR-0122).
var apiOperations = []apiOperation{
`)
	for _, c := range cmds {
		fmt.Fprintf(&b, "\t{\n\t\tID: %q, Method: %q, Path: %q,\n", c.ID, c.Method, c.Path)
		fmt.Fprintf(&b, "\t\tWords: %#v, Verb: %q, Short: %q,\n", c.Words, c.Verb, c.Summary)
		fmt.Fprintf(&b, "\t\tArgs: %#v, FromOrg: %v,\n", c.Args, c.FromOrg)
		var fields []field
		for _, p := range c.Params {
			if p.In == "query" || (p.In == "header" && c.Headers[p.Name] == "") {
				fields = append(fields, p)
			}
		}
		fields = append(fields, c.Body...)
		if len(fields) > 0 {
			b.WriteString("\t\tFields: []apiField{\n")
			for _, f := range fields {
				fmt.Fprintf(&b, "\t\t\t{Name: %q, In: %q, Kind: %q, Required: %v, Help: %q},\n", f.Name, f.In, f.Kind, f.Required, firstSentence(f.Help))
			}
			b.WriteString("\t\t},\n")
		}
		if c.BodyType != "" {
			fmt.Fprintf(&b, "\t\tBodyType: %q,\n", c.BodyType)
		}
		if len(c.Headers) > 0 {
			fmt.Fprintf(&b, "\t\tHeaders: %#v,\n", c.Headers)
		}
		// A secret is never a column: the table is stdout.
		var cols []string
		for _, col := range c.Columns {
			if !contains(c.Secrets, col) {
				cols = append(cols, col)
			}
		}
		if len(cols) > 0 {
			fmt.Fprintf(&b, "\t\tColumns: %#v,\n", cols)
		}
		if c.Public {
			b.WriteString("\t\tPublic: true,\n")
		}
		if c.Rows != "" {
			fmt.Fprintf(&b, "\t\tRows: %q,\n", c.Rows)
		}
		if len(c.Secrets) > 0 {
			fmt.Fprintf(&b, "\t\tSecrets: %#v,\n", c.Secrets)
		}
		b.WriteString("\t\tcall: " + callFor(c) + ",\n\t},\n")
	}
	b.WriteString("}\n")
	return format.Source(b.Bytes())
}

// callFor is the call into the generated client: every path parameter
// in path order, a nil params struct where the operation has one (the
// query and headers travel on the request editor), and the body where
// it takes one.
func callFor(c command) string {
	var args, conv []string
	i := 0
	hasParams := false
	for _, p := range c.Params {
		switch p.In {
		case "path":
			v := fmt.Sprintf("path[%d]", i)
			if p.Format == "uuid" {
				conv = append(conv, fmt.Sprintf("p%d, err := uuid.Parse(%s)\n\t\t\tif err != nil {\n\t\t\t\treturn nil, err\n\t\t\t}", i, v))
				v = fmt.Sprintf("p%d", i)
			}
			args = append(args, v)
			i++
		case "query", "header":
			hasParams = true
		}
	}
	if hasParams {
		args = append(args, "nil")
	}
	method := goName(c.ID)
	if c.BodyType != "" {
		method += "WithBody"
		args = append(args, "contentType", "body")
	}
	args = append(args, "edit")
	pre := ""
	if len(conv) > 0 {
		pre = strings.Join(conv, "\n\t\t\t") + "\n\t\t\t"
	}
	return fmt.Sprintf("func(ctx context.Context, c apigen.ClientInterface, path []string, contentType string, body io.Reader, edit apigen.RequestEditorFn) (*http.Response, error) {\n\t\t\t%sreturn c.%s(ctx, %s)\n\t\t}",
		pre, method, strings.Join(args, ", "))
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// firstSentence keeps a flag's help to one line.
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	return s
}
