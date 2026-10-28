// Command testprincipal prepares a disposable Garam database holding
// one signed-in principal, for tests in other repositories that call
// Garam's public API. It is test-only and no Garam binary or image
// carries it (ADR-0081); README.md beside it is the contract.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/garamsh/garam/platform/internal/authn"
	authnpg "github.com/garamsh/garam/platform/internal/authn/postgres"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/user"
	userpg "github.com/garamsh/garam/platform/internal/user/postgres"
)

// contractVersion is the one contract this build serves. A caller
// pins it with -contract; any other value is refused before anything
// is read or written.
const contractVersion = 1

// sessionCookie is the cookie the api reads a session from.
const sessionCookie = "garam_session"

// maxSessionTTL bounds the credential prepare hands out.
const maxSessionTTL = 24 * time.Hour

// Prepared is what prepare writes to stdout: the minimum a caller
// needs to start Garam against the database and call its public API
// as the principal.
type Prepared struct {
	Contract    int       `json:"contract"`
	Database    string    `json:"database"`
	DatabaseURL string    `json:"databaseUrl"`
	UserGRN     string    `json:"userGrn"`
	Session     Session   `json:"session"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

// Session is the principal's signed-in browser session.
type Session struct {
	Cookie string `json:"cookie"`
	Token  string `json:"token"`
}

const usage = `usage:
  testprincipal prepare -contract 1 -server-url URL -migrations DIR [-session-ttl 1h]
  testprincipal drop    -contract 1 -server-url URL -database NAME`

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one subcommand and answers the exit code: 0 on
// success, 1 when the work failed, 2 when the invocation is refused.
// Nothing written to stderr carries a password or a session token.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	contract := fs.String("contract", "", "the contract version the caller was written against")
	serverURL := fs.String("server-url", "", "a connection string to a loopback Postgres server")
	migrations := fs.String("migrations", "", "the migrations directory of this Garam checkout")
	database := fs.String("database", "", "the database prepare created")
	ttl := fs.Duration("session-ttl", time.Hour, "how long the session lives")
	if err := fs.Parse(args[1:]); err != nil {
		fmt.Fprintf(stderr, "testprincipal: %s\n%s\n", err, usage)
		return 2
	}
	if *contract != strconv.Itoa(contractVersion) {
		fmt.Fprintf(stderr, "testprincipal: this build serves contract %d and the caller asked for %q; "+
			"build testprincipal from the Garam revision your tests pin\n", contractVersion, *contract)
		return 2
	}
	var err error
	switch args[0] {
	case "prepare":
		err = prepare(ctx, *serverURL, *migrations, *ttl, stdout)
	case "drop":
		err = drop(ctx, *serverURL, *database)
	default:
		fmt.Fprintf(stderr, "testprincipal: unknown command %q\n%s\n", args[0], usage)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "testprincipal: "+scrub(err.Error(), *serverURL))
		if errorx.IsKind(err, errorx.KindInvalidArgument) {
			return 2
		}
		return 1
	}
	return 0
}

// scrub removes the connection string and its password from msg. The
// password is removed where it stands as a word of its own, so a
// password that is part of a longer name — garam in garam_principal_ —
// leaves that name readable.
func scrub(msg, dsn string) string {
	if dsn == "" {
		return msg
	}
	msg = strings.ReplaceAll(msg, dsn, "[connection string]")
	if pw := passwordOf(dsn); pw != "" {
		word := regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(pw) + `($|[^A-Za-z0-9_])`)
		msg = word.ReplaceAllString(msg, "${1}[password]${2}")
	}
	return msg
}

// prepare creates a fresh database, migrates it, signs one principal
// in, and writes the result. Any failure after the database exists,
// writing the result included, drops it again, or names it as left
// behind with the reason.
func prepare(ctx context.Context, serverURL, migrations string, ttl time.Duration, stdout io.Writer) error {
	if migrations == "" {
		return errorx.New(errorx.KindInvalidArgument, "-migrations is required")
	}
	if ttl <= 0 || ttl > maxSessionTTL {
		return errorx.New(errorx.KindInvalidArgument, "-session-ttl must be positive and at most 24h")
	}
	dir, err := migrationsDir(migrations)
	if err != nil {
		return err
	}
	admin, err := parseLocal(serverURL)
	if err != nil {
		return err
	}
	suffix, err := randomSuffix()
	if err != nil {
		return err
	}
	name := databasePrefix + suffix
	databaseURL, target, err := withDatabase(serverURL, name)
	if err != nil {
		return err
	}
	conn, err := connect(ctx, admin)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	if err := createDatabase(ctx, conn, name); err != nil {
		return err
	}
	err = complete(ctx, conn, target, name, dir, ttl, func(out *Prepared) error {
		out.Database = name
		out.DatabaseURL = databaseURL
		return json.NewEncoder(stdout).Encode(out)
	})
	if err == nil {
		return nil
	}
	// This invocation created name a moment ago, so it is dropped
	// whether or not the marker was set.
	if dropErr := dropDatabase(ctx, conn, name); dropErr != nil {
		return errorx.Wrap(err, "database "+name+" was left behind ("+dropErr.Error()+")")
	}
	return err
}

// complete marks, migrates and seeds the database prepare created,
// then hands the result to write.
func complete(ctx context.Context, conn *pgx.Conn, target *pgxpool.Config, name, dir string,
	ttl time.Duration, write func(*Prepared) error) error {
	if err := markOwned(ctx, conn, name); err != nil {
		return err
	}
	out, err := seed(ctx, target, dir, ttl)
	if err != nil {
		return err
	}
	if err := write(out); err != nil {
		return errorx.Wrap(err, "write the result")
	}
	return nil
}

// seed migrates the new database and writes the principal and its
// session through Garam's own stores.
func seed(ctx context.Context, target *pgxpool.Config, dir string, ttl time.Duration) (*Prepared, error) {
	if err := migrateUp(target, dir); err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, target)
	if err != nil {
		return nil, errorx.Wrap(err, "connect to the new database", errorx.KindUnavailable)
	}
	defer pool.Close()

	suffix, err := randomSuffix()
	if err != nil {
		return nil, err
	}
	id := "principal-" + suffix
	g := grn.New(grn.RootOwner, grn.DefaultProject, user.ServiceType, id)
	if err := userpg.New(pool).Create(ctx, user.New(g, id)); err != nil {
		return nil, errorx.Wrap(err, "create the principal")
	}
	token, err := authn.NewToken()
	if err != nil {
		return nil, err
	}
	session := authn.NewSession(g, ttl)
	if err := authnpg.NewSessionStore(pool).Create(ctx, token, session); err != nil {
		return nil, errorx.Wrap(err, "sign the principal in")
	}
	return &Prepared{
		Contract:  contractVersion,
		UserGRN:   g.String(),
		Session:   Session{Cookie: sessionCookie, Token: token},
		ExpiresAt: session.ExpiresAt,
	}, nil
}

// drop removes a database prepare created.
func drop(ctx context.Context, serverURL, database string) error {
	admin, err := parseLocal(serverURL)
	if err != nil {
		return err
	}
	return dropOwned(ctx, admin, database)
}
