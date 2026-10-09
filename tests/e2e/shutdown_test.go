// Package e2e_test exercises what the build produces: it builds the
// garam binary and runs it as the processes a deployment runs.
// GARAM_TEST_DATABASE_URL names the database those processes open;
// `make db-up e2e` starts the docker-compose Postgres and sets it. The
// suite fails without it rather than skipping.
package e2e_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The paths every serve process mounts its probes on. Spelled here
// rather than imported: this layer reaches the binary as a deployment
// does, over HTTP, so a path it gets wrong is a defect this suite is
// meant to catch.
const (
	livePath  = "/healthz"
	readyPath = "/readyz"
)

// The message a serve process writes when its grace ran out with work
// still in flight. Spelled here for the reason the paths above are:
// this layer reads what the built binary emits.
const abandonedRecord = "shutdown abandoned work in flight"

var migrateOnce sync.Once

// TestMain requires the test database before any test runs. Every test
// here hands its URL to the processes under test, and skipping instead
// would leave `go test` printing the same `ok` for a run that started
// none as for one that passed.
func TestMain(m *testing.M) {
	if os.Getenv("GARAM_TEST_DATABASE_URL") == "" {
		fmt.Fprintln(os.Stderr, "e2e: GARAM_TEST_DATABASE_URL is not set; run `make db-up e2e`")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// moduleRoot returns the directory holding go.mod. It walks up from
// this source file, so the suite keeps working wherever the test
// directory is moved to.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "cannot locate the test source file")
	for dir := filepath.Dir(file); ; {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "no go.mod above %s", file)
		dir = parent
	}
}

// migrated brings the test database up to the schema a deployment's
// Postgres carries, and answers its URL. Readiness opens that database;
// nothing here reads its schema or its rows.
func migrated(t *testing.T) string {
	t.Helper()
	url := os.Getenv("GARAM_TEST_DATABASE_URL")
	migrateOnce.Do(func() {
		m, err := migrate.New("file://"+filepath.Join(moduleRoot(t), "migrations"), url)
		require.NoError(t, err)
		err = m.Up()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			require.NoError(t, err)
		}
	})
	return url
}

// loopbackCertificate is what `serve machine` terminates TLS with. What
// garam presents is not what this suite is about, so it is self-signed
// and nothing here verifies it.
func loopbackCertificate(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "garam machine listener"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// TestServe_ExitsZeroOnSIGTERM runs the built binary as each of the
// three serve processes, stops it the way a rollout does, and asserts
// the exit code a kubelet would record. The liveness answer is the
// control: the signal reaches a process that was serving, so a pass
// cannot come from one that never started.
//
// It is also the control for the record the test below asserts: these
// processes hold nothing when the signal lands, so a line saying they
// dropped something is one written for having stopped at all.
func TestServe_ExitsZeroOnSIGTERM(t *testing.T) {
	databaseURL := migrated(t)
	bin := buildGaram(t)

	tests := []struct {
		name  string
		setup func(t *testing.T) (env []string, probeBase string)
	}{
		{"api", func(t *testing.T) ([]string, string) {
			addr := freeAddr(t)
			return []string{
				"GARAM_DATABASE_URL=" + databaseURL,
				"GARAM_HTTP_ADDR=" + addr,
			}, "http://" + addr
		}},
		{"authz", func(t *testing.T) ([]string, string) {
			metricsAddr := freeAddr(t)
			return []string{
				"GARAM_DATABASE_URL=" + databaseURL,
				"GARAM_GRPC_ADDR=" + freeAddr(t),
				"GARAM_METRICS_ADDR=" + metricsAddr,
			}, "http://" + metricsAddr
		}},
		{"machine", func(t *testing.T) ([]string, string) {
			certFile, keyFile, kekFile, rootFile := machineKeyMaterialFiles(t)
			healthAddr := freeAddr(t)
			return []string{
				"GARAM_DATABASE_URL=" + databaseURL,
				"GARAM_MACHINE_ADDR=" + freeAddr(t),
				"GARAM_MACHINE_HEALTH_ADDR=" + healthAddr,
				"GARAM_MACHINE_TLS_CERT_FILE=" + certFile,
				"GARAM_MACHINE_TLS_KEY_FILE=" + keyFile,
				"GARAM_CA_KEY_FILE=" + kekFile,
				"GARAM_MACHINE_SERVER_ROOT_FILE=" + rootFile,
				"GARAM_MACHINE_ISSUER=https://garam.test",
			}, "http://" + healthAddr
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, probeBase := tc.setup(t)

			var logs bytes.Buffer
			cmd := exec.Command(bin, "serve", tc.name)
			cmd.Env = env
			cmd.Stdout = &logs
			cmd.Stderr = &logs
			require.NoError(t, cmd.Start())
			t.Cleanup(func() { _ = cmd.Process.Kill() })

			waitForProbe(t, probeBase+livePath, &logs)

			require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				assert.NoError(t, err, "exit on SIGTERM; logs:\n%s", logs.String())
				assert.NotContains(t, logs.String(), abandonedRecord,
					"nothing was in flight, so nothing was dropped")
			case <-time.After(20 * time.Second):
				t.Fatalf("still running 20s after SIGTERM; logs:\n%s", logs.String())
			}
		})
	}
}

// TestServeAPI_ExitsZeroWhenTheGraceExpiresWithWorkInFlight holds one
// request open across the SIGTERM, so the grace expires instead of the
// server stopping cleanly. The elapsed time is the control: a run that
// returns early proves the request was not in flight, and the exit code
// would then be the idle path's rather than this one's.
//
// The exit code is the same 0 either way, so the line is what says the
// request was dropped (#1064).
func TestServeAPI_ExitsZeroWhenTheGraceExpiresWithWorkInFlight(t *testing.T) {
	databaseURL := migrated(t)
	bin := buildGaram(t)
	addr := freeAddr(t)

	var logs bytes.Buffer
	cmd := exec.Command(bin, "serve", "api")
	cmd.Env = []string{
		"GARAM_DATABASE_URL=" + databaseURL,
		"GARAM_HTTP_ADDR=" + addr,
	}
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	waitForProbe(t, "http://"+addr+livePath, &logs)

	// A body shorter than its Content-Length on an unauthenticated
	// endpoint that decodes before it does anything else: the handler
	// blocks reading a request that never completes, so it is still in
	// flight when the signal lands.
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	_, err = conn.Write([]byte("POST /auth/password/register HTTP/1.1\r\n" +
		"Host: garam\r\n" +
		"Content-Type: application/json\r\n" +
		"Content-Length: 200\r\n\r\n" +
		`{"username":"a"`))
	require.NoError(t, err)
	// Long enough for the server to accept and dispatch it. Too short
	// and the elapsed assertion below fails rather than passing quietly.
	time.Sleep(500 * time.Millisecond)

	start := time.Now()
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		elapsed := time.Since(start)
		assert.NoError(t, err, "a requested stop is not a failure; logs:\n%s", logs.String())
		assert.Greater(t, elapsed, 9*time.Second,
			"the grace did not expire, so the exit code above is the idle path's; logs:\n%s", logs.String())
		assert.Contains(t, logs.String(), abandonedRecord,
			"exit 0 alone does not say the request was dropped; logs:\n%s", logs.String())
	case <-time.After(30 * time.Second):
		t.Fatalf("still running 30s after SIGTERM; logs:\n%s", logs.String())
	}
}

// TestServeAuthz_ReadyzAnswersReadyWithItsDatabase is what makes this
// layer need the Postgres `make db-up` starts. Liveness answers for the
// process alone (ADR-0021), so every assertion above holds against a
// database that is not there; readiness answers only once the
// dependency ping reaches one (#893).
//
// `serve authz` and not the other two: readiness on api and machine
// registers the authz channel beside the database, and this suite runs
// one process at a time, so neither can answer ready without a second
// process that has nothing to do with the store.
func TestServeAuthz_ReadyzAnswersReadyWithItsDatabase(t *testing.T) {
	databaseURL := migrated(t)
	bin := buildGaram(t)
	metricsAddr := freeAddr(t)

	var logs bytes.Buffer
	cmd := exec.Command(bin, "serve", "authz")
	cmd.Env = []string{
		"GARAM_DATABASE_URL=" + databaseURL,
		"GARAM_GRPC_ADDR=" + freeAddr(t),
		"GARAM_METRICS_ADDR=" + metricsAddr,
	}
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	// Liveness first, so a readiness answer that never arrives is
	// reported against a process that was already serving.
	waitForProbe(t, "http://"+metricsAddr+livePath, &logs)

	// One request, no polling: pgxpool connects on demand, so readiness
	// is not a condition that becomes true while the test waits.
	resp, err := http.Get("http://" + metricsAddr + readyPath)
	require.NoError(t, err, "readiness did not answer; logs:\n%s", logs.String())
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"readiness answered %d %q; logs:\n%s", resp.StatusCode, bytes.TrimSpace(body), logs.String())
}

// buildGaram builds the binary under test: this layer exercises what
// `go build` produces, not the packages compiled into the test.
func buildGaram(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "garam")
	build := exec.Command("go", "build", "-o", bin, "./cmd/garam")
	build.Dir = moduleRoot(t)
	out, err := build.CombinedOutput()
	require.NoError(t, err, "go build: %s", out)
	return bin
}

// freeAddr reserves a loopback address for the process to bind. The
// port is free again between Close here and Listen there, which is the
// best a test can do without handing the process an open socket.
func freeAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = lis.Close() }()
	return lis.Addr().String()
}

// waitForProbe polls until the process answers liveness, so the SIGTERM
// after it provably lands on a process that was serving.
func waitForProbe(t *testing.T, url string, logs *bytes.Buffer) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		require.False(t, time.Now().After(deadline), "probe %s never answered ok; logs:\n%s", url, logs.String())
		time.Sleep(100 * time.Millisecond)
	}
}

// machineKeyMaterialFiles writes the four files `serve machine` refuses
// to start without: the listener's certificate and key, the
// key-encryption key with the owner-only mode the sealer enforces, and
// the garam server root every identity pins garam by (ADR-0040).
func machineKeyMaterialFiles(t *testing.T) (certFile, keyFile, kekFile, rootFile string) {
	t.Helper()
	dir := t.TempDir()

	kek := make([]byte, 32)
	_, err := rand.Read(kek)
	require.NoError(t, err)
	kekFile = filepath.Join(dir, "ca-kek.hex")
	require.NoError(t, os.WriteFile(kekFile, []byte(hex.EncodeToString(kek)), 0o600))

	cert := loopbackCertificate(t)
	var certPEM []byte
	for _, der := range cert.Certificate {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	certFile = filepath.Join(dir, "tls.crt")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))

	key, ok := cert.PrivateKey.(*ecdsa.PrivateKey)
	require.True(t, ok, "loopbackCertificate returns an ECDSA key")
	der, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyFile = filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600))

	// The root every machine identity pins garam by (ADR-0040). What it
	// carries is not what a shutdown is about, so it is the certificate
	// above: the process reads it, hands it on, and never verifies with
	// it.
	//
	// Owner-only like the three above, though nothing reads this one's
	// mode: the umask subtracts from os.WriteFile's mode argument, so a
	// mode reaching past the owner is one the environment decides.
	rootFile = filepath.Join(dir, "server-root.pem")
	require.NoError(t, os.WriteFile(rootFile, certPEM, 0o600))
	return certFile, keyFile, kekFile, rootFile
}
