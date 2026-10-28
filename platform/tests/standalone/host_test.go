package standalone_test

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/stretchr/testify/require"
)

// host is one standalone host's two directories, as the shipped
// `garam standalone` commands keep them.
type host struct {
	s          *stack
	name       string
	supervisor string
	agent      string
	// approvalRequest is what prepare printed for the administrator.
	request map[string]string
}

func (s *stack) newHost(name string) *host {
	s.t.Helper()
	base := filepath.Join(s.dir, name)
	return &host{s: s, name: name, supervisor: filepath.Join(base, "supervisor"), agent: filepath.Join(base, "agent")}
}

// prepare runs `garam standalone prepare` and keeps what it printed.
func (h *host) prepare() {
	h.s.t.Helper()
	out, err := h.s.cli("standalone", "prepare", "--dir", h.supervisor, "--agent-dir", h.agent)
	require.NoError(h.s.t, err, out)
	require.NoError(h.s.t, json.Unmarshal([]byte(out), &h.request), out)
}

// approve has the stack's principal, the organization's owner, approve
// what prepare printed through the public API, expiring at expiresAt,
// and answers the approval's identifier.
func (h *host) approve(expiresAt time.Time) string {
	h.s.t.Helper()
	approval := map[string]string{
		"requestId": h.request["requestId"], "hostName": h.name,
		"expiresAt":    expiresAt.UTC().Format(time.RFC3339Nano),
		"secretSha256": h.request["secretSha256"],
	}
	raw, err := json.Marshal(approval)
	require.NoError(h.s.t, err)
	status, body := h.s.browse(http.MethodPost, "/orgs/"+h.s.org+"/agent-enrollments", string(raw))
	require.Equal(h.s.t, http.StatusCreated, status, body)
	var e struct {
		ID string `json:"id"`
	}
	require.NoError(h.s.t, json.Unmarshal([]byte(body), &e))
	return e.ID
}

// enroll runs `garam standalone enroll` against the stack.
func (h *host) enroll() (string, error) {
	h.s.t.Helper()
	return h.s.cli("standalone", "enroll", "--dir", h.supervisor, "--agent-dir", h.agent,
		"--machine-url", h.s.machineURL, "--server-root-file", h.s.rootFile)
}

// enrolled is what the enrollment recorded on the host.
func (h *host) enrolled() (grn, supervisor, epoch string) {
	h.s.t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.supervisor, "enrollment.json"))
	require.NoError(h.s.t, err)
	var e struct {
		Grn           string `json:"grn"`
		SupervisorGrn string `json:"supervisorGrn"`
		Epoch         string `json:"epoch"`
	}
	require.NoError(h.s.t, json.Unmarshal(raw, &e))
	return e.Grn, e.SupervisorGrn, e.Epoch
}

// client is a machine-listener client pinning the stack's root and
// presenting token, where one is given, on every request that names no
// Authorization of its own.
func (h *host) client(token string) *http.Client {
	h.s.t.Helper()
	rootPEM, err := os.ReadFile(h.s.rootFile)
	require.NoError(h.s.t, err)
	// A claim waits on garam for a message, so the bound is past its
	// longest wait.
	var transport http.RoundTripper = &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: pool(rootPEM), MinVersion: tls.VersionTLS13,
	}}
	if token != "" {
		transport = presenting{token: token, next: transport}
	}
	return &http.Client{Timeout: 2 * time.Minute, Transport: transport}
}

// presenting sets Authorization to a token on a request naming none.
type presenting struct {
	token string
	next  http.RoundTripper
}

func (p presenting) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Header.Get("Authorization") != "" {
		return p.next.RoundTrip(r)
	}
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Garam-Token "+p.token)
	return p.next.RoundTrip(r)
}

// supervisorToken exchanges the credential the enrollment installed for
// the supervisor's token, as the supervisor does.
func (h *host) supervisorToken() string {
	h.s.t.Helper()
	credential, err := os.ReadFile(filepath.Join(h.supervisor, "credential"))
	require.NoError(h.s.t, err)
	status, raw := h.call(h.client(""), "/tokens", operator.TokenContractVersion,
		"Garam-Credential "+strings.TrimSpace(string(credential)), map[string]string{})
	require.Equal(h.s.t, http.StatusCreated, status, string(raw))
	var token struct {
		Token string `json:"token"`
	}
	require.NoError(h.s.t, json.Unmarshal(raw, &token))
	return token.Token
}

func (h *host) supervisorClient() *http.Client {
	return h.client(h.supervisorToken())
}

// agentToken is the agent's token for garam, minted by its supervisor
// as the supervisor mints the one it hands the adapter.
func (h *host) agentToken() string {
	h.s.t.Helper()
	agentGRN, _, _ := h.enrolled()
	status, raw := h.call(h.supervisorClient(), "/agents/"+agentGRN+"/tokens", operator.TokenContractVersion, "", map[string]string{})
	require.Equal(h.s.t, http.StatusCreated, status, string(raw))
	var token struct {
		Token string `json:"token"`
	}
	require.NoError(h.s.t, json.Unmarshal(raw, &token))
	return token.Token
}

func (h *host) agentClient() *http.Client {
	return h.client(h.agentToken())
}

// call makes one machine-listener request and answers its status and
// body.
func (h *host) call(client *http.Client, path, contract, authorization string, body any) (int, []byte) {
	h.s.t.Helper()
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(h.s.t, err)
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(http.MethodPost, h.s.machineURL+path, payload)
	require.NoError(h.s.t, err)
	req.Header.Set("Content-Type", "application/json")
	if contract != "" {
		req.Header.Set("Garam-Contract-Version", contract)
	}
	// An execution token travels in a header of its own beside the
	// agent's token (ADR-0100); any other credential is Authorization.
	if execution, ok := strings.CutPrefix(authorization, "Garam-Execution "); ok {
		req.Header.Set("Garam-Execution", execution)
	} else if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := client.Do(req)
	require.NoError(h.s.t, err)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(h.s.t, err)
	return resp.StatusCode, raw
}

// sender is the second agent the joint scenario's message comes from.
// It is the test's own: enrolled like any standalone agent, but no
// runtime runs for it, and the test activates it with its supervisor's
// token under a generation the test makes up, so its own calls carry
// an execution token beside its agent token. Nothing here is the
// supervisor's behaviour.
type sender struct {
	*host
	grn    string
	token  string
	client *http.Client
}

func (s *stack) newSender() *sender {
	s.t.Helper()
	h := s.newHost("sender")
	h.prepare()
	h.approve(time.Now().Add(time.Hour))
	out, err := h.enroll()
	require.NoError(s.t, err, out)
	agentGRN, _, epoch := h.enrolled()
	own := h.agentToken()
	status, body := h.call(h.supervisorClient(), "/agents/"+agentGRN+"/activations", "execution-fence.v2", "", map[string]any{
		"requestId": "garam-1180-sender", "epoch": epoch, "generation": hex32(),
		"replacesActivationId": nil, "operationRef": nil, "token": own,
	})
	require.Equal(s.t, http.StatusCreated, status, string(body))
	var a struct {
		Token string `json:"token"`
	}
	require.NoError(s.t, json.Unmarshal(body, &a))
	return &sender{host: h, grn: agentGRN, token: a.Token, client: h.client(own)}
}

func hex32() string { return fmt.Sprintf("%x", randomBytes(16)) }

// post sends body to recipient as the sender.
func (p *sender) post(recipient, key, body string) {
	p.s.t.Helper()
	status, raw := p.call(p.client, "/messages", "", "Garam-Execution "+p.token, map[string]string{
		"recipient": recipient, "idempotencyKey": key, "body": body,
	})
	require.Equal(p.s.t, http.StatusCreated, status, string(raw))
}

// received is one message the sender claimed from its own queue.
type received struct {
	ID     string
	Sender string
	Body   string
}

// receive claims the next message addressed to the sender and completes
// it, or answers nil where none arrived within one claim's wait.
func (p *sender) receive() *received {
	p.s.t.Helper()
	status, raw := p.call(p.client, "/agents/"+p.grn+"/claims", "", "Garam-Execution "+p.token, nil)
	if status == http.StatusNoContent {
		return nil
	}
	require.Equal(p.s.t, http.StatusOK, status, string(raw))
	var c struct {
		ID      string `json:"id"`
		Message struct {
			ID     string `json:"id"`
			Sender string `json:"sender"`
			Body   string `json:"body"`
		} `json:"message"`
	}
	require.NoError(p.s.t, json.Unmarshal(raw, &c))
	status, raw = p.call(p.client, "/agents/"+p.grn+"/claims/"+c.ID+"/completion", "", "Garam-Execution "+p.token, nil)
	require.Contains(p.s.t, []int{http.StatusOK, http.StatusNoContent}, status, string(raw))
	return &received{ID: c.Message.ID, Sender: c.Message.Sender, Body: c.Message.Body}
}

// awaitReplyWithin claims until a message from from arrives, for up to
// within, and fails with why where none does.
func (p *sender) awaitReplyWithin(from string, within time.Duration, why string) *received {
	p.s.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if r := p.receive(); r != nil && r.Sender == from {
			return r
		} else if r != nil {
			p.s.t.Logf("the sender received a message from %s, not the agent", r.Sender)
		}
	}
	p.s.t.Fatalf("no reply from %s within %s: %s", from, within, why)
	return nil
}

// trimmed is s with its surrounding whitespace removed, for comparing
// a model's reply.
func trimmed(s string) string { return strings.TrimSpace(s) }
