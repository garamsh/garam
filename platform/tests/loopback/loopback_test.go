package loopback_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/tests/loopback"
)

func TestParseConfig_AcceptsOnlyTargetsOnThisMachine(t *testing.T) {
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
			_, err := loopback.ParseConfig(tc.dsn)
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, loopback.ErrNotLoopback)
			}
		})
	}
}

// TestParseConfig_RefusesADialThatLeavesTheMachine holds the check on
// the address dialled, not the host written: localhost resolving to
// a remote address is refused, and a loopback dial is the control.
func TestParseConfig_RefusesADialThatLeavesTheMachine(t *testing.T) {
	cfg, err := loopback.ParseConfig("postgres://u:p@localhost:5432/db")
	require.NoError(t, err)
	dial := cfg.ConnConfig.DialFunc

	_, err = dial(context.Background(), "tcp", "10.0.0.5:5432")
	assert.ErrorIs(t, err, loopback.ErrNotLoopback)
	_, err = dial(context.Background(), "tcp", "127.0.0.1:1")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, loopback.ErrNotLoopback, "a loopback dial reaches the network and fails there")
}
