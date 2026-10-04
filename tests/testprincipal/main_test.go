package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// unreachable names a loopback port nothing listens on, so a test
// that passes with it proves the refusal came before any connection.
const unreachable = "postgres://garam:pa55-word@127.0.0.1:1/postgres?sslmode=disable"

func TestParseLocal_AcceptsOnlyTargetsOnThisMachine(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		ok   bool
	}{
		{"loopback address", "postgres://u:p@127.0.0.1:5432/db", true},
		{"ipv6 loopback", "postgres://u:p@[::1]:5432/db", true},
		{"localhost", "postgres://u:p@localhost:5432/db", true},
		{"unix socket keyword form", "host=/var/run/postgresql dbname=db", true},
		{"remote host", "postgres://u:p@db.example.com:5432/db", false},
		{"remote address", "postgres://u:p@10.0.0.5:5432/db", false},
		{"query parameter overrides a local host", "postgres://u:p@localhost:5432/db?host=10.0.0.5", false},
		{"second of several hosts is remote", "postgres://u:p@localhost:5432,10.0.0.5:5432/db", false},
		{"keyword form remote", "host=10.0.0.5 dbname=db", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseLocal(tc.dsn)
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, errNotLoopback)
			}
		})
	}
}

// TestParseLocal_RefusesADialThatLeavesTheMachine holds the check on
// the address dialled, not the host written: localhost resolving to
// a remote address is refused, and a loopback dial is the control.
func TestParseLocal_RefusesADialThatLeavesTheMachine(t *testing.T) {
	cfg, err := parseLocal("postgres://u:p@localhost:5432/db")
	require.NoError(t, err)
	dial := cfg.ConnConfig.DialFunc

	_, err = dial(context.Background(), "tcp", "10.0.0.5:5432")
	assert.ErrorIs(t, err, errNotLoopback)
	_, err = dial(context.Background(), "tcp", "127.0.0.1:1")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, errNotLoopback, "a loopback dial reaches the network and fails there")
}

func TestWithDatabase_SelectsTheNewDatabaseOverEveryWayTheInputNamedOne(t *testing.T) {
	tests := []struct{ name, dsn string }{
		{"url path", "postgres://u:p@localhost:5432/garam_test?sslmode=disable"},
		{"url query dbname", "postgres://u:p@localhost:5432/garam_test?dbname=other"},
		{"keyword form", "host=localhost dbname=garam_test user=u"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, cfg, err := withDatabase(tc.dsn, "garam_principal_0123456789abcdef")
			require.NoError(t, err)
			assert.Equal(t, "garam_principal_0123456789abcdef", cfg.ConnConfig.Database)
		})
	}
}

func TestRun_RefusesAnotherContractVersionBeforeConnecting(t *testing.T) {
	for _, contract := range []string{"2", "0", ""} {
		t.Run("contract "+contract, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{"prepare", "-contract", contract,
				"-server-url", unreachable, "-migrations", "."}, &stdout, &stderr)
			assert.Equal(t, 2, code)
			assert.Contains(t, stderr.String(), "this build serves contract 1")
			assert.Empty(t, stdout.String())
		})
	}
	// Control: contract 1 passes that check and fails only on the
	// unreachable server.
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"prepare", "-contract", "1",
		"-server-url", unreachable, "-migrations", "."}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "connect to the Postgres server")
	assert.NotContains(t, stderr.String(), "pa55-word", "the password never reaches stderr")
}

func TestRun_RefusesToDropADatabasePrepareDidNotName(t *testing.T) {
	for _, name := range []string{"garam", "garam_test", "garam_principal_", "garam_principal_0123456789ABCDEF", "x; DROP"} {
		t.Run(name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := run(context.Background(), []string{"drop", "-contract", "1",
				"-server-url", unreachable, "-database", name}, &stdout, &stderr)
			assert.Equal(t, 2, code)
			assert.Contains(t, stderr.String(), "name prepare did not generate")
		})
	}
	// Control: a generated name passes the shape and fails only on the
	// unreachable server.
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"drop", "-contract", "1",
		"-server-url", unreachable, "-database", "garam_principal_0123456789abcdef"}, &stdout, &stderr)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "connect to the Postgres server")
}

func TestScrub_RemovesTheConnectionStringAndItsPassword(t *testing.T) {
	got := scrub("dial "+unreachable+" failed for pa55-word", unreachable)
	assert.NotContains(t, got, "pa55-word")
	assert.Contains(t, got, "[connection string]")
	assert.Contains(t, got, "[password]")
}

// TestScrub_LeavesANameThePasswordIsPartOf holds the compose default,
// password garam, apart from the database names prepare reports.
func TestScrub_LeavesANameThePasswordIsPartOf(t *testing.T) {
	dsn := "postgres://garam:garam@localhost:25432/postgres"
	got := scrub(`database garam_principal_0123456789abcdef was left behind; password "garam" refused`, dsn)
	assert.Contains(t, got, "garam_principal_0123456789abcdef")
	assert.Contains(t, got, `password "[password]" refused`)
}
