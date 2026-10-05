// Package standalone_test runs a standalone host end to end (#1180's
// joint matrix, #1157 scenarios 2 and 5): the shipped `garam` binary
// prepares, enrolls and runs the host's supervisor, against a real
// garam built from this tree — its authz, api and machine listener as
// containers over a database tests/testprincipal creates — on a real
// rootless Docker daemon with the pinned Sherlock images.
//
// `make standalone-e2e` runs it. It needs:
//   - GARAM_TEST_DOCKER_HOST: the rootless daemon, unix://…
//   - GARAM_TEST_SERVER_URL: the Postgres server `make db-up` publishes
//   - GARAM_TEST_POSTGRES_CONTAINER: that server's container, whose
//     network the garam containers join
//   - GARAM_TEST_GARAM_IMAGE: this tree's garam image, by ID
//   - GARAM_TEST_RUNTIME_IMAGE, GARAM_TEST_WORKSPACE_IMAGE: Sherlock's
//   - GARAM_TEST_TEI_IMAGE: the embedding service a real model needs
//   - GARAM_TEST_MODEL_ENV_FILE: an owner-only KEY=VALUE file holding
//     the model's credential, for the scenarios a model answers
//
// It fails rather than skips without them. Every container, volume and
// network it makes is named garam-1180-…, and it removes only those.
package standalone_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/workload"
	"github.com/garamsh/garam/internal/workload/docker"
)

const testPrefix = "garam-1180-"

func required(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("%s is required: run this suite through make standalone-e2e", name)
	}
	return v
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	require.NoError(t, err)
	return filepath.Dir(strings.TrimSpace(string(out)))
}

// binaries are the shipped garam CLI and the test-only principal
// fixture, each built once from this tree.
var binaries = sync.OnceValues(func() (map[string]string, error) {
	dir, err := os.MkdirTemp("", "garam-1180-bin")
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for name, pkg := range map[string]string{"garam": "./cmd/garam", "testprincipal": "./tests/testprincipal"} {
		path := filepath.Join(dir, name)
		cmd := exec.Command("go", "build", "-o", path, pkg)
		cmd.Dir = "../.."
		if b, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("build %s: %v: %s", pkg, err, b)
		}
		out[name] = path
	}
	return out, nil
})

// stack is one garam: its database, its three processes as containers,
// and the organization the test's principal owns.
type stack struct {
	t       *testing.T
	daemon  string
	engine  *docker.Engine
	prefix  string
	dir     string
	garam   string
	network string

	databaseURL string
	session     *http.Cookie
	apiURL      string
	// machineURL is where the host reaches the machine listener, and
	// bridgeURL where a container on the default bridge does.
	machineURL string
	bridgeURL  string
	rootFile   string
	org        string
	rootKey    *ecdsa.PrivateKey
	root       *x509.Certificate
}

func newStack(t *testing.T) *stack {
	t.Helper()
	daemon := required(t, "GARAM_TEST_DOCKER_HOST")
	engine, err := docker.New(daemon)
	require.NoError(t, err)
	t.Cleanup(func() { _ = engine.Close() })
	dir, err := os.MkdirTemp("", "g1180")
	require.NoError(t, err)
	s := &stack{
		t: t, daemon: daemon, engine: engine, dir: dir,
		prefix: testPrefix + hex.EncodeToString(randomBytes(3)) + "-",
		garam:  required(t, "GARAM_TEST_GARAM_IMAGE"),
	}
	t.Cleanup(s.cleanup)
	s.network = strings.Fields(s.mustDocker("inspect", "-f",
		"{{range $k, $v := .NetworkSettings.Networks}}{{$k}} {{end}}", required(t, "GARAM_TEST_POSTGRES_CONTAINER")))[0]
	s.prepareDatabase()
	s.serve()
	s.org = s.createOrg("acme")
	return s
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// cleanup removes what this stack made, and only that.
func (s *stack) cleanup() {
	if ids, err := s.docker("ps", "-aq", "--filter", "name=^"+s.prefix); err == nil {
		for _, id := range strings.Fields(ids) {
			_, _ = s.docker("rm", "-f", id)
		}
	}
	if names, err := s.docker("volume", "ls", "-q", "--filter", "name=^"+s.prefix); err == nil {
		for _, name := range strings.Fields(names) {
			_, _ = s.docker("volume", "rm", name)
		}
	}
	_ = os.RemoveAll(s.dir)
}

func (s *stack) docker(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	cmd.Env = append(os.Environ(), "DOCKER_HOST="+s.daemon)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (s *stack) mustDocker(args ...string) string {
	s.t.Helper()
	out, err := s.docker(args...)
	require.NoError(s.t, err, "docker %v: %s", args, out)
	return out
}

// prepareDatabase creates the stack's database and its signed-in
// principal with tests/testprincipal, which migrates it, and drops it
// once everything using it has stopped.
func (s *stack) prepareDatabase() {
	s.t.Helper()
	bins, err := binaries()
	require.NoError(s.t, err)
	server := required(s.t, "GARAM_TEST_SERVER_URL")
	out, err := exec.Command(bins["testprincipal"], "prepare", "-contract", "1",
		"-migrations", filepath.Join(moduleRoot(s.t), "migrations"), "-server-url", server).Output()
	require.NoError(s.t, err)
	var p struct {
		Database    string `json:"database"`
		DatabaseURL string `json:"databaseUrl"`
		Session     struct {
			Cookie string `json:"cookie"`
			Token  string `json:"token"`
		} `json:"session"`
	}
	require.NoError(s.t, json.Unmarshal(out, &p))
	// Registered first, so it runs after the containers using it are
	// gone.
	s.t.Cleanup(func() {
		_ = exec.Command(bins["testprincipal"], "drop", "-contract", "1", "-server-url", server,
			"-database", p.Database).Run()
	})
	s.databaseURL = p.DatabaseURL
	s.session = &http.Cookie{Name: p.Session.Cookie, Value: p.Session.Token}
}

// inside is the database's URL as a container on the stack's network
// reaches it.
func (s *stack) inside() string {
	u, err := url.Parse(s.databaseURL)
	require.NoError(s.t, err)
	u.Host = "postgres:5432"
	return u.String()
}

// serve starts authz, api and the machine listener from the garam
// image, and waits until each answers.
func (s *stack) serve() {
	s.t.Helper()
	authz := s.prefix + "authz"
	s.mustDocker("run", "-d", "--name", authz, "--network", s.network,
		"-e", "GARAM_DATABASE_URL="+s.inside(), s.garam, "serve", "authz")
	api := s.prefix + "api"
	s.mustDocker("run", "-d", "--name", api, "--network", s.network, "-p", "127.0.0.1::8080",
		"-e", "GARAM_DATABASE_URL="+s.inside(), "-e", "GARAM_AUTHZ_ADDR="+authz+":9090",
		"-e", "GARAM_COOKIE_SECURE=false", s.garam, "serve", "api")
	s.apiURL = "http://" + s.mustDocker("port", api, "8080")
	s.startMachine(authz)
	s.eventually("the api answering", func() bool {
		status, _ := s.browse(http.MethodGet, "/me", "")
		return status == http.StatusOK
	})
}

// startMachine starts the machine listener with key material this
// stack mints: a garam server root, and a serving certificate for the
// two addresses the listener is reached at, the host's loopback and its
// bridge address, which is known only once it runs and is then written
// in place, since the listener reads its certificate per handshake.
func (s *stack) startMachine(authz string) {
	s.t.Helper()
	rootKey, root, rootPEM := mintRoot(s.t)
	s.rootKey, s.root = rootKey, root
	s.rootFile = filepath.Join(s.dir, "server-root.pem")
	require.NoError(s.t, os.WriteFile(s.rootFile, rootPEM, 0o600))
	kek := hex.EncodeToString(randomBytes(32))

	name := s.prefix + "machine"
	s.mustDocker("create", "--name", name, "--network", s.network, "-p", "127.0.0.1::8443",
		"-e", "GARAM_DATABASE_URL="+s.inside(), "-e", "GARAM_AUTHZ_ADDR="+authz+":9090",
		"-e", "GARAM_MACHINE_TLS_CERT_FILE=/etc/garam/tls.crt", "-e", "GARAM_MACHINE_TLS_KEY_FILE=/etc/garam/tls.key",
		"-e", "GARAM_MACHINE_SERVER_ROOT_FILE=/etc/garam/server-root.pem", "-e", "GARAM_CA_KEY_FILE=/etc/garam/kek",
		s.garam, "serve", "machine")
	s.mustDocker("network", "connect", "bridge", name)
	place := func(ips ...net.IP) {
		certPEM, keyPEM := mintServing(s.t, rootKey, root, ips)
		own := func(path string, data []byte) workload.File {
			return workload.File{Path: path, Data: data, Mode: 0o600, UID: 10001, GID: 10001}
		}
		require.NoError(s.t, s.engine.CopyFiles(context.Background(), name, "/etc", []workload.File{
			{Path: "garam", Mode: 0o700, Dir: true, UID: 10001, GID: 10001},
			own("garam/tls.crt", certPEM), own("garam/tls.key", keyPEM),
			own("garam/server-root.pem", rootPEM), own("garam/kek", []byte(kek)),
		}))
	}
	place(net.IPv4(127, 0, 0, 1))
	s.mustDocker("start", name)
	bridge := net.ParseIP(s.mustDocker("inspect", "-f", "{{.NetworkSettings.Networks.bridge.IPAddress}}", name))
	require.NotNil(s.t, bridge)
	place(net.IPv4(127, 0, 0, 1), bridge)
	s.machineURL = "https://" + s.mustDocker("port", name, "8443")
	s.bridgeURL = "https://" + bridge.String() + ":8443"
	s.eventually("the machine listener presenting the bridge certificate", func() bool {
		conn, err := tls.Dial("tcp", strings.TrimPrefix(s.machineURL, "https://"), &tls.Config{
			RootCAs: pool(rootPEM), ServerName: bridge.String(), MinVersion: tls.VersionTLS13,
		})
		if err == nil {
			_ = conn.Close()
		}
		return err == nil
	})
}

func pool(pemBytes []byte) *x509.CertPool {
	p := x509.NewCertPool()
	p.AppendCertsFromPEM(pemBytes)
	return p
}

func mintRoot(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "garam-1180 server root"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	root, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return key, root, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func mintServing(t *testing.T, rootKey *ecdsa.PrivateKey, root *x509.Certificate, ips []net.IP) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), IPAddresses: ips,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, root, &key.PublicKey, rootKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

// eventually waits for cond, for as long as a container's start or a
// model's turn takes on a loaded host.
func (s *stack) eventually(what string, cond func() bool) {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Minute)
	for !cond() {
		if time.Now().After(deadline) {
			s.t.Fatalf("%s did not happen", what)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// browse makes one browser request as the stack's principal.
func (s *stack) browse(method, path, body string) (int, string) {
	s.t.Helper()
	req, err := http.NewRequest(method, s.apiURL+path, strings.NewReader(body))
	require.NoError(s.t, err)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(s.session)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func (s *stack) createOrg(name string) string {
	s.t.Helper()
	status, body := s.browse(http.MethodPost, "/orgs", fmt.Sprintf(`{"name":%q}`, name))
	require.Equal(s.t, http.StatusCreated, status, body)
	var o struct {
		GRN string `json:"grn"`
	}
	require.NoError(s.t, json.Unmarshal([]byte(body), &o))
	parts := strings.Split(o.GRN, ":")
	return parts[len(parts)-1]
}

// cli runs the shipped garam binary and answers its combined output.
func (s *stack) cli(args ...string) (string, error) {
	s.t.Helper()
	bins, err := binaries()
	require.NoError(s.t, err)
	var out bytes.Buffer
	cmd := exec.Command(bins["garam"], args...)
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	return out.String(), err
}

// serving is a certificate the stack's server root signed for ips, as
// a fixture standing where garam's listener stands presents it.
func (s *stack) serving(ips ...net.IP) ([]byte, []byte) {
	s.t.Helper()
	return mintServing(s.t, s.rootKey, s.root, ips)
}
