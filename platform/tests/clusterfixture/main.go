// Command clusterfixture is the server fixture garam-bot-operator's
// cluster-connection v1alpha1 conformance suite runs against garam
// (`conformance.md` §The server fixture). Each subcommand acts through
// garam's browser API as a signed-in principal, and --expired and
// expire-cursors alone write to the database. It is test-only and no
// garam binary or image carries it; README.md beside it is the
// contract.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/tests/loopback"
)

// The settings the suite's environment carries. The session token is a
// credential, and nothing this command writes repeats it.
const (
	envAPIURL       = "GARAM_FIXTURE_API_URL"
	envSessionToken = "GARAM_FIXTURE_SESSION_TOKEN"
	envOrg          = "GARAM_FIXTURE_ORG"
	envDatabaseURL  = "GARAM_FIXTURE_DATABASE_URL"
)

// sessionCookie is the cookie serve api reads a session from.
const sessionCookie = "garam_session"

// requestTimeout bounds one call to garam.
const requestTimeout = 30 * time.Second

const usage = `usage: clusterfixture <subcommand> [arguments]
  mint-token [--cluster GRN] [--expired]
  declare FILE            FILE is {"cluster": GRN, "id": ID (optional), "object": DeclaredObject}
  tombstone ID
  revoke-keys GRN
  delete-cluster GRN
  latest-report GRN
  expire-cursors GRN
environment: ` + envAPIURL + `, ` + envSessionToken + `, ` + envOrg + `, and ` + envDatabaseURL + ` for --expired and expire-cursors`

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// refusal is the interface's error body (T-5), which a subcommand
// prints when garam refuses it or it fails.
type refusal struct {
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (r *refusal) Error() string { return r.Code + ": " + r.Message }

// errUsage is a command line run cannot act on.
var errUsage = errors.New("usage")

// run executes one subcommand and answers the exit code: 0 with its
// answer printed, 1 with T-5's body printed, 2 for a command line it
// cannot act on, with the usage on stderr.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	answer, err := dispatch(ctx, args, getenv)
	if errors.Is(err, errUsage) {
		_, _ = fmt.Fprintln(stderr, usage)
		return 2
	}
	if err != nil {
		var r *refusal
		if !errors.As(err, &r) {
			r = &refusal{Code: "internal", Message: err.Error()}
		}
		_ = json.NewEncoder(stdout).Encode(r)
		return 1
	}
	_ = json.NewEncoder(stdout).Encode(answer)
	return 0
}

func dispatch(ctx context.Context, args []string, getenv func(string) string) (any, error) {
	if len(args) == 0 {
		return nil, errUsage
	}
	f, err := newFixture(getenv)
	if err != nil {
		return nil, err
	}
	name, rest := args[0], args[1:]
	switch name {
	case "mint-token":
		flags := flag.NewFlagSet(name, flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		named := flags.String("cluster", "", "the record to mint for; a new one when empty")
		expired := flags.Bool("expired", false, "mint one already past REG-5's hour")
		if err := flags.Parse(rest); err != nil || flags.NArg() > 0 {
			return nil, errUsage
		}
		return f.mintToken(ctx, *named, *expired, getenv(envDatabaseURL))
	case "declare":
		if len(rest) != 1 {
			return nil, errUsage
		}
		read, err := os.ReadFile(rest[0])
		if err != nil {
			return nil, &refusal{Code: "invalid_request", Message: "the declare file could not be read"}
		}
		return f.declare(ctx, read)
	case "tombstone":
		if len(rest) != 1 {
			return nil, errUsage
		}
		return f.tombstone(ctx, rest[0])
	case "revoke-keys", "delete-cluster", "expire-cursors", "latest-report":
		if len(rest) != 1 {
			return nil, errUsage
		}
		g, err := clusterGRN(rest[0])
		if err != nil {
			return nil, err
		}
		switch name {
		case "revoke-keys":
			return f.revokeKeys(ctx, g)
		case "delete-cluster":
			return f.deleteCluster(ctx, g)
		case "latest-report":
			return f.latestReport(ctx, g)
		}
		return f.expireCursors(ctx, g, getenv(envDatabaseURL))
	}
	return nil, errUsage
}

// fixture is garam's browser API as the signed-in principal sees it.
type fixture struct {
	api     string
	session string
	org     string
	client  *http.Client
}

func newFixture(getenv func(string) string) (*fixture, error) {
	f := &fixture{
		api:     strings.TrimSuffix(getenv(envAPIURL), "/"),
		session: getenv(envSessionToken),
		org:     getenv(envOrg),
		client:  &http.Client{Timeout: requestTimeout},
	}
	for name, v := range map[string]string{envAPIURL: f.api, envSessionToken: f.session, envOrg: f.org} {
		if v == "" {
			return nil, &refusal{Code: "invalid_request", Message: name + " is required"}
		}
	}
	return f, nil
}

// minted is what mint-token prints.
type minted struct {
	RegistrationToken string `json:"registrationToken"`
	Cluster           string `json:"cluster"`
}

// mintToken creates a record and answers its registration token, or
// reissues the token of the record named. --expired then moves that
// token's expiry into the past in the database, the one write no
// browser route makes, since none should mint an expired token.
func (f *fixture) mintToken(ctx context.Context, named string, expired bool, databaseURL string) (minted, error) {
	// The database is checked before anything is minted, so a refused
	// --expired leaves no record or token behind.
	var database *pgxpool.Config
	if expired {
		cfg, err := localDatabase(databaseURL)
		if err != nil {
			return minted{}, err
		}
		database = cfg
	}
	var out minted
	if named == "" {
		if err := f.install(ctx); err != nil {
			return minted{}, err
		}
		var created apigen.CreatedInstance
		if err := f.call(ctx, http.MethodPost, installationPath(f.org)+"/instances", `{}`, &created, http.StatusCreated); err != nil {
			return minted{}, err
		}
		out = minted{RegistrationToken: created.Registration.RegistrationToken, Cluster: created.Instance.Grn}
	} else {
		g, err := clusterGRN(named)
		if err != nil {
			return minted{}, err
		}
		var reissued apigen.InstanceRegistrationToken
		if err := f.call(ctx, http.MethodPost, recordPath(g)+"/registration-token", "", &reissued, http.StatusCreated); err != nil {
			return minted{}, err
		}
		out = minted{RegistrationToken: reissued.RegistrationToken, Cluster: g.String()}
	}
	if database != nil {
		if err := expire(ctx, database, out.Cluster); err != nil {
			return minted{}, err
		}
	}
	return out, nil
}

// connectorSlug is the app the suite's records are instances of.
const connectorSlug = "garam-bot-connector"

// install installs garam-bot-connector in the organization, which
// creating its first record needs, and grants the installation bot
// hosting, which placing a declaration on its records needs (ADR-0192
// decision 10). One installed already answers 409, and stands; a grant
// repeated answers as the first.
func (f *fixture) install(ctx context.Context) error {
	if err := f.call(ctx, http.MethodPost, "/orgs/"+f.org+"/installations", `{"app":"`+connectorSlug+`"}`, nil,
		http.StatusCreated, http.StatusConflict); err != nil {
		return err
	}
	return f.call(ctx, http.MethodPut, installationPath(f.org)+"/bot-hosting", "", nil, http.StatusOK)
}

// localDatabase parses the database --expired and expire-cursors
// write, refusing one off this machine, as testprincipal does.
func localDatabase(databaseURL string) (*pgxpool.Config, error) {
	if databaseURL == "" {
		return nil, &refusal{Code: "invalid_request", Message: envDatabaseURL + " is required for --expired and expire-cursors"}
	}
	cfg, err := loopback.ParseConfig(databaseURL)
	if err != nil {
		return nil, &refusal{Code: "invalid_request", Message: err.Error()}
	}
	return cfg, nil
}

// expire moves cluster's registration token past its expiry.
func expire(ctx context.Context, cfg *pgxpool.Config, clusterGRN string) error {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return errors.New("the database could not be reached")
	}
	defer pool.Close()
	tag, err := pool.Exec(ctx,
		`UPDATE instance_registration_tokens SET expires_at = now() - interval '1 second' WHERE instance_grn = $1`, clusterGRN)
	if err != nil {
		return errors.New("the registration token could not be expired")
	}
	if tag.RowsAffected() != 1 {
		return errors.New("the registration token just minted is not in the database")
	}
	return nil
}

// named is what revoke-keys and delete-cluster print.
type named struct {
	Cluster string `json:"cluster"`
}

// revokeKeys withdraws every key of g the record has not withdrawn.
func (f *fixture) revokeKeys(ctx context.Context, g grn.GRN) (named, error) {
	var keys apigen.InstanceKeyList
	if err := f.call(ctx, http.MethodGet, recordPath(g)+"/keys", "", &keys, http.StatusOK); err != nil {
		return named{}, err
	}
	for _, k := range keys.Keys {
		if k.State == apigen.InstanceKeyRevoked {
			continue
		}
		if err := f.call(ctx, http.MethodDelete, recordPath(g)+"/keys/"+k.Kid, "", nil, http.StatusNoContent); err != nil {
			return named{}, err
		}
	}
	return named{Cluster: g.String()}, nil
}

func (f *fixture) deleteCluster(ctx context.Context, g grn.GRN) (named, error) {
	if err := f.call(ctx, http.MethodDelete, recordPath(g), "", nil, http.StatusNoContent); err != nil {
		return named{}, err
	}
	return named{Cluster: g.String()}, nil
}

// clusterGRN parses an instance's GRN, the one form a record is named
// by on the suite's command line.
func clusterGRN(raw string) (grn.GRN, error) {
	g, err := grn.Parse(raw)
	if err != nil || !app.IsInstanceGRN(g) {
		return grn.GRN{}, &refusal{Code: "invalid_request", Message: "not an instance's GRN"}
	}
	return g, nil
}

// installationPath is the browser API's path of org's installation of
// garam-bot-connector.
func installationPath(org string) string {
	return "/orgs/" + org + "/installations/" + connectorSlug
}

// recordPath is the browser API's path of g's record, an instance of
// that installation.
func recordPath(g grn.GRN) string {
	return installationPath(g.Organization) + "/instances/" + g.Identifier
}

// call sends one request as the principal and decodes an answer of one
// of statuses into out. Any other answer is a refusal in T-5's shape.
func (f *fixture) call(ctx context.Context, method, path, body string, out any, statuses ...int) error {
	req, err := http.NewRequestWithContext(ctx, method, f.api+path, bytes.NewReader([]byte(body)))
	if err != nil {
		return errors.New("the request could not be built")
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: f.session})
	resp, err := f.client.Do(req)
	if err != nil {
		return errors.New("garam could not be reached at " + envAPIURL)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return errors.New("garam's answer could not be read")
	}
	if !slices.Contains(statuses, resp.StatusCode) {
		var e apigen.Error
		_ = json.Unmarshal(raw, &e)
		return &refusal{Code: codeOf(resp.StatusCode), Message: e.Message}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return errors.New("garam's answer is not what " + method + " " + path + " answers")
	}
	return nil
}

// codeOf names a browser API status as the interface's codes do, by
// the status alone, as T-5 has a client read a code it does not know.
func codeOf(status int) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "unauthorized"
	case status == http.StatusNotFound:
		return "not_found"
	case status == http.StatusTooManyRequests:
		return "throttled"
	case status >= http.StatusInternalServerError:
		return "internal"
	}
	return "invalid_request"
}

// declared is what declare and tombstone print.
type declared struct {
	ID         string `json:"id"`
	Generation int64  `json:"generation"`
}

// declareFile is declare's FILE.
type declareFile struct {
	Cluster string          `json:"cluster"`
	ID      string          `json:"id"`
	Object  json.RawMessage `json:"object"`
}

// declare places the file's object on its record: under an identifier
// garam draws where the file names none, or as the next generation of
// the one it names.
func (f *fixture) declare(ctx context.Context, file []byte) (declared, error) {
	var q declareFile
	if err := json.Unmarshal(file, &q); err != nil {
		return declared{}, &refusal{Code: "invalid_request", Message: "the declare file is not {cluster, id, object}"}
	}
	g, err := clusterGRN(q.Cluster)
	if err != nil {
		return declared{}, err
	}
	body, err := json.Marshal(struct {
		Object json.RawMessage `json:"object"`
	}{q.Object})
	if err != nil {
		return declared{}, &refusal{Code: "invalid_request", Message: "the declare file's object is not JSON"}
	}
	var out apigen.Declaration
	if q.ID == "" {
		err = f.call(ctx, http.MethodPost, recordPath(g)+"/declarations", string(body), &out, http.StatusCreated)
	} else {
		err = f.call(ctx, http.MethodPut, recordPath(g)+"/declarations/"+url.PathEscape(q.ID), string(body), &out,
			http.StatusCreated, http.StatusOK)
	}
	if err != nil {
		return declared{}, err
	}
	return declared{ID: out.Id, Generation: out.Generation}, nil
}

// tombstone deletes the live declaration id, on whichever record of
// the organization holds it, and answers the generation the delete
// carries.
func (f *fixture) tombstone(ctx context.Context, id string) (declared, error) {
	var records []apigen.Instance
	err := f.call(ctx, http.MethodGet, installationPath(f.org)+"/instances", "", &records, http.StatusOK)
	var r *refusal
	if errors.As(err, &r) && r.Code == "not_found" {
		return declared{}, &refusal{Code: "not_found", Message: "no live declaration " + id + " in the organization"}
	}
	if err != nil {
		return declared{}, err
	}
	for _, c := range records {
		g, err := clusterGRN(c.Grn)
		if err != nil {
			return declared{}, err
		}
		path := recordPath(g) + "/declarations/" + url.PathEscape(id)
		var live apigen.Declaration
		err = f.call(ctx, http.MethodGet, path, "", &live, http.StatusOK)
		var r *refusal
		if errors.As(err, &r) && r.Code == "not_found" {
			continue
		}
		if err != nil {
			return declared{}, err
		}
		if err := f.call(ctx, http.MethodDelete, path, "", nil, http.StatusNoContent); err != nil {
			return declared{}, err
		}
		return declared{ID: id, Generation: live.Generation + 1}, nil
	}
	return declared{}, &refusal{Code: "not_found", Message: "no live declaration " + id + " in the organization"}
}

// latestReport reads g's kept report and its lastContactAt, each null
// where there is none, as getInstanceReport answers them: cluster,
// lastContactAt and report are the members the suite reads, the record
// under the name the suite's contract gives it.
func (f *fixture) latestReport(ctx context.Context, g grn.GRN) (map[string]json.RawMessage, error) {
	var view map[string]json.RawMessage
	if err := f.call(ctx, http.MethodGet, recordPath(g)+"/report", "", &view, http.StatusOK); err != nil {
		return nil, err
	}
	view["cluster"] = view["instance"]
	delete(view, "instance")
	return view, nil
}

// expireCursors purges g's log and every cursor answered so far
// (DECL-6), the one write no browser route makes: the purged position
// becomes the head, and the head moves one past it, so the full set
// read next answers a cursor that is live. It wakes g's held polls.
func (f *fixture) expireCursors(ctx context.Context, g grn.GRN, databaseURL string) (named, error) {
	cfg, err := localDatabase(databaseURL)
	if err != nil {
		return named{}, err
	}
	// The record is read through garam first, so one the principal
	// cannot reach is refused as every other subcommand refuses it.
	if err := f.call(ctx, http.MethodGet, recordPath(g), "", nil, http.StatusOK); err != nil {
		return named{}, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return named{}, errors.New("the database could not be reached")
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return named{}, errors.New("the database could not be reached")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range []string{
		`INSERT INTO bot_declaration_heads (instance_grn, head, purged_through) VALUES ($1, 1, 0)
		 ON CONFLICT (instance_grn) DO UPDATE
		 SET purged_through = bot_declaration_heads.head, head = bot_declaration_heads.head + 1`,
		`DELETE FROM bot_declaration_changes WHERE instance_grn = $1`,
		`SELECT pg_notify('garam_bot_declarations', $1)`,
	} {
		if _, err := tx.Exec(ctx, statement, g.String()); err != nil {
			return named{}, errors.New("the record's cursors could not be expired")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return named{}, errors.New("the record's cursors could not be expired")
	}
	return named{Cluster: g.String()}, nil
}
