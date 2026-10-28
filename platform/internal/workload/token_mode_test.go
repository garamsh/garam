package workload_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/delivery"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/workload"
)

// stubTokens is the supervisor's agent token as its exchange and mint
// would hold it: one current token, replaced as a refresh replaces it.
type stubTokens struct {
	mu      sync.Mutex
	current string
}

func (s *stubTokens) Current(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current, nil
}

func (s *stubTokens) set(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = token
}

// agentToken is an agent token as garam spells one, made of c.
func agentToken(c string) string { return "garam_at_" + strings.Repeat(c, 64) }

// TestRun_TokenModeHandsTheAdapterTokensAndNoCertificate holds what a
// token-mode adapter is given (ADR-0100): the agent token garam accepts
// and the generation's control token, each its own file readable by the
// adapter's account alone, and no agent certificate or key. The two
// tokens differ, and the control token is not one the supervisor
// obtained from garam.
func TestRun_TokenModeHandsTheAdapterTokensAndNoCertificate(t *testing.T) {
	h := newHost(t)
	h.start()
	_, _, adapter := h.serving()

	agentFile, controlFile := adapter.files["/etc/garam/agent-token"], adapter.files["/etc/garam/control-token"]
	assert.Equal(t, agentToken("a"), string(agentFile.Data))
	assert.Regexp(t, `^garam_at_[0-9a-f]{64}$`, string(controlFile.Data), "spelled as the adapter reads a token")
	assert.NotEqual(t, agentFile.Data, controlFile.Data, "the two audiences are two tokens")
	for _, f := range []workload.File{agentFile, controlFile} {
		assert.Equal(t, 0o600, int(f.Mode))
		assert.Equal(t, 65532, f.UID)
	}
	assert.NotContains(t, adapter.files, "/etc/garam/agent.crt")
	assert.NotContains(t, adapter.files, "/etc/garam/agent.key")
	assert.Contains(t, adapter.spec.Env, "GARAM_ADAPTER_TOKEN_FILE=/etc/garam/agent-token")
	assert.Contains(t, adapter.spec.Env, "GARAM_ADAPTER_CONTROL_TOKEN_FILE=/etc/garam/control-token")
	for _, kv := range adapter.spec.Env {
		assert.NotContains(t, kv, "GARAM_ADAPTER_TLS_", "no certificate setting beside the tokens")
		assert.NotContains(t, kv, "garam_at_", "no token travels in the environment")
	}
}

// TestActivate_TokenModeForwardsTheAgentTokenUnderV2 drives the
// token-mode adapter's own client against the supervisor: it activates
// under agent-execution.v2, and the supervisor forwards the agent token
// it minted for garam, the one it holds when it forwards, and no
// certificate. The runtime's status is taken under the same token.
func TestActivate_TokenModeForwardsTheAgentTokenUnderV2(t *testing.T) {
	h := newHost(t)
	tokens := h.tokens
	h.start()
	_, _, adapter := h.serving()
	auth, placement := h.adapterOf(adapter)
	generation := newGeneration()
	id := delivery.RequestID(delivery.TokenExecutionContract, binding(generation), placement)

	first, err := auth.Activate(context.Background(), id, binding(generation), placement)
	require.NoError(t, err)
	tokens.set(agentToken("b"))
	again, err := auth.Activate(context.Background(), id, binding(generation), placement)
	require.NoError(t, err)
	assert.Equal(t, first.ID, again.ID, "a retry is the same activation")

	asked := h.garam.asked()
	require.Len(t, asked, 2)
	assert.Equal(t, workload.ActivationRequest{
		RequestID: id, Epoch: "3", Generation: generation, Token: agentToken("a"),
	}, asked[0])
	assert.Equal(t, agentToken("b"), asked[1].Token, "a retry forwards the token held then")

	status := &delivery.RuntimeStatus{Agent: testAgent, Epoch: "3", Generation: generation, Revision: "r1",
		State: delivery.RuntimeServing, StartedAt: time.Now()}
	require.NoError(t, auth.Report(context.Background(), first.ID, status, time.Now()))
}

// TestActivate_TokenModeAcceptsTheGenerationsControlTokenAlone holds
// the token mode's control to its one credential: no token, garam's
// agent token, another token of the same spelling and another contract
// are each refused before anything reaches garam. The
// adapter's own request, which carries the generation's control token,
// is the control.
func TestActivate_TokenModeAcceptsTheGenerationsControlTokenAlone(t *testing.T) {
	h := newHost(t)
	h.start()
	_, _, adapter := h.serving()
	_, placement := h.adapterOf(adapter)
	control := string(adapter.files["/etc/garam/control-token"].Data)
	generation := newGeneration()
	body := fmt.Sprintf(`{"requestId":%q,"epoch":"3","generation":%q,"configRevision":"r1"}`,
		delivery.RequestID(delivery.TokenExecutionContract, binding(generation), placement), generation)
	client := h.clientOf(adapter)
	post := func(contract, token string) int {
		req, err := http.NewRequest(http.MethodPost,
			"https://supervisor/v1/agents/"+testAgent.String()+"/activations", strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Garam-Contract-Version", contract)
		req.Header.Set("Garam-Placement", placement)
		if token != "" {
			req.Header.Set("Authorization", "Garam-Token "+token)
		}
		resp, err := client.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	assert.Equal(t, http.StatusUnauthorized, post(workload.TokenExecutionContract, ""), "no token")
	assert.Equal(t, http.StatusUnauthorized, post(workload.TokenExecutionContract, agentToken("a")),
		"garam's agent token is not control's")
	assert.Equal(t, http.StatusUnauthorized, post(workload.TokenExecutionContract, agentToken("f")),
		"a token of the same spelling that is not the generation's")
	assert.Equal(t, http.StatusBadRequest, post("agent-execution.v1", control),
		"another contract is not served")
	assert.Empty(t, h.garam.asked(), "nothing reached garam")

	assert.Equal(t, http.StatusCreated, post(workload.TokenExecutionContract, control),
		"the control: the generation's control token")
}

// TestRun_TokenModeHandsARefreshedTokenToTheRunningAdapter is the
// refresh reaching a running adapter without a restart: the token the
// supervisor holds replaces the adapter's file. The unchanged file
// before the refresh is the control.
func TestRun_TokenModeHandsARefreshedTokenToTheRunningAdapter(t *testing.T) {
	h := newHost(t)
	tokens := h.tokens
	h.start()
	_, _, adapter := h.serving()
	handed := func() string {
		h.engine.mu.Lock()
		defer h.engine.mu.Unlock()
		return string(adapter.files["/etc/garam/agent-token"].Data)
	}
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, agentToken("a"), handed(), "the control: nothing changes while the token does not")

	tokens.set(agentToken("b"))
	require.Eventually(t, func() bool { return handed() == agentToken("b") }, 5*time.Second, 5*time.Millisecond,
		"the refreshed token reaches the running adapter")
	h.engine.mu.Lock()
	defer h.engine.mu.Unlock()
	assert.Equal(t, workload.ContainerRunning, adapter.status, "without a restart")
}

// TestRun_TokenModeKeepsTheControlTokenAcrossARestart is a supervisor
// restarted beside its running generation: the generation's control
// token is recorded with its intent, so the adapter it was handed to
// still activates against the new process.
func TestRun_TokenModeKeepsTheControlTokenAcrossARestart(t *testing.T) {
	h := newHost(t)
	first := h.start()
	_, _, adapter := h.serving()
	auth, placement := h.adapterOf(adapter)
	require.NoError(t, first.stop())

	h.start()
	generation := newGeneration()
	var err error
	require.Eventually(t, func() bool {
		_, err = auth.Activate(context.Background(),
			delivery.RequestID(delivery.TokenExecutionContract, binding(generation), placement), binding(generation), placement)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond, "the adapter's control token is still the generation's: %v", err)
}

// TestNewSupervisor_RequiresTheAgentsTokens refuses a host with no
// source of the agent's token, which is the one credential an adapter
// is handed (ADR-0118); the same configuration with one is the control.
func TestNewSupervisor_RequiresTheAgentsTokens(t *testing.T) {
	h := newHost(t)
	_, err := workload.NewSupervisor(h.engine, h.garam, h.config())
	require.NoError(t, err, "the control: the agent's tokens")

	none := h.config()
	none.Tokens = nil
	_, err = workload.NewSupervisor(h.engine, h.garam, none)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%v", err)
}

// controlTokenOf is the control token a generation's adapter was given.
func (h *host) controlTokenOf(adapter *stubContainer) string {
	h.engine.mu.Lock()
	defer h.engine.mu.Unlock()
	return string(adapter.files["/etc/garam/control-token"].Data)
}

// TestRun_TokenModeDrawsEachGenerationsControlToken holds the control
// token to its generation: a replacement draws another, so a token the
// earlier adapter held is not the next generation's. The same token
// across a supervisor restart, adopting the same generation, is the
// control.
func TestRun_TokenModeDrawsEachGenerationsControlToken(t *testing.T) {
	h := newHost(t)
	first := h.start()
	runtime, _, adapter := h.serving()
	firstToken := h.controlTokenOf(adapter)
	require.NoError(t, first.stop())

	again := h.start()
	_, _, adopted := h.serving()
	assert.Equal(t, firstToken, h.controlTokenOf(adopted), "the control: the same generation keeps its token")
	require.NoError(t, again.stop())

	h.start(replace)
	require.NoError(t, h.engine.await(func() bool { return runtime.status == workload.ContainerExited }))
	_, _, successor := h.serving()
	assert.NotEqual(t, firstToken, h.controlTokenOf(successor), "the next generation draws its own")
	assert.Regexp(t, `^garam_at_[0-9a-f]{64}$`, h.controlTokenOf(successor))
}

// forgetControlToken rewrites the host's record as a supervisor of the
// certificate mode ADR-0118 removed left it: its intent holds no
// control token.
func (h *host) forgetControlToken() {
	h.t.Helper()
	path := filepath.Join(h.dir, "workload.json")
	raw, err := os.ReadFile(path)
	require.NoError(h.t, err)
	var record map[string]any
	require.NoError(h.t, json.Unmarshal(raw, &record))
	intent, ok := record["intent"].(map[string]any)
	require.True(h.t, ok, "the record holds an intent")
	require.Contains(h.t, intent, "controlToken")
	delete(intent, "controlToken")
	raw, err = json.Marshal(record)
	require.NoError(h.t, err)
	require.NoError(h.t, os.WriteFile(path, raw, 0o600))
}

// TestRun_DrainsAGenerationRecordedWithoutAControlToken holds a
// supervisor to serving only an intent that holds a control token: a
// generation a certificate-mode supervisor started, recorded without
// one, is drained and replaced rather than adopted, so no request is
// ever compared with an empty token. The same record holding its
// token, adopted, is the control.
func TestRun_DrainsAGenerationRecordedWithoutAControlToken(t *testing.T) {
	h := newHost(t)
	first := h.start()
	runtime, _, _ := h.serving()
	require.NoError(t, first.stop())

	again := h.start()
	adopted, _, _ := h.serving()
	require.Same(t, runtime, adopted, "the control: a record holding its token is adopted")
	require.NoError(t, again.stop())

	h.forgetControlToken()
	h.start()
	require.NoError(t, h.engine.await(func() bool { return runtime.status == workload.ContainerExited }),
		"the generation recorded without a control token is drained")
	successor, _, adapter := h.serving()
	assert.NotSame(t, runtime, successor)
	assert.Regexp(t, `^garam_at_[0-9a-f]{64}$`, h.controlTokenOf(adapter), "and its successor holds a control token")
}
