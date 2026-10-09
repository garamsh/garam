// Package workload_test runs the standalone supervisor against a real
// Docker daemon and the pinned images (ADR-0085). The supervisor runs
// in process with the shipped Docker engine; garam's activateAgent is
// stood in for, which is the one part not exercised here.
//
// It needs GARAM_TEST_DOCKER_HOST (a rootless daemon's unix:// socket)
// and the three images by digest or ID: GARAM_TEST_RUNTIME_IMAGE,
// GARAM_TEST_WORKSPACE_IMAGE and GARAM_TEST_ADAPTER_IMAGE. `make
// workload-integration` runs it; it fails rather than skips without
// them. Every container and volume it makes is named under testPrefix,
// and it removes only those.
package workload_test

import (
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
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/workload"
	"github.com/garamsh/garam/internal/workload/docker"
)

// testPrefix begins every name this suite makes, so cleanup removes
// what it made and nothing another user of the daemon did.
const testPrefix = "garam-sa-test-"

var testAgent = func() grn.GRN {
	g, err := grn.Parse("grn:acme:default:agent:0a1b2c3d4e5f6071")
	if err != nil {
		panic(err)
	}
	return g
}()

// agentConfig is the runtime's configuration: the offline mock model,
// so a turn needs nothing outside the host.
const agentConfig = "revision: r1\nmodel:\n  provider: mock\n"

func required(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("%s is required: run this suite through make workload-integration", name)
	}
	return v
}

// probeBinary is the static probe, built once.
var probeBinary = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "garam-1180-probe")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "probe")
	cmd := exec.Command("go", "build", "-o", path, "./probe")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build probe: %v: %s", err, out)
	}
	return path, nil
})

// stubGaram stands in for activateAgent: one activation per request
// identifier, a newer token for each replay.
type stubGaram struct {
	mu       sync.Mutex
	requests []workload.ActivationRequest
	versions map[string]int
	asked    chan struct{}
	// undecided answers unavailable, as a garam out of reach does.
	undecided bool
	// before runs ahead of each answer, outside the lock.
	before func()
}

func (g *stubGaram) setUndecided(v bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.undecided = v
}

func (g *stubGaram) Activate(_ context.Context, req workload.ActivationRequest) (*workload.Activation, error) {
	g.mu.Lock()
	before := g.before
	g.mu.Unlock()
	if before != nil {
		before()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.undecided {
		return nil, errorx.New(errorx.KindUnavailable, "garam out of reach")
	}
	g.requests = append(g.requests, req)
	g.versions[req.RequestID]++
	v := g.versions[req.RequestID]
	select {
	case g.asked <- struct{}{}:
	default:
	}
	return &workload.Activation{
		ID: "activation-" + req.RequestID, Token: fmt.Sprintf("token-%d", v), TokenVersion: v,
		GRN: testAgent.String(), Epoch: req.Epoch, Generation: req.Generation, Replayed: v > 1,
	}, nil
}

// generations answers each generation garam was asked for, once.
func (g *stubGaram) generations() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, r := range g.requests {
		if !slices.Contains(out, r.Generation) {
			out = append(out, r.Generation)
		}
	}
	return out
}

// host is one standalone host on the real daemon.
type host struct {
	t      *testing.T
	engine *docker.Engine
	garam  *stubGaram
	dir    string
	prefix string
	images workload.Images
	leaf   []byte
	key    []byte
	issuer []byte
	signer *signer
	daemon string
	// machineURL is where the adapter reaches garam: nothing, unless
	// the test stands garam in with garamOn.
	machineURL string
}

// signer is the agent's issuer, which here also signs the stand-in
// garam's serving certificate the adapter verifies garam by.
type signer struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newHost(t *testing.T) *host {
	t.Helper()
	daemon := required(t, "GARAM_TEST_DOCKER_HOST")
	engine, err := docker.New(daemon)
	require.NoError(t, err)
	t.Cleanup(func() { _ = engine.Close() })
	// The socket's path must fit a unix address.
	dir, err := os.MkdirTemp("", "g1180")
	require.NoError(t, err)
	h := &host{
		t: t, engine: engine, dir: dir, daemon: daemon,
		garam:      &stubGaram{versions: map[string]int{}, asked: make(chan struct{}, 1)},
		prefix:     testPrefix + hex.EncodeToString(randomBytes(3)) + "-",
		machineURL: "https://127.0.0.1:1",
		images: workload.Images{
			Runtime:   required(t, "GARAM_TEST_RUNTIME_IMAGE"),
			Workspace: required(t, "GARAM_TEST_WORKSPACE_IMAGE"),
			Adapter:   required(t, "GARAM_TEST_ADAPTER_IMAGE"),
		},
	}
	h.issuer, h.leaf, h.key, h.signer = agentCredential(t)
	t.Cleanup(h.cleanup)
	return h
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// cleanup removes what this host made, and only that: containers and
// volumes under its own prefix. It forces nothing it did not make.
func (h *host) cleanup() {
	if ids, err := h.docker("ps", "-aq", "--filter", "name=^"+h.prefix); err == nil {
		for _, id := range strings.Fields(ids) {
			_, _ = h.docker("rm", "-f", id)
		}
	}
	if names, err := h.docker("volume", "ls", "-q", "--filter", "name=^"+h.prefix); err == nil {
		for _, name := range strings.Fields(names) {
			_, _ = h.docker("volume", "rm", name)
		}
	}
	_ = os.RemoveAll(h.dir)
}

// docker runs the docker CLI against the test's daemon.
func (h *host) docker(args ...string) (string, error) {
	cmd := exec.Command("docker", args...)
	cmd.Env = append(os.Environ(), "DOCKER_HOST="+h.daemon)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (h *host) mustDocker(args ...string) string {
	h.t.Helper()
	out, err := h.docker(args...)
	require.NoError(h.t, err, "docker %v: %s", args, out)
	return out
}

func (h *host) config(edit ...func(*workload.Desired)) workload.Config {
	d := workload.Desired{
		Agent: testAgent, Epoch: "3", Config: []byte(agentConfig), Revision: "r1",
		ModelEnv: []string{"MODEL_KEY=unused-by-the-mock"}, Images: h.images,
	}
	for _, e := range edit {
		e(&d)
	}
	return workload.Config{
		Dir: h.dir, Prefix: h.prefix, Desired: d,
		Adapter: workload.AdapterFiles{
			CertificatePEM: h.leaf, KeyPEM: h.key, IssuerPEM: h.issuer, ServerRootPEM: h.issuer,
			// Nothing answers there unless garamOn stood garam in: the
			// adapter's claims then fail and are retried, as they are
			// while garam is unreachable.
			MachineURL: h.machineURL,
		},
		MessageIssuer: "https://garam.test",
	}
}

func replace(d *workload.Desired) { d.Replace = true }

// running is one Run in the background.
type running struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func (h *host) start(edit ...func(*workload.Desired)) *running {
	return h.startOn(h.engine, edit...)
}

func (h *host) startOn(engine workload.Engine, edit ...func(*workload.Desired)) *running {
	h.t.Helper()
	s, err := workload.NewSupervisor(engine, h.garam, h.config(edit...))
	require.NoError(h.t, err)
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(r.done)
		r.err = s.Run(ctx)
	}()
	h.t.Cleanup(func() { r.cancel(); <-r.done })
	return r
}

func (r *running) stop() error {
	r.cancel()
	<-r.done
	return r.err
}

// result waits for Run to return on its own, for as long as a
// generation's start and drain take on a loaded host.
func (r *running) result(t *testing.T) error {
	t.Helper()
	select {
	case <-r.done:
		return r.err
	case <-time.After(3 * time.Minute):
		t.Fatal("the supervisor did not return")
		return nil
	}
}

// returned reports whether Run has returned.
func (r *running) returned() bool {
	select {
	case <-r.done:
		return true
	default:
		return false
	}
}

// activated waits until garam has been asked for n generations, and
// answers the last.
func (h *host) activated(n int) string {
	h.t.Helper()
	deadline := time.After(3 * time.Minute)
	for {
		if got := h.garam.generations(); len(got) >= n {
			return got[n-1]
		}
		select {
		case <-h.garam.asked:
		case <-deadline:
			h.t.Fatalf("garam was not asked for generation %d; asked for %v", n, h.garam.generations())
		}
	}
}

// container answers the ID of the one container of role this host
// holds, or "" where it holds none.
func (h *host) container(role workload.Role) string {
	h.t.Helper()
	ids := strings.Fields(h.mustDocker("ps", "-aq", "--filter", "name=^"+h.prefix+string(role)+"-"))
	require.LessOrEqual(h.t, len(ids), 1, "one %s at a time", role)
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// inspect answers one field of a container's inspection.
func (h *host) inspect(id, format string) string {
	h.t.Helper()
	return h.mustDocker("inspect", "-f", format, id)
}

// volume answers the name of this host's volume of kind.
func (h *host) volume(kind string) string {
	h.t.Helper()
	names := strings.Fields(h.mustDocker("volume", "ls", "-q", "--filter", "name=^"+h.prefix+kind+"-"))
	require.Len(h.t, names, 1)
	return names[0]
}

// probe copies the probe into a container and answers its path there.
func (h *host) probe(id string) string {
	h.t.Helper()
	path, err := probeBinary()
	require.NoError(h.t, err)
	h.mustDocker("cp", path, id+":/probe")
	return "/probe"
}

// report runs the probe's report in a container as its own user.
func (h *host) report(id string, args ...string) probeReport {
	h.t.Helper()
	out := h.mustDocker(append([]string{"exec", id, h.probe(id), "report"}, args...)...)
	var r probeReport
	require.NoError(h.t, json.Unmarshal([]byte(out), &r), out)
	return r
}

type probeReport struct {
	Status map[string]string `json:"status"`
	Env    []string          `json:"env"`
	Mounts []string          `json:"mounts"`
	Open   map[string]string `json:"open"`
	Write  map[string]string `json:"write"`
}

// agentCredential is an issuer and a leaf it signed for the test agent,
// as garam signs one.
func agentCredential(t *testing.T) (issuerPEM, leafPEM, keyPEM []byte, issuing *signer) {
	t.Helper()
	issuerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	issuer := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "acme authority"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true,
	}
	issuerDER, err := x509.CreateCertificate(rand.Reader, issuer, issuer, &issuerKey.PublicKey, issuerKey)
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	uri, err := url.Parse(testAgent.String())
	require.NoError(t, err)
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), URIs: []*url.URL{uri},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, issuer, &key.PublicKey, issuerKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	leafPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	_, err = tls.X509KeyPair(leafPEM, keyPEM)
	require.NoError(t, err)
	parsed, err := x509.ParseCertificate(issuerDER)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issuerDER}), leafPEM, keyPEM,
		&signer{cert: parsed, key: issuerKey}
}

// garamOn stands garam's machine listener in, in a container of its
// own on the network the runtime's namespace is on, and points the
// adapter at it. It answers what the probe's garam mode answers and
// prints each outbox key it stored.
func (h *host) garamOn() string {
	h.t.Helper()
	name := h.prefix + "garam"
	h.mustDocker("create", "--name", name, "--entrypoint", "/probe", h.images.Workspace,
		"garam", ":8443", "/tmp/garam.crt", "/tmp/garam.key")
	path, err := probeBinary()
	require.NoError(h.t, err)
	h.mustDocker("cp", path, name+":/probe")
	h.mustDocker("start", name)
	ip := net.ParseIP(h.inspect(name, "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}"))
	require.NotNil(h.t, ip, "the stand-in has an address")

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(h.t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(3), IPAddresses: []net.IP{ip},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, h.signer.cert, &key.PublicKey, h.signer.key)
	require.NoError(h.t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(h.t, err)
	for _, f := range []struct {
		name string
		data []byte
	}{
		// The key first: the stand-in serves once the certificate is
		// there.
		{"garam.key", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})},
		{"garam.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})},
	} {
		local := filepath.Join(h.dir, f.name)
		require.NoError(h.t, os.WriteFile(local, f.data, 0o600))
		h.mustDocker("cp", local, name+":/tmp/"+f.name)
	}
	h.machineURL = "https://" + ip.String() + ":8443"
	return name
}
