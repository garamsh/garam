package agent_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/garamsh/gaon/lib/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/gaon-operator/internal/agent"
	"github.com/garamsh/garam/gaon-operator/tests/gaonfake"
	"github.com/garamsh/garam/lib/grn"
)

// stubFeed is a stand-in for the control service's controller API.
type stubFeed struct {
	mu       sync.Mutex
	agents   []agent.Desired
	err      error
	rendered []string
}

func (f *stubFeed) Desired(context.Context) ([]agent.Desired, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]agent.Desired(nil), f.agents...), f.err
}

func (f *stubFeed) ReportRendered(_ context.Context, g grn.GRN, revision string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rendered = append(f.rendered, g.String()+"@"+revision)
	return nil
}

func (f *stubFeed) set(agents ...agent.Desired) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents = agents
}

// stubGaram is a stand-in for garam's machine listener. Each token it
// mints is random, so a test can look for it where it must not be.
type stubGaram struct {
	mu        sync.Mutex
	minted    []agent.Credentials
	mintErr   error
	reports   []string
	reportErr error
}

func (g *stubGaram) MintTokens(_ context.Context, a grn.GRN, _ int64) (agent.Credentials, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.mintErr != nil {
		return nil, g.mintErr
	}
	c := agent.Credentials{"agent-token": []byte("garam_at_" + rand.Text()), "placement-token": []byte("garam_pt_" + rand.Text())}
	g.minted = append(g.minted, c)
	return c, nil
}

func (g *stubGaram) ReportState(_ context.Context, a grn.GRN, epoch int64, s agent.State) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.reports = append(g.reports, fmt.Sprintf("%s@%d=%s", a, epoch, s))
	return g.reportErr
}

func (g *stubGaram) reported() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.reports...)
}

// failingList makes the list of microvms fail, as an unreachable gaon
// does.
type failingList struct{ agent.Cluster }

func (failingList) ListMicroVMs(context.Context) (*api.MicroVMList, error) {
	return nil, errors.New("gaon unreachable")
}

type harness struct {
	feed    *stubFeed
	garam   *stubGaram
	gaon    *gaonfake.Server
	cluster agent.Cluster
	opts    agent.Options
	svc     agent.Service
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	gaon, client := gaonfake.New(t, agent.Selector())
	h := &harness{
		feed: &stubFeed{}, garam: &stubGaram{}, gaon: gaon, cluster: client,
		opts: agent.Options{
			Image:       "registry.example/garam/agent:1",
			GaramRoot:   []byte("garam root"),
			ControlRoot: []byte("control root"),
			Logger:      slog.New(slog.DiscardHandler),
		},
	}
	h.svc = agent.NewService(h.feed, h.garam, h.cluster, h.opts)
	return h
}

func (h *harness) restart() { h.svc = agent.NewService(h.feed, h.garam, h.cluster, h.opts) }

func desired(id string) agent.Desired {
	return agent.Desired{
		GRN:              grn.New("org1", "agent", id),
		Epoch:            3,
		Revision:         "2",
		MemoryStorage:    "1Gi",
		WorkspaceStorage: "10Gi",
	}
}

func objectName(t *testing.T, d agent.Desired) string {
	t.Helper()
	require.NoError(t, d.GRN.Validate(), "the test's GRN is not one")
	return "agent-" + sha16(d.GRN.String())
}

func TestReconcile_AgentYieldsSecretVolumesAndMicroVM(t *testing.T) {
	h := newHarness(t)
	d := desired("a1")
	h.feed.set(d)

	require.NoError(t, h.svc.Reconcile(t.Context()))

	name := objectName(t, d)
	m, ok := h.gaon.MicroVM(t, name)
	require.True(t, ok, "no microvm named %s", name)
	secret, ok := h.gaon.Secret(t, name)
	require.True(t, ok)
	_, memory := h.gaon.Volume(t, name+"-memory")
	_, workspace := h.gaon.Volume(t, name+"-workspace")
	assert.True(t, memory && workspace, "both volumes are written")

	for _, labels := range []map[string]string{m.Metadata.Labels, secret.Metadata.Labels} {
		assert.Equal(t, "agent", labels["garam.sh/role"])
		assert.NotEmpty(t, labels["garam.sh/organization"])
		assert.NotEmpty(t, labels["garam.sh/agent"])
		for k, v := range labels {
			assert.LessOrEqual(t, len(v), 63, "label %s", k)
			assert.NotContains(t, v, ":", "label %s", k)
		}
	}
	assert.Equal(t, d.GRN.String(), m.Metadata.Annotations["garam.sh/agent-grn"])
	assert.Equal(t, "3", m.Metadata.Annotations["garam.sh/epoch"])
	assert.Equal(t, "2", m.Metadata.Annotations["garam.sh/revision"])
	assert.Equal(t, api.RunAlways, m.Spec.RunStrategy)
	assert.Equal(t, "65532", m.Spec.User)

	require.NotEmpty(t, m.Spec.Files)
	for _, f := range m.Spec.Files {
		require.NotNil(t, f.ValueFrom, "file %s is not sourced from the Secret", f.Path)
		assert.Equal(t, name, f.ValueFrom.SecretKeyRef.Name)
		assert.Contains(t, secret.Data, f.ValueFrom.SecretKeyRef.Key)
		assert.Empty(t, f.Content, "file %s holds inline content", f.Path)
		require.NotNil(t, f.Mode)
		assert.Equal(t, int32(0o400), *f.Mode)
		require.NotNil(t, f.Owner)
		assert.Equal(t, int64(65532), f.Owner.UID)
	}

	// The control: the Secret does hold the tokens, so their absence
	// from the spec is the operator's doing and not an empty Secret's.
	require.Len(t, h.garam.minted, 1)
	spec, err := json.Marshal(m)
	require.NoError(t, err)
	for key, token := range h.garam.minted[0] {
		assert.Equal(t, token, secret.Data[key], "the Secret holds %s", key)
		assert.NotContains(t, string(spec), string(token), "the spec holds %s", key)
	}
	assert.NotContains(t, string(spec), "garam root")
}

func TestReconcile_ReportsOnlyWhatItObserved(t *testing.T) {
	ready := []api.Condition{{Type: "Ready", Status: api.ConditionTrue}}
	exit := &api.ExitRecord{Code: 1, Time: time.Unix(1, 0)}
	tests := []struct {
		name   string
		status api.MicroVMStatus
		want   agent.State
	}{
		{"a pending microvm is provisioned", api.MicroVMStatus{Phase: api.MicroVMPending}, agent.StateProvisioned},
		{"a running and ready microvm is ready", api.MicroVMStatus{Phase: api.MicroVMRunning, Conditions: ready}, agent.StateReady},
		{"a running microvm that is not ready is provisioned", api.MicroVMStatus{Phase: api.MicroVMRunning}, agent.StateProvisioned},
		{"a running microvm whose Ready is false is provisioned", api.MicroVMStatus{Phase: api.MicroVMRunning, Conditions: []api.Condition{{Type: "Ready", Status: api.ConditionFalse}}}, agent.StateProvisioned},
		{"a ready status of another epoch is void and provisioned", api.MicroVMStatus{Phase: api.MicroVMRunning, Conditions: ready, BindingEpoch: 9}, agent.StateProvisioned},
		{"a failed microvm with its last exit is failed", api.MicroVMStatus{Phase: api.MicroVMFailed, LastExit: exit}, agent.StateFailed},
		{"a failed microvm without a last exit is provisioned", api.MicroVMStatus{Phase: api.MicroVMFailed}, agent.StateProvisioned},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			d := desired("a1")
			h.feed.set(d)
			require.NoError(t, h.svc.Reconcile(t.Context()))
			h.gaon.SetStatus(t, objectName(t, d), tc.status)
			// A pass over the first state reported provisioned; the
			// second reports what the status now says.
			h.restart()

			require.NoError(t, h.svc.Reconcile(t.Context()))

			reports := h.garam.reported()
			assert.Equal(t, fmt.Sprintf("%s@3=%s", d.GRN, tc.want), reports[len(reports)-1])
		})
	}
}

func TestReconcile_ReportsEachChangeOnce(t *testing.T) {
	h := newHarness(t)
	d := desired("a1")
	h.feed.set(d)

	require.NoError(t, h.svc.Reconcile(t.Context()))
	require.NoError(t, h.svc.Reconcile(t.Context()))
	assert.Equal(t, []string{d.GRN.String() + "@3=provisioned"}, h.garam.reported())
	assert.Equal(t, []string{d.GRN.String() + "@2"}, h.feed.rendered)

	h.gaon.SetStatus(t, objectName(t, d), api.MicroVMStatus{Phase: api.MicroVMRunning, Conditions: []api.Condition{{Type: "Ready", Status: api.ConditionTrue}}})
	require.NoError(t, h.svc.Reconcile(t.Context()))
	assert.Len(t, h.garam.reported(), 2)
}

func TestReconcile_ReportsNothingWhereNothingWasRead(t *testing.T) {
	h := newHarness(t)
	h.feed.set(desired("a1"))
	svc := agent.NewService(h.feed, h.garam, failingList{h.cluster}, h.opts)

	err := svc.Reconcile(t.Context())

	require.Error(t, err)
	assert.Empty(t, h.garam.reported())
	assert.Empty(t, h.garam.minted, "no token is minted for an agent whose objects were not read")
	assert.Zero(t, h.gaon.Count("PUT", "microvms")+h.gaon.Count("PUT", "secrets"))
}

func TestReconcile_ADroppedReportOfAStaleEpochDoesNotFailThePass(t *testing.T) {
	h := newHarness(t)
	h.feed.set(desired("a1"))
	h.garam.reportErr = agent.ErrStaleEpoch

	require.NoError(t, h.svc.Reconcile(t.Context()))
	require.NoError(t, h.svc.Reconcile(t.Context()))

	assert.Len(t, h.garam.reported(), 1, "a refused report is not repeated until something changes")
}

func TestReconcile_AStoppedAgentIsHaltedAndNothingIsDeleted(t *testing.T) {
	h := newHarness(t)
	d := desired("a1")
	h.feed.set(d)
	require.NoError(t, h.svc.Reconcile(t.Context()))
	name := objectName(t, d)
	// A state that changed while the agent is stopped is not reported:
	// without this the dedupe would hide a report sent on a stop.
	h.gaon.SetStatus(t, name, api.MicroVMStatus{Phase: api.MicroVMRunning, Conditions: []api.Condition{{Type: "Ready", Status: api.ConditionTrue}}})

	d.Stopped = true
	h.feed.set(d)
	require.NoError(t, h.svc.Reconcile(t.Context()))

	m, ok := h.gaon.MicroVM(t, name)
	require.True(t, ok)
	assert.Equal(t, api.RunHalted, m.Spec.RunStrategy)
	assert.Zero(t, h.gaon.Count("DELETE", "microvms")+h.gaon.Count("DELETE", "secrets")+h.gaon.Count("DELETE", "volumes"))
	_, secret := h.gaon.Secret(t, name)
	_, volume := h.gaon.Volume(t, name+"-workspace")
	assert.True(t, secret && volume, "the Secret and the Volumes stay")
	assert.Len(t, h.garam.reported(), 1, "a halt reports no state")

	// The control: the stop is the feed's, and ending it starts the
	// agent again, with nothing minted a second time.
	d.Stopped = false
	h.feed.set(d)
	require.NoError(t, h.svc.Reconcile(t.Context()))
	m, _ = h.gaon.MicroVM(t, name)
	assert.Equal(t, api.RunAlways, m.Spec.RunStrategy)
	assert.Len(t, h.garam.minted, 1)
}

func TestReconcile_AStoppedAgentWithNoMicroVMIsNotStarted(t *testing.T) {
	h := newHarness(t)
	d := desired("a1")
	d.Stopped = true
	h.feed.set(d)

	require.NoError(t, h.svc.Reconcile(t.Context()))

	assert.Zero(t, h.gaon.Count("PUT", "microvms")+h.gaon.Count("PUT", "secrets")+h.gaon.Count("PUT", "volumes"))
	assert.Empty(t, h.garam.minted)
}

func TestReconcile_AnAgentThatLeftTheFeedIsHaltedAndKept(t *testing.T) {
	h := newHarness(t)
	d := desired("a1")
	h.feed.set(d)
	require.NoError(t, h.svc.Reconcile(t.Context()))

	h.feed.set()
	require.NoError(t, h.svc.Reconcile(t.Context()))

	m, ok := h.gaon.MicroVM(t, objectName(t, d))
	require.True(t, ok)
	assert.Equal(t, api.RunHalted, m.Spec.RunStrategy)
	assert.Zero(t, h.gaon.Count("DELETE", "microvms"))
	_, volume := h.gaon.Volume(t, objectName(t, d)+"-memory")
	assert.True(t, volume)
}

func TestReconcile_ARestartedOperatorCreatesNothingTwice(t *testing.T) {
	h := newHarness(t)
	h.feed.set(desired("a1"), desired("a2"))
	require.NoError(t, h.svc.Reconcile(t.Context()))
	writes := h.gaon.Count("PUT", "microvms") + h.gaon.Count("PUT", "secrets") + h.gaon.Count("PUT", "volumes")
	require.Equal(t, 8, writes, "two agents write four objects each")
	require.Len(t, h.garam.minted, 2)

	h.restart()
	require.NoError(t, h.svc.Reconcile(t.Context()))

	assert.Equal(t, writes, h.gaon.Count("PUT", "microvms")+h.gaon.Count("PUT", "secrets")+h.gaon.Count("PUT", "volumes"))
	assert.Len(t, h.garam.minted, 2, "no second token for a Secret that exists")
}

func TestReconcile_AMissingSecretIsMintedAgainAndAnExistingOneIsNot(t *testing.T) {
	h := newHarness(t)
	d := desired("a1")
	h.feed.set(d)
	h.garam.mintErr = errors.New("garam unavailable")

	require.Error(t, h.svc.Reconcile(t.Context()))
	_, microvm := h.gaon.MicroVM(t, objectName(t, d))
	assert.False(t, microvm, "no microvm is made without its credential")
	assert.Empty(t, h.garam.reported(), "nothing is reported for an agent that was not rendered")

	h.garam.mintErr = nil
	require.NoError(t, h.svc.Reconcile(t.Context()))
	assert.Len(t, h.garam.minted, 1)
}

// collidingGaram mints a credential named like a trust root.
type collidingGaram struct{ stubGaram }

func (g *collidingGaram) MintTokens(context.Context, grn.GRN, int64) (agent.Credentials, error) {
	return agent.Credentials{agent.KeyGaramRoot: []byte("garam_at_x")}, nil
}

func TestReconcile_ACredentialNamedLikeATrustRootIsRefused(t *testing.T) {
	h := newHarness(t)
	h.feed.set(desired("a1"))
	svc := agent.NewService(h.feed, &collidingGaram{}, h.cluster, h.opts)

	err := svc.Reconcile(t.Context())

	require.Error(t, err)
	assert.Zero(t, h.gaon.Count("PUT", "secrets")+h.gaon.Count("PUT", "microvms"))
}

func TestReconcile_DoesNotTouchWhatItDoesNotOwn(t *testing.T) {
	// A client built with no selector lists every object, so the filter
	// in the operator is what is under test.
	gaon, everything := gaonfake.New(t, nil)
	other := &api.MicroVM{
		TypeMeta: api.TypeMeta{APIVersion: api.GroupVersion, Kind: api.KindMicroVM},
		Metadata: api.ObjectMeta{Name: "someone-else", Labels: map[string]string{"app": "other"}},
		Spec:     api.MicroVMSpec{Image: "x", RunStrategy: api.RunAlways},
	}
	_, err := everything.PutMicroVM(t.Context(), other)
	require.NoError(t, err)
	h := newHarness(t)
	svc := agent.NewService(h.feed, h.garam, everything, h.opts)
	h.feed.set(desired("a1"))

	require.NoError(t, svc.Reconcile(t.Context()))

	got, ok := gaon.MicroVM(t, "someone-else")
	require.True(t, ok)
	assert.Equal(t, api.RunAlways, got.Spec.RunStrategy, "an object without the operator's label is not halted")
	owned, ok := gaon.MicroVM(t, objectName(t, desired("a1")))
	require.True(t, ok)
	assert.Equal(t, api.RunAlways, owned.Spec.RunStrategy)
}

func TestCredentials_NeverPrintTheirValues(t *testing.T) {
	c := agent.Credentials{"agent-token": []byte("garam_at_secretvalue")}
	var out strings.Builder
	slog.New(slog.NewTextHandler(&out, nil)).Info("minted", "credentials", c)

	for _, s := range []string{out.String(), fmt.Sprint(c), fmt.Sprintf("%v %+v %#v", c, c, c)} {
		assert.NotContains(t, s, "secretvalue")
	}
	assert.Contains(t, out.String(), "redacted")
}

func sha16(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}
