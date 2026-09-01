package e2e_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestServe_ExitsZeroOnSIGTERM runs the built binary as each of the
// three serve processes, stops it the way a rollout does, and asserts
// the exit code a kubelet would record. The healthz answer is the
// control: the signal reaches a process that was serving, so a pass
// cannot come from one that never started.
func TestServe_ExitsZeroOnSIGTERM(t *testing.T) {
	// The processes open the migrated test database; nothing they do
	// on the shutdown path touches its rows.
	_ = pool(t)
	bin := buildGaram(t)
	databaseURL := os.Getenv("GARAM_TEST_DATABASE_URL")

	tests := []struct {
		name  string
		setup func(t *testing.T) (env []string, probeURL string)
	}{
		{"api", func(t *testing.T) ([]string, string) {
			addr := freeAddr(t)
			return []string{
				"GARAM_DATABASE_URL=" + databaseURL,
				"GARAM_HTTP_ADDR=" + addr,
			}, "http://" + addr + "/healthz"
		}},
		{"authz", func(t *testing.T) ([]string, string) {
			metricsAddr := freeAddr(t)
			return []string{
				"GARAM_DATABASE_URL=" + databaseURL,
				"GARAM_GRPC_ADDR=" + freeAddr(t),
				"GARAM_METRICS_ADDR=" + metricsAddr,
			}, "http://" + metricsAddr + "/healthz"
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
			}, "http://" + healthAddr + "/healthz"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, probeURL := tc.setup(t)

			var logs bytes.Buffer
			cmd := exec.Command(bin, "serve", tc.name)
			cmd.Env = env
			cmd.Stdout = &logs
			cmd.Stderr = &logs
			require.NoError(t, cmd.Start())
			t.Cleanup(func() { _ = cmd.Process.Kill() })

			waitForProbe(t, probeURL, &logs)

			require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case err := <-done:
				assert.NoError(t, err, "exit on SIGTERM; logs:\n%s", logs.String())
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
func TestServeAPI_ExitsZeroWhenTheGraceExpiresWithWorkInFlight(t *testing.T) {
	_ = pool(t)
	bin := buildGaram(t)
	addr := freeAddr(t)

	var logs bytes.Buffer
	cmd := exec.Command(bin, "serve", "api")
	cmd.Env = []string{
		"GARAM_DATABASE_URL=" + os.Getenv("GARAM_TEST_DATABASE_URL"),
		"GARAM_HTTP_ADDR=" + addr,
	}
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	waitForProbe(t, "http://"+addr+"/healthz", &logs)

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
	case <-time.After(30 * time.Second):
		t.Fatalf("still running 30s after SIGTERM; logs:\n%s", logs.String())
	}
}

// buildGaram builds the binary under test: this layer exercises what
// `go build` produces, not the packages compiled into the test.
func buildGaram(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "garam")
	build := exec.Command("go", "build", "-o", bin, "./cmd/garam")
	build.Dir = filepath.Dir(migrationsDir(t))
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
