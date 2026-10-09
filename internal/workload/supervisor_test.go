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
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/workload"
)

const (
	runtimeImage   = "sherlock-agent@sha256:" + "1111111111111111111111111111111111111111111111111111111111111111"
	workspaceImage = "sherlock-workspace@sha256:" + "2222222222222222222222222222222222222222222222222222222222222222"
	adapterImage   = "sha256:" + "33333333333333333333333333333333333333333333333333333333333333cc"
)

// stubGaram answers activateAgent as garam does: one activation per
// request identifier, a new token of a higher version for every replay.
// refuse, where set, answers instead.
type stubGaram struct {
	mu       sync.Mutex
	requests []workload.ActivationRequest
	versions map[string]int
	refuse   error
}

func (g *stubGaram) Activate(_ context.Context, req workload.ActivationRequest) (*workload.Activation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.requests = append(g.requests, req)
	if g.refuse != nil {
		return nil, g.refuse
	}
	if g.versions == nil {
		g.versions = map[string]int{}
	}
	g.versions[req.RequestID]++
	v := g.versions[req.RequestID]
	return &workload.Activation{
		ID: "activation-" + req.RequestID, Token: fmt.Sprintf("token-%d", v), TokenVersion: v,
		GRN: testAgent.String(), Epoch: req.Epoch, Generation: req.Generation, Replayed: v > 1,
	}, nil
}

func (g *stubGaram) asked() []workload.ActivationRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.requests)
}

var testAgent = mustGRN("grn:acme:default:agent:0a1b2c3d4e5f6071")

func mustGRN(s string) grn.GRN {
	g, err := grn.Parse(s)
	if err != nil {
		panic(err)
	}
	return g
}

// identity is an issuer and a leaf it signed for one GRN, as garam
// signs an agent's.
type identity struct {
	issuerPEM []byte
	leaf      tls.Certificate
	leafPEM   []byte
	keyPEM    []byte

	issuer    *x509.Certificate
	issuerKey *ecdsa.PrivateKey
}

func newIdentity(t *testing.T, subject string) identity {
	t.Helper()
	issuerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "acme authority"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true,
	}
	issuerDER, err := x509.CreateCertificate(rand.Reader, template, template, &issuerKey.PublicKey, issuerKey)
	require.NoError(t, err)
	issuer, err := x509.ParseCertificate(issuerDER)
	require.NoError(t, err)
	id := identity{issuerPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: issuerDER}),
		issuer: issuer, issuerKey: issuerKey}
	return id.sign(t, subject)
}

// sign answers the identity with a new leaf its issuer signed for
// subject.
func (id identity) sign(t *testing.T, subject string) identity {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	uri, err := url.Parse(subject)
	require.NoError(t, err)
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2), URIs: []*url.URL{uri},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, id.issuer, &key.PublicKey, id.issuerKey)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	id.leafPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	id.keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	id.leaf, err = tls.X509KeyPair(id.leafPEM, id.keyPEM)
	require.NoError(t, err)
	return id
}

// host is one standalone host: its supervisor's directory, the engine,
// garam, and the agent's credential.
type host struct {
	t      *testing.T
	engine *stubEngine
	garam  *stubGaram
	dir    string
	id     identity
	// machineURL is where the adapter, and the runtime beside it,
	// reach garam.
	machineURL string
	// issuer is garam's message issuer the host names; empty is none.
	issuer string
}

// testIssuer is garam's message-signing issuer in these tests.
const testIssuer = "https://garam.example"

func newHost(t *testing.T) *host {
	t.Helper()
	// The socket's path must fit a unix address, which a test's own
	// temporary directory may not.
	dir, err := os.MkdirTemp("", "wl")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return &host{t: t, engine: newStubEngine(), garam: &stubGaram{}, dir: dir, id: newIdentity(t, testAgent.String()),
		machineURL: "https://garam.example", issuer: testIssuer}
}

func (h *host) config(edit ...func(*workload.Desired)) workload.Config {
	d := workload.Desired{
		Agent: testAgent, Epoch: "3", Config: []byte("revision: r1\n"), Revision: "r1",
		ModelEnv: []string{"MODEL_KEY=k"},
		Images:   workload.Images{Runtime: runtimeImage, Workspace: workspaceImage, Adapter: adapterImage},
	}
	for _, e := range edit {
		e(&d)
	}
	return workload.Config{
		Dir: h.dir, Prefix: "garam-test-", Desired: d,
		Adapter: workload.AdapterFiles{
			CertificatePEM: h.id.leafPEM, KeyPEM: h.id.keyPEM, IssuerPEM: h.id.issuerPEM,
			ServerRootPEM: h.id.issuerPEM, MachineURL: h.machineURL,
		},
		MessageIssuer: h.issuer,
	}
}

// running is one Run of a supervisor in the background. done closes
// once err holds what Run returned.
type running struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    error
}

func (h *host) start(edit ...func(*workload.Desired)) *running {
	h.t.Helper()
	s, err := workload.NewSupervisor(h.engine, h.garam, h.config(edit...))
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

// stop ends the supervisor, leaving the workload, and answers what Run
// returned.
func (r *running) stop() error {
	r.cancel()
	<-r.done
	return r.err
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

// result waits for Run to return on its own.
func (r *running) result(t *testing.T) error {
	t.Helper()
	select {
	case <-r.done:
		return r.err
	case <-time.After(10 * time.Second):
		t.Fatal("the supervisor did not return")
		return nil
	}
}

// serving waits until a generation's three containers run, and answers
// them.
func (h *host) serving() (runtime, workspace, adapter *stubContainer) {
	h.t.Helper()
	require.NoError(h.t, h.engine.await(func() bool {
		runtime, workspace, adapter = h.live(workload.RoleRuntime), h.live(workload.RoleWorkspace), h.live(workload.RoleAdapter)
		return runtime != nil && workspace != nil && adapter != nil
	}))
	return runtime, workspace, adapter
}

// live is the running container of role. The caller holds the engine's
// lock.
func (h *host) live(role workload.Role) *stubContainer {
	for _, c := range h.engine.named(role) {
		if c.status == workload.ContainerRunning {
			return c
		}
	}
	return nil
}

// adapterOf is the adapter's own control client, built from what its
// container was given: the socket it mounts, the root it verifies the
// supervisor by, and its placement token.
func (h *host) adapterOf(c *stubContainer) (*delivery.HTTPAuthority, string) {
	h.t.Helper()
	auth, err := delivery.NewHTTPAuthority(h.clientOf(c, h.id.leaf), "https://"+workload.ControlName, testAgent)
	require.NoError(h.t, err)
	return auth, string(c.files["/etc/garam/placement-token"].Data)
}

func (h *host) clientOf(c *stubContainer, presented tls.Certificate) *http.Client {
	h.t.Helper()
	roots := x509.NewCertPool()
	require.True(h.t, roots.AppendCertsFromPEM(c.files["/etc/garam/control-root.pem"].Data))
	require.Len(h.t, c.spec.Binds, 1)
	socket := filepath.Join(c.spec.Binds[0].Source, "control.sock")
	return &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{presented}, MinVersion: tls.VersionTLS13},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}}
}

func newGeneration() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func binding(generation string) delivery.Binding {
	return delivery.Binding{Agent: testAgent, Epoch: "3", Generation: generation, Revision: "r1"}
}

// TestRun_BringsAGenerationUpInTheArrangementsShape holds what each
// container is given (ADR-0085): the memory volume to the runtime and
// the adapter alone, the workspace volume to the workspace alone, the
// runtime's namespace joined by the other two, the model credential to
// the runtime alone, and the socket and garam's material to the
// adapter alone.
func TestRun_BringsAGenerationUpInTheArrangementsShape(t *testing.T) {
	h := newHost(t)
	h.start()
	runtime, workspace, adapter := h.serving()

	memory, space := runtime.spec.Volumes[0].Volume, workspace.spec.Volumes[0].Volume
	assert.NotEqual(t, memory, space)
	assert.Equal(t, []workload.VolumeMount{{Volume: memory, Target: "/data"}}, runtime.spec.Volumes)
	assert.Equal(t, []workload.VolumeMount{{Volume: space, Target: "/var/lib/sherlock-workspace"}}, workspace.spec.Volumes)
	assert.Equal(t, []workload.VolumeMount{{Volume: memory, Target: "/outbox", Subpath: "outbox"}}, adapter.spec.Volumes,
		"the adapter mounts the memory volume's outbox alone, and not the workspace")
	assert.Empty(t, runtime.spec.Binds, "the runtime mounts no host directory")
	assert.Empty(t, workspace.spec.Binds, "the workspace mounts no host directory")
	assert.Len(t, adapter.spec.Binds, 1, "the adapter mounts its socket's directory and nothing else of the host")

	assert.Empty(t, runtime.spec.NetworkOf, "the runtime owns its network namespace")
	assert.Equal(t, runtime.id, workspace.spec.NetworkOf)
	assert.Equal(t, runtime.id, adapter.spec.NetworkOf)

	assert.Equal(t, []string{"MODEL_KEY=k"}, runtime.spec.Env, "the runtime's environment is the model credential alone")
	for _, kv := range workspace.spec.Env {
		assert.NotContains(t, kv, "MODEL_KEY")
	}
	assert.Equal(t, "65532:65532", runtime.spec.User)
	assert.Equal(t, "65532:65532", adapter.spec.User, "the adapter reads and removes outbox entries the runtime wrote 0600")
	assert.Contains(t, runtime.spec.Cmd, testAgent.String())
	assert.NotContains(t, runtime.spec.Cmd, "--require-store", "the first start on a new volume creates the store")

	assert.Contains(t, runtime.files, "/etc/sherlock/config.yaml")
	assert.Equal(t, 0o600, int(runtime.files["/etc/sherlock/config.yaml"].Mode))
	for path := range adapter.files {
		assert.NotContains(t, runtime.files, path)
		assert.NotContains(t, workspace.files, path)
	}
	assert.Equal(t, h.id.keyPEM, adapter.files["/etc/garam/agent.key"].Data)
	assert.Equal(t, 0o600, int(adapter.files["/etc/garam/agent.key"].Mode))
	assert.Equal(t, 65532, adapter.files["/etc/garam/agent.key"].UID)
}

// TestActivate_ForwardsTheGenerationTheRuntimeIs drives the adapter's
// own client against the supervisor: the first activation is forwarded
// with the presented leaf and no anchor, a retry repeats it exactly and
// answers a newer token of the same activation, and the runtime's
// status for it is taken.
func TestActivate_ForwardsTheGenerationTheRuntimeIs(t *testing.T) {
	h := newHost(t)
	h.start()
	_, _, adapter := h.serving()
	auth, token := h.adapterOf(adapter)
	generation := newGeneration()
	id := delivery.RequestID(binding(generation), token)

	first, err := auth.Activate(context.Background(), id, binding(generation), token)
	require.NoError(t, err)
	again, err := auth.Activate(context.Background(), id, binding(generation), token)
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID)
	assert.Greater(t, again.TokenVersion, first.TokenVersion)

	asked := h.garam.asked()
	require.Len(t, asked, 2)
	assert.Equal(t, asked[0], asked[1], "a retry repeats the first request")
	assert.Equal(t, workload.ActivationRequest{
		RequestID: id, Epoch: "3", Generation: generation, Replaces: "", CertificatePEM: string(h.id.leafPEM),
	}, asked[0])

	status := &delivery.RuntimeStatus{Agent: testAgent, Epoch: "3", Generation: generation, Revision: "r1",
		State: delivery.RuntimeServing, StartedAt: time.Now()}
	require.NoError(t, auth.Report(context.Background(), first.ID, status, time.Now()))
	err = auth.Report(context.Background(), "another-activation", status, time.Now())
	assert.True(t, errors.Is(err, delivery.ErrGenerationSuperseded),
		"a status of another activation is not current: %v", err)
}

// TestActivate_RefusesWhatIsNotThisGeneration holds each check the
// supervisor makes before it forwards an activation, against the same
// request accepted: the token, the epoch, the revision, the runtime's
// own start, and a generation never asked for before.
func TestActivate_RefusesWhatIsNotThisGeneration(t *testing.T) {
	h := newHost(t)
	h.start()
	runtime, _, adapter := h.serving()
	auth, token := h.adapterOf(adapter)

	activate := func(b delivery.Binding, presented string) error {
		_, err := auth.Activate(context.Background(), delivery.RequestID(b, presented), b, presented)
		return err
	}
	refusedAs := func(t *testing.T, err error, kind string) {
		t.Helper()
		require.Error(t, err)
		assert.Contains(t, err.Error(), kind)
	}

	stale := newGeneration()
	t.Run("a wrong placement token", func(t *testing.T) {
		refusedAs(t, activate(binding(stale), "not-the-token"), "placement_not_current")
	})
	t.Run("another epoch", func(t *testing.T) {
		b := binding(stale)
		b.Epoch = "2"
		refusedAs(t, activate(b, token), "epoch_superseded")
	})
	t.Run("another revision", func(t *testing.T) {
		b := binding(stale)
		b.Revision = "r0"
		refusedAs(t, activate(b, token), "placement_not_current")
	})
	assert.Empty(t, h.garam.asked(), "nothing refused reached garam")

	generation := newGeneration()
	_, err := auth.Activate(context.Background(), delivery.RequestID(binding(generation), token), binding(generation), token)
	require.NoError(t, err, "the control: this generation's own request is forwarded")

	t.Run("a second generation of the same runtime", func(t *testing.T) {
		refusedAs(t, activate(binding(newGeneration()), token), "placement_not_current")
	})
	t.Run("a runtime started again since", func(t *testing.T) {
		h.engine.mu.Lock()
		runtime.startedAt = runtime.startedAt.Add(time.Second)
		h.engine.mu.Unlock()
		refusedAs(t, activate(binding(generation), token), "placement_not_current")
	})
	assert.Len(t, h.garam.asked(), 1, "only the control reached garam")
}

// TestActivate_RefusesAnUnauthenticatedCaller holds the transport's
// own checks: no placement token, a certificate naming another agent,
// a certificate another authority signed, and a request under another
// contract are refused, beside the adapter's own request accepted.
func TestActivate_RefusesAnUnauthenticatedCaller(t *testing.T) {
	h := newHost(t)
	h.start()
	_, _, adapter := h.serving()
	_, token := h.adapterOf(adapter)
	generation := newGeneration()
	body := fmt.Sprintf(`{"requestId":%q,"epoch":"3","generation":%q,"configRevision":"r1"}`,
		delivery.RequestID(binding(generation), token), generation)

	post := func(client *http.Client, contract, authorization string) (*http.Response, error) {
		req, err := http.NewRequest(http.MethodPost,
			"https://supervisor/v1/agents/"+testAgent.String()+"/activations", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Garam-Contract-Version", contract)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		resp, err := client.Do(req)
		if err == nil {
			t.Cleanup(func() { _ = resp.Body.Close() })
		}
		return resp, err
	}
	statusOf := func(resp *http.Response, err error) int {
		require.NoError(t, err)
		return resp.StatusCode
	}
	own := h.clientOf(adapter, h.id.leaf)

	assert.Equal(t, http.StatusUnauthorized, statusOf(post(own, workload.ExecutionContract, "")),
		"no placement token")
	assert.Equal(t, http.StatusBadRequest, statusOf(post(own, "agent-execution.v0", "Garam-Placement "+token)),
		"another contract")
	sibling := h.id.sign(t, "grn:acme:default:agent:ffffffffffffffff")
	assert.Equal(t, http.StatusForbidden, statusOf(post(h.clientOf(adapter, sibling.leaf),
		workload.ExecutionContract, "Garam-Placement "+token)), "a certificate of the same authority naming another agent")
	other := newIdentity(t, testAgent.String())
	_, err := post(h.clientOf(adapter, other.leaf), workload.ExecutionContract, "Garam-Placement "+token)
	require.Error(t, err, "a certificate another authority signed fails the handshake")
	assert.Empty(t, h.garam.asked())

	assert.Equal(t, http.StatusCreated, statusOf(post(own, workload.ExecutionContract, "Garam-Placement "+token)),
		"the control: the adapter's own request")
}

// TestActivate_MapsGaramsAnswer holds what the adapter is told when
// garam does not activate: undecided is retried, superseded and
// refused are definite.
func TestActivate_MapsGaramsAnswer(t *testing.T) {
	tests := []struct {
		name   string
		refuse error
		want   errorx.Kind
		kind   string
	}{
		{"garam unavailable", errorx.New(errorx.KindUnavailable, "down"), errorx.KindUnavailable, ""},
		{"garam refusing the anchor", errorx.New(errorx.KindFailedPrecondition, "409"), errorx.KindFailedPrecondition, "activation_superseded"},
		{"garam refusing the authority", errorx.New(errorx.KindPermissionDenied, "403"), errorx.KindFailedPrecondition, "not_authorized"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost(t)
			h.garam.refuse = tt.refuse
			h.start()
			_, _, adapter := h.serving()
			auth, token := h.adapterOf(adapter)
			generation := newGeneration()
			_, err := auth.Activate(context.Background(), delivery.RequestID(binding(generation), token), binding(generation), token)
			require.Error(t, err)
			assert.Equal(t, tt.want, errorx.KindOf(err), err.Error())
			assert.Contains(t, err.Error(), tt.kind)
		})
	}
}

// TestActivate_RefusesAGenerationAnEarlierIntentActivated holds a stale
// listener off the successor: the generation its predecessor was
// activated for is refused on the successor's socket, with the
// successor's own token, beside the successor's own generation
// accepted.
func TestActivate_RefusesAGenerationAnEarlierIntentActivated(t *testing.T) {
	h := newHost(t)
	first := h.start()
	_, _, adapter := h.serving()
	auth, token := h.adapterOf(adapter)
	stale := newGeneration()
	_, err := auth.Activate(context.Background(), delivery.RequestID(binding(stale), token), binding(stale), token)
	require.NoError(t, err)
	require.NoError(t, first.stop())

	h.start(func(d *workload.Desired) { d.Replace = true })
	require.NoError(t, h.engine.await(func() bool {
		a := h.live(workload.RoleAdapter)
		return a != nil && a != adapter
	}))
	h.engine.mu.Lock()
	successor := h.live(workload.RoleAdapter)
	h.engine.mu.Unlock()
	auth, token = h.adapterOf(successor)
	_, err = auth.Activate(context.Background(), delivery.RequestID(binding(stale), token), binding(stale), token)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "placement_not_current")

	fresh := newGeneration()
	_, err = auth.Activate(context.Background(), delivery.RequestID(binding(fresh), token), binding(fresh), token)
	require.NoError(t, err, "the control: the successor's own generation")
	asked := h.garam.asked()
	require.Len(t, asked, 2)
	assert.Equal(t, "activation-"+asked[0].RequestID, asked[1].Replaces, "the successor names its predecessor's activation")
}

// TestNewSupervisor_RefusesAnImageThatCanMove holds every image to a
// reference that cannot come to mean other content, beside the same
// configuration with a digest and with an image ID accepted.
func TestNewSupervisor_RefusesAnImageThatCanMove(t *testing.T) {
	h := newHost(t)
	_, err := workload.NewSupervisor(h.engine, h.garam, h.config())
	require.NoError(t, err, "the control: digests and an image ID")
	_, err = workload.NewSupervisor(h.engine, h.garam, h.config(func(d *workload.Desired) {
		d.Images.Runtime = "sherlock-agent:v0.2.0"
	}))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// TestRun_GivesTheRuntimeItsEgoAndInstructions holds the runtime's ego
// and operator instructions to what the host named: each is copied in,
// readable by the runtime alone, and named by its flag. The control is
// the same start naming neither, which copies and names nothing, so the
// runtime keeps its own default ego.
func TestRun_GivesTheRuntimeItsEgoAndInstructions(t *testing.T) {
	for _, named := range []bool{false, true} {
		t.Run(fmt.Sprintf("named %v", named), func(t *testing.T) {
			h := newHost(t)
			h.start(func(d *workload.Desired) {
				if named {
					d.Ego, d.Instructions = []byte("ego"), []byte("reply with message_send")
				}
			})
			runtime, _, _ := h.serving()
			for path, flag := range map[string]string{
				"/etc/sherlock/ego.md": "--ego-file", "/etc/sherlock/instructions.md": "--instructions-file",
			} {
				f, copied := runtime.files[path]
				assert.Equal(t, named, copied, path)
				assert.Equal(t, named, slices.Contains(runtime.spec.Cmd, flag), flag)
				if named {
					assert.Equal(t, 0o600, int(f.Mode))
					assert.Equal(t, 65532, f.UID)
					assert.True(t, slices.Contains(runtime.spec.Cmd, path), "%s names %s", flag, path)
				}
			}
		})
	}
}
