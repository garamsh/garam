package cli_test

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/ca"
	"github.com/garamsh/garam/platform/internal/cli"
	"github.com/garamsh/garam/platform/internal/keyfile"
	"github.com/garamsh/garam/platform/internal/operator"
)

// TestIdentityTokens_RefusesAMalformedTokenWithoutAReadForIt holds that
// a value no issuance could have produced costs the store nothing. The
// listener's database is unreachable, so a value it looked up would be
// answered unavailable: each malformed value is refused as
// unauthenticated, and the well-formed one beside them is looked up, so
// what spared the store is the format.
func TestIdentityTokens_RefusesAMalformedTokenWithoutAReadForIt(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), unreachableDatabase)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	sealer, err := ca.NewAESGCMSealer(make([]byte, ca.KeySize))
	require.NoError(t, err)
	certFile, keyFile := writeTLSKeyPair(t, t.TempDir())
	certificate, err := keyfile.NewPair(certFile, keyFile)
	require.NoError(t, err)
	srv, err := cli.NewMachineServer(pool, cli.MachineConfig{
		Sealer:      sealer,
		Issuer:      "https://machine.example",
		Certificate: certificate,
		Logger:      slog.New(slog.NewJSONHandler(io.Discard, nil)),
	})
	require.NoError(t, err)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	t.Cleanup(func() { _ = srv.Close() })
	// What garam presents is not under test, and this client presents
	// no certificate.
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}}}
	t.Cleanup(client.CloseIdleConnections)
	identity := func(t *testing.T, token string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, "https://"+ln.Addr().String()+"/identity", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Garam-Token "+token)
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		return resp.StatusCode
	}

	for name, token := range map[string]string{
		"another prefix": "garam_xx_" + strings.Repeat("a", 64),
		"too short":      operator.TokenPrefix + strings.Repeat("a", 63),
		"uppercase hex":  operator.TokenPrefix + strings.Repeat("A", 64),
		"not hex":        operator.TokenPrefix + strings.Repeat("g", 64),
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, http.StatusUnauthorized, identity(t, token))
		})
	}

	assert.Equal(t, http.StatusServiceUnavailable, identity(t, operator.TokenPrefix+strings.Repeat("a", 64)),
		"the well-formed token was looked up")
}
