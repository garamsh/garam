package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/platform/api/machine"
	"github.com/garamsh/garam/platform/internal/agent"
	"github.com/garamsh/garam/platform/internal/delivery"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// tokenControlStandIn is the lifecycle authority's activation route as
// agent-execution.v2 pins it (ADR-0100), in front of garam's real
// activateAgent under execution-fence.v2: it takes the control token
// from Authorization and the placement token from its own header, and
// forwards the control token with the hosted operator's own token,
// which is how garam proves the control token is the agent's and shown
// to it. It stands in for the Operator control service, which is not
// this repository's.
type tokenControlStandIn struct {
	stack  *tokenStack
	agent  grn.GRN
	server *httptest.Server

	mu       sync.Mutex
	ref      *string
	replaces map[string]*string
	latest   *string
	asked    []string
	tokens   []string
}

func newTokenControlStandIn(t *testing.T, s *tokenStack, m managed) *tokenControlStandIn {
	t.Helper()
	ref := m.ref
	c := &tokenControlStandIn{stack: s, agent: m.agent, ref: &ref, replaces: map[string]*string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/agents/{grn}/activations", c.activate)
	mux.HandleFunc("POST /v1/agents/{grn}/runtime-status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Garam-Contract-Version", delivery.TokenExecutionContract)
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Garam-Token ") {
			writeAnswer(w, http.StatusUnauthorized, map[string]string{"kind": "unauthenticated", "message": "token"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	c.server = httptest.NewTLSServer(mux)
	t.Cleanup(c.server.Close)
	return c
}

func (c *tokenControlStandIn) activate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Garam-Contract-Version", delivery.TokenExecutionContract)
	refuse := func(status int, kind string) {
		writeAnswer(w, status, map[string]string{"kind": kind, "message": kind})
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Garam-Token ")
	switch {
	case r.Header.Get("Garam-Contract-Version") != delivery.TokenExecutionContract:
		refuse(http.StatusBadRequest, "invalid_request")
		return
	case !ok || r.Header.Get("Garam-Placement") == "":
		refuse(http.StatusUnauthorized, "unauthenticated")
		return
	case r.PathValue("grn") != c.agent.String():
		refuse(http.StatusForbidden, "not_authorized")
		return
	}
	var body struct {
		RequestID      string `json:"requestId"`
		Epoch          string `json:"epoch"`
		Generation     string `json:"generation"`
		ConfigRevision string `json:"configRevision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		refuse(http.StatusBadRequest, "invalid_request")
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.asked = append(c.asked, body.RequestID)
	c.tokens = append(c.tokens, token)
	replaces, replay := c.replaces[body.RequestID]
	if !replay {
		replaces = c.latest
		c.replaces[body.RequestID] = replaces
	}
	var ref *string
	if replaces == nil {
		ref = c.ref
	}
	status, answer, err := c.forward(body.RequestID, agent.Generation(body.Generation), replaces, ref, token)
	switch {
	case err != nil || status >= http.StatusInternalServerError || status == http.StatusUnauthorized:
		refuse(http.StatusServiceUnavailable, "undecided")
		return
	case status == http.StatusConflict:
		refuse(http.StatusConflict, "activation_superseded")
		return
	case status != http.StatusOK && status != http.StatusCreated:
		refuse(http.StatusForbidden, "credential_fenced")
		return
	}
	var a machinegen.Activation
	if err := json.Unmarshal(answer, &a); err != nil {
		refuse(http.StatusServiceUnavailable, "undecided")
		return
	}
	c.latest = &a.ActivationId
	writeAnswer(w, status, map[string]any{
		"activationId": a.ActivationId, "tokenVersion": a.TokenVersion, "token": a.Token,
		"grn": a.Grn, "epoch": a.Epoch, "generation": a.Generation, "configRevision": body.ConfigRevision,
	})
}

// forward is activateAgent under execution-fence.v2 with the hosted
// operator's own token, forwarding token.
func (c *tokenControlStandIn) forward(requestID string, g agent.Generation, replaces, ref *string, token string) (int, []byte, error) {
	body := tokenActivationBody(requestID, g, replaces, ref, token)
	req, err := http.NewRequest(http.MethodPost, c.stack.machine+"/agents/"+c.agent.String()+"/activations", bytes.NewReader([]byte(body)))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Garam-Token "+c.stack.hostedToken)
	req.Header.Set(operator.ContractVersionHeader, agent.TokenExecutionContractVersion)
	resp, err := c.stack.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	return resp.StatusCode, answer, err
}

func (c *tokenControlStandIn) requests() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.asked...)
}

// askedWith reports whether an activation was asked for on token.
func (c *tokenControlStandIn) askedWith(token string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Contains(c.tokens, token)
}

// tokenFiles is a token-mode adapter's files: garam's token, control's
// beside it where the adapter is fenced, and the placement token.
type tokenFiles struct {
	garam, control, placement string
}

func newTokenFiles(t *testing.T, garam, control string) tokenFiles {
	t.Helper()
	dir := t.TempDir()
	f := tokenFiles{garam: filepath.Join(dir, "agent-token")}
	replaceToken(t, f.garam, garam)
	if control != "" {
		f.control, f.placement = filepath.Join(dir, "control-token"), filepath.Join(dir, "placement-token")
		replaceToken(t, f.control, control)
		replaceToken(t, f.placement, "placement-1")
	}
	return f
}

// replaceToken replaces the file at path with token in one rename, as
// an assignee keeps its copy current.
func replaceToken(t *testing.T, path, token string) {
	t.Helper()
	tmp := path + ".next"
	require.NoError(t, os.WriteFile(tmp, []byte(token+"\n"), 0o600))
	require.NoError(t, os.Rename(tmp, path))
}

// projectedTokens is a fenced adapter's two tokens mounted as a Secret
// volume mounts them: each file links through ..data, and one rename of
// ..data replaces both at once.
type projectedTokens struct {
	tokenFiles
	dir     string
	version int
}

func newProjectedTokens(t *testing.T, garam, control string) *projectedTokens {
	t.Helper()
	p := &projectedTokens{dir: t.TempDir()}
	p.replace(t, garam, control)
	p.tokenFiles = tokenFiles{
		garam: filepath.Join(p.dir, "agent-token"), control: filepath.Join(p.dir, "control-token"),
		placement: filepath.Join(p.dir, "placement-token"),
	}
	for _, name := range []string{"agent-token", "control-token"} {
		require.NoError(t, os.Symlink(filepath.Join("..data", name), filepath.Join(p.dir, name)))
	}
	replaceToken(t, p.placement, "placement-1")
	return p
}

// replace writes both tokens into a new directory and moves ..data to
// it in one rename.
func (p *projectedTokens) replace(t *testing.T, garam, control string) {
	t.Helper()
	p.version++
	version := fmt.Sprintf("..v%d", p.version)
	require.NoError(t, os.Mkdir(filepath.Join(p.dir, version), 0o700))
	for name, token := range map[string]string{"agent-token": garam, "control-token": control} {
		require.NoError(t, os.WriteFile(filepath.Join(p.dir, version, name), []byte(token+"\n"), 0o600))
	}
	next := filepath.Join(p.dir, "..data.next")
	require.NoError(t, os.Symlink(version, next))
	require.NoError(t, os.Rename(next, filepath.Join(p.dir, "..data")))
}

// tokenFileOf is the token mode's credential over path.
func tokenFileOf(t *testing.T, path string) *delivery.TokenFile {
	t.Helper()
	f, err := delivery.NewTokenFile(path)
	require.NoError(t, err)
	return f
}

// serverTLSOnly reaches garam or control as a token-mode adapter does:
// presenting no certificate.
func serverTLSOnly(t *testing.T) *http.Client {
	t.Helper()
	client, _ := tokenClient(t)
	return client
}

// startTokenAdapter runs a token-mode adapter for g beside rt, fenced
// through c, forwarding the outbox in dir.
func startTokenAdapter(t *testing.T, s *tokenStack, c *tokenControlStandIn, rt *runtimeDouble, g grn.GRN, files tokenFiles, dir string) *adapterProcess {
	t.Helper()
	return startTokenAdapterSigning(t, s.executionStack, c, rt, g, files, dir, false)
}

// startTokenAdapterSigning is startTokenAdapter with the adapter's
// signing switch as sign says.
func startTokenAdapterSigning(t *testing.T, s *executionStack, c *tokenControlStandIn, rt *runtimeDouble, g grn.GRN, files tokenFiles, dir string, sign bool) *adapterProcess {
	t.Helper()
	garam := tokenFileOf(t, files.garam)
	control := tokenFileOf(t, files.control)
	execution := delivery.NewFencedExecution()
	credentials := delivery.NewFileCredentials(files.placement, garam, control)
	authority, err := delivery.NewHTTPAuthority(c.server.Client(), c.server.URL, g, control)
	require.NoError(t, err)
	queue, err := delivery.NewHTTPQueue(serverTLSOnly(t), s.machine, g, execution, garam)
	require.NoError(t, err)
	gateway, err := delivery.NewHTTPGateway(rt.server.Client(), rt.server.URL, g)
	require.NoError(t, err)
	outbox, err := delivery.NewOutbox(dir)
	require.NoError(t, err)
	loop, err := delivery.NewService(delivery.Ends{
		Agent: g, Queue: queue, Gateway: gateway, Execution: execution, Authority: authority,
		Credentials: credentials, Outbox: outbox,
	}, delivery.Config{
		RetryDelay: 10 * time.Millisecond, MaxRetryDelay: 50 * time.Millisecond, MinRenewInterval: 50 * time.Millisecond,
		StatusInterval: 10 * time.Millisecond, OutboxInterval: 20 * time.Millisecond,
		ClaimTimeout: 5 * time.Second, CallTimeout: 2 * time.Second,
		ForwardSignature: sign,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		assert.NoError(t, loop.Run(ctx))
	}()
	var once sync.Once
	stop := func() { once.Do(func() { cancel(); <-done }) }
	t.Cleanup(stop)
	return &adapterProcess{execution: execution, queue: queue, stop: stop}
}

// postToAgent has the peer post a message to g and answers its
// identifier.
func (s *executionStack) postToAgent(t *testing.T, g grn.GRN, body string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"recipient": g.String(), "idempotencyKey": s.key(), "body": body})
	require.NoError(t, err)
	got := s.send(t, s.peerTLS, http.MethodPost, "/messages", "", "", string(raw))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var m machinegen.Message
	require.NoError(t, json.Unmarshal([]byte(got.body), &m))
	return m.Id
}

// restartAs is the runtime starting again under another generation.
func (r *runtimeDouble) restartAs(g agent.Generation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status["generation"] = string(g)
	r.status["started_at"] = time.Now().UTC().Format(time.RFC3339)
}

// carried waits for id to reach rt, ends its turn and waits for garam
// to hold it completed.
func carried(t *testing.T, s *executionStack, rt *runtimeDouble, id string) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, got := range rt.received() {
			if got["id"] == id {
				return true
			}
		}
		return false
	}, 10*time.Second, 10*time.Millisecond, "the message reaches the runtime")
	rt.finish(id)
	require.Eventually(t, func() bool { return s.stateOf(t, id) == "completed" }, 10*time.Second, 20*time.Millisecond,
		"the turn's end completes it at garam")
}

// TestAdapterTokenMode_AFencedAdapterKeepsItsTwoAudiencesApart holds
// ADR-0100's separation against the real listener: garam refuses the
// control audience's token on a claim and the authority cannot activate
// on garam's, while each in its own place carries a message under an
// activation that agent-execution.v2 obtained. Each refusal stands
// beside its own control.
func TestAdapterTokenMode_AFencedAdapterKeepsItsTwoAudiencesApart(t *testing.T) {
	s := newTokenStack(t)
	m := s.createdV2(t, "c-1")
	own := s.agentToken(t, m.agent, nil)
	shown := s.agentToken(t, m.agent, &s.hosted)
	c := newTokenControlStandIn(t, s, m)

	files := newTokenFiles(t, own.Token, shown.Token)
	rt := newRuntimeDouble(t, m.agent, 1, generationOf(1))
	process := startTokenAdapter(t, s, c, rt, m.agent, files, t.TempDir())
	carried(t, s.executionStack, rt, s.postToAgent(t, m.agent, "hello"))
	_, err := process.execution.Present()
	require.NoError(t, err, "the control: each token in its own place activates and carries")
	asked := c.requests()

	// The same activation, presented beside the control audience's token
	// in garam's place: garam refuses that token.
	misplaced := newTokenFiles(t, shown.Token, own.Token)
	claims, err := delivery.NewHTTPQueue(serverTLSOnly(t), s.machine, m.agent, process.execution,
		tokenFileOf(t, misplaced.garam))
	require.NoError(t, err)
	_, err = claims.Claim(context.Background())
	require.ErrorIs(t, err, delivery.ErrTokenRefused, "garam refuses the control audience's token")
	authority, err := delivery.NewHTTPAuthority(c.server.Client(), c.server.URL, m.agent, tokenFileOf(t, misplaced.control))
	require.NoError(t, err)
	b := delivery.Binding{Agent: m.agent, Epoch: "1", Generation: string(generationOf(9)), Revision: "1"}
	_, err = authority.Activate(context.Background(),
		delivery.RequestID(delivery.TokenExecutionContract, b, "placement-1"), b, "placement-1")
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition), "garam's token activates nothing: %v", err)

	b.Generation = string(generationOf(1))
	assert.Equal(t, delivery.RequestID(delivery.TokenExecutionContract, b, "placement-1"), asked[len(asked)-1])
}

// TestAdapterTokenMode_ARecoveryIsResumedByTheNextGeneration is
// agent:recover reaching a running token-mode adapter. The recovery
// refuses the replaced lineage's garam token and ends the activation.
// Writing the new lineage's tokens over both files makes the adapter
// ask again, and garam refuses the same request as ended: a rewritten
// token activates nothing and clears no fence. The runtime's next
// generation is new intent, which activates and carries — the control.
func TestAdapterTokenMode_ARecoveryIsResumedByTheNextGeneration(t *testing.T) {
	s := newTokenStack(t)
	m := s.createdV2(t, "c-1")
	own := s.agentToken(t, m.agent, nil)
	shown := s.agentToken(t, m.agent, &s.hosted)
	c := newTokenControlStandIn(t, s, m)
	files := newProjectedTokens(t, own.Token, shown.Token)
	rt := newRuntimeDouble(t, m.agent, 1, generationOf(1))
	startTokenAdapter(t, s, c, rt, m.agent, files.tokenFiles, t.TempDir())
	carried(t, s.executionStack, rt, s.postToAgent(t, m.agent, "hello"))

	body := `{"requestId":"rec-1","epoch":"1"}`
	digest := sha256Hex(body)
	status, minted := s.mintRecovery(t, m.agent, digest, "h-rec-1")
	require.Equal(t, http.StatusCreated, status, minted)
	got := s.v2(t, http.MethodPost, "/agents/"+m.agent.String()+"/credential-recovery",
		agent.TokenExecutionContractVersion, s.hostedToken, authorityOf(t, minted).Authority, body)
	require.Equal(t, http.StatusCreated, got.status, got.body)

	// The recovery fences the claim in flight before the swap or after
	// it, and a fenced adapter asks again once an input changed. The
	// look after the ask on the new control token re-reads both files,
	// so the count is taken after it.
	next := s.agentToken(t, m.agent, &s.hosted).Token
	files.replace(t, s.agentToken(t, m.agent, nil).Token, next)
	require.Eventually(t, func() bool { return c.askedWith(next) }, 10*time.Second, 10*time.Millisecond,
		"the rewritten tokens ask again")
	looks := rt.looked()
	require.Eventually(t, func() bool { return rt.looked() >= looks+2 }, 10*time.Second, 10*time.Millisecond,
		"the adapter looks again at the tokens it asked on")
	asked := c.requests()
	for _, id := range asked[1:] {
		assert.Equal(t, asked[0], id, "under the same request")
	}
	waiting := s.postToAgent(t, m.agent, "after the recovery")
	time.Sleep(time.Second)
	assert.Len(t, c.requests(), len(asked), "the ended activation is not asked for again")
	for _, received := range rt.received() {
		assert.NotEqual(t, waiting, received["id"], "nothing is carried on the ended activation")
	}

	rt.restartAs(generationOf(2))
	carried(t, s.executionStack, rt, waiting)
	assert.Len(t, c.requests(), len(asked)+1, "the next generation is one new request")
}

// sha256Hex is the hex SHA-256 of body.
func sha256Hex(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
