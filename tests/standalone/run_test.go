package standalone_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// mockConfig is a runtime configuration with Sherlock's offline mock
// model, for the cases no model has to answer.
const mockConfig = "revision: r1\nmodel:\n  provider: mock\n"

// running is one `garam standalone run` of the shipped binary.
type running struct {
	h    *host
	cmd  *exec.Cmd
	out  *bytes.Buffer
	done chan struct{}
	err  error
}

// runOptions is what one run is started with beyond the stack's own.
type runOptions struct {
	machineURL string
	config     string
	modelEnv   string
	extra      []string
}

// run starts the host's supervisor with the shipped binary, its
// containers named under the stack's prefix.
func (h *host) run(o runOptions) *running {
	h.s.t.Helper()
	bins, err := binaries()
	require.NoError(h.s.t, err)
	write := func(name, content string) string {
		path := filepath.Join(h.s.dir, h.name+"-"+name)
		require.NoError(h.s.t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}
	if o.machineURL == "" {
		o.machineURL = h.s.machineURL
	}
	if o.config == "" {
		o.config = write("config.yaml", mockConfig)
	}
	if o.modelEnv == "" {
		o.modelEnv = write("model.env", "MOCK_MODEL=unused\n")
	}
	args := append([]string{
		"standalone", "run", "--dir", h.supervisor, "--agent-dir", h.agent,
		"--engine", h.s.daemon, "--machine-url", o.machineURL, "--adapter-machine-url", h.s.bridgeURL,
		"--config", o.config, "--model-env-file", o.modelEnv,
		"--runtime-image", required(h.s.t, "GARAM_TEST_RUNTIME_IMAGE"),
		"--workspace-image", required(h.s.t, "GARAM_TEST_WORKSPACE_IMAGE"),
		"--adapter-image", h.s.garam, "--name-prefix", h.s.prefix + h.name + "-",
	}, o.extra...)
	r := &running{h: h, cmd: exec.Command(bins["garam"], args...), out: &bytes.Buffer{}, done: make(chan struct{})}
	r.cmd.Stdout, r.cmd.Stderr = r.out, r.out
	require.NoError(h.s.t, r.cmd.Start())
	go func() {
		defer close(r.done)
		r.err = r.cmd.Wait()
	}()
	h.s.t.Cleanup(func() { r.stop() })
	return r
}

// stop asks the supervisor to stop, which leaves the workload running,
// and waits for it.
func (r *running) stop() error {
	select {
	case <-r.done:
		return r.err
	default:
	}
	_ = r.cmd.Process.Signal(syscall.SIGTERM)
	<-r.done
	return r.err
}

// db is the stack's database, for reading what garam recorded: one
// connection for the stack, opened on first use.
func (s *stack) db() *pgx.Conn {
	s.t.Helper()
	if s.conn == nil {
		conn, err := pgx.Connect(context.Background(), s.databaseURL)
		require.NoError(s.t, err)
		s.t.Cleanup(func() { _ = conn.Close(context.Background()) })
		s.conn = conn
	}
	return s.conn
}

// activation is one row garam holds for an agent's activations.
type activation struct {
	ID, RequestID, Generation, Ended string
	TokenVersion                     int64
}

func (s *stack) activations(agentGRN string) []activation {
	s.t.Helper()
	rows, err := s.db().Query(context.Background(),
		`SELECT id::text, request_id, generation, token_version, COALESCE(ended_reason, '')
		   FROM agent_activations WHERE agent_grn = $1 ORDER BY created_at`, agentGRN)
	require.NoError(s.t, err)
	var out []activation
	for rows.Next() {
		var a activation
		require.NoError(s.t, rows.Scan(&a.ID, &a.RequestID, &a.Generation, &a.TokenVersion, &a.Ended))
		out = append(out, a)
	}
	require.NoError(s.t, rows.Err())
	return out
}

// record is the supervisor's durable record, as it wrote it.
type record struct {
	LastActivation string `json:"lastActivation"`
	Memory         struct {
		Name   string `json:"name"`
		Nonce  string `json:"nonce"`
		Served bool   `json:"served"`
	} `json:"memory"`
	Intent struct {
		ID           string `json:"id"`
		Epoch        string `json:"epoch"`
		Generation   string `json:"generation"`
		RequestID    string `json:"requestId"`
		Replaces     string `json:"replaces"`
		ActivationID string `json:"activationId"`
	} `json:"intent"`
}

func (h *host) record() record {
	h.s.t.Helper()
	raw, err := os.ReadFile(filepath.Join(h.supervisor, "workload.json"))
	require.NoError(h.s.t, err)
	var r record
	require.NoError(h.s.t, json.Unmarshal(raw, &r))
	return r
}

// interposer stands between a supervisor and garam's machine listener,
// as the test's own fixture: it terminates TLS under a certificate the
// stack's root signed and calls garam with the supervisor's own
// certificate, so garam sees the supervisor's request unchanged. While
// lose is set it forwards an activation and then drops garam's answer,
// as a connection lost after garam committed; while hold is set it
// keeps every activation waiting. Nothing here is the supervisor's
// behaviour.
type interposer struct {
	s        *stack
	url      string
	mu       sync.Mutex
	lose     bool
	held     chan struct{}
	requests []map[string]any
}

func (s *stack) interpose(h *host, certPEM, keyPEM []byte) *interposer {
	s.t.Helper()
	p := &interposer{s: s, held: make(chan struct{})}
	close(p.held)
	upstream := h.supervisorClient()
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(s.t, err)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS13,
	})
	require.NoError(s.t, err)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		activating := strings.HasSuffix(r.URL.Path, "/activations")
		if activating {
			var parsed map[string]any
			_ = json.Unmarshal(body, &parsed)
			p.mu.Lock()
			p.requests = append(p.requests, parsed)
			held := p.held
			p.mu.Unlock()
			<-held
		}
		req, _ := http.NewRequestWithContext(r.Context(), r.Method, s.machineURL+r.URL.Path, bytes.NewReader(body))
		req.Header = r.Header.Clone()
		resp, err := upstream.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		answer, _ := io.ReadAll(resp.Body)
		p.mu.Lock()
		lose := activating && p.lose
		if lose {
			// Garam committed and its answer is lost; what follows waits
			// until the test lets it through.
			p.lose = false
			p.held = make(chan struct{})
		}
		p.mu.Unlock()
		if lose {
			conn, _, err := http.NewResponseController(w).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(answer)
	})}
	go func() { _ = srv.Serve(ln) }()
	s.t.Cleanup(func() { _ = srv.Close() })
	p.url = "https://" + ln.Addr().(*net.TCPAddr).String()
	return p
}

func (p *interposer) hold() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.held = make(chan struct{})
}

func (p *interposer) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	close(p.held)
}

func (p *interposer) asked() []map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]map[string]any(nil), p.requests...)
}
