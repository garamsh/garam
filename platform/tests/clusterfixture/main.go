// Command clusterfixture is the server fixture garam-bot-operator's
// cluster-connection v1alpha1 conformance suite runs against garam
// (`conformance.md` §The server fixture). Each subcommand acts through
// garam's browser API as a signed-in principal, and --expired alone
// writes to the database. It is test-only and no garam binary or image
// carries it; README.md beside it is the contract.
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
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/lib/grn"
	apigen "github.com/garamsh/garam/platform/api"
	"github.com/garamsh/garam/platform/internal/cluster"
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
  revoke-keys GRN
  delete-cluster GRN
environment: ` + envAPIURL + `, ` + envSessionToken + `, ` + envOrg + `, and ` + envDatabaseURL + ` for --expired`

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
	case "revoke-keys", "delete-cluster":
		if len(rest) != 1 {
			return nil, errUsage
		}
		g, err := clusterGRN(rest[0])
		if err != nil {
			return nil, err
		}
		if name == "revoke-keys" {
			return f.revokeKeys(ctx, g)
		}
		return f.deleteCluster(ctx, g)
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
		var created apigen.CreatedCluster
		if err := f.call(ctx, http.MethodPost, "/orgs/"+f.org+"/clusters", `{}`, http.StatusCreated, &created); err != nil {
			return minted{}, err
		}
		out = minted{RegistrationToken: created.Registration.RegistrationToken, Cluster: created.Cluster.Grn}
	} else {
		g, err := clusterGRN(named)
		if err != nil {
			return minted{}, err
		}
		var reissued apigen.ClusterRegistrationToken
		if err := f.call(ctx, http.MethodPost, recordPath(g)+"/registration-token", "", http.StatusCreated, &reissued); err != nil {
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

// localDatabase parses --expired's database, refusing one off this
// machine, as testprincipal does.
func localDatabase(databaseURL string) (*pgxpool.Config, error) {
	if databaseURL == "" {
		return nil, &refusal{Code: "invalid_request", Message: envDatabaseURL + " is required for --expired"}
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
		`UPDATE cluster_registration_tokens SET expires_at = now() - interval '1 second' WHERE cluster_grn = $1`, clusterGRN)
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
	var keys apigen.ClusterKeyList
	if err := f.call(ctx, http.MethodGet, recordPath(g)+"/keys", "", http.StatusOK, &keys); err != nil {
		return named{}, err
	}
	for _, k := range keys.Keys {
		if k.State == apigen.ClusterKeyRevoked {
			continue
		}
		if err := f.call(ctx, http.MethodDelete, recordPath(g)+"/keys/"+k.Kid, "", http.StatusNoContent, nil); err != nil {
			return named{}, err
		}
	}
	return named{Cluster: g.String()}, nil
}

func (f *fixture) deleteCluster(ctx context.Context, g grn.GRN) (named, error) {
	if err := f.call(ctx, http.MethodDelete, recordPath(g), "", http.StatusNoContent, nil); err != nil {
		return named{}, err
	}
	return named{Cluster: g.String()}, nil
}

// clusterGRN parses a connected cluster's GRN, the one form a record is
// named by on the suite's command line.
func clusterGRN(raw string) (grn.GRN, error) {
	g, err := grn.Parse(raw)
	if err != nil || g.ServiceType != cluster.ServiceType {
		return grn.GRN{}, &refusal{Code: "invalid_request", Message: "not a connected cluster's GRN"}
	}
	return g, nil
}

// recordPath is the browser API's path of g's record.
func recordPath(g grn.GRN) string {
	return "/orgs/" + g.Organization + "/clusters/" + g.Identifier
}

// call sends one request as the principal and decodes an answer of
// status into out. Any other answer is a refusal in T-5's shape.
func (f *fixture) call(ctx context.Context, method, path, body string, status int, out any) error {
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
	if resp.StatusCode != status {
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
