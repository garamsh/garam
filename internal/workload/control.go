package workload

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/garamsh/garam/internal/errorx"
)

// ExecutionContract is the adapter-facing activation and runtime-status
// contract (ADR-0084), which the supervisor serves as the Operator's
// control service does, to an adapter presenting the agent's
// certificate.
const ExecutionContract = "agent-execution.v1"

// TokenExecutionContract is the same contract served in the token
// mode: the control token in Authorization and the placement token in
// a Garam-Placement header of its own (ADR-0100).
const TokenExecutionContract = "agent-execution.v2"

// controlTokenPrefix spells the control token as an agent token, so
// the adapter reads its two token files alike. garam never issued it,
// and refuses it.
const controlTokenPrefix = "garam_at_"

const contractHeader = "Garam-Contract-Version"

// tokenScheme and placementHeader are the token mode's headers
// (ADR-0100).
const (
	tokenScheme     = "Garam-Token"
	placementHeader = "Garam-Placement"
)

// maxBody bounds a request body; both are a handful of short strings.
const maxBody = 64 << 10

var (
	requestIDPattern  = regexp.MustCompile(`^[A-Za-z0-9._~-]{1,128}$`)
	generationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// controlServer is a generation's control and what reports its
// listener stopping on its own.
type controlServer struct {
	srv    *http.Server
	failed chan error
}

// serveControl serves the two routes on the generation's socket, which
// lives in a directory only that generation's adapter mounts. The
// adapter verifies this listener by the generation's own root. In the
// certificate mode it presents the agent's certificate, which must
// chain to the agent's issuer; in the token mode the listener asks for
// no certificate, and the generation's control token authenticates
// each request (ADR-0100).
func (s *Supervisor) serveControl(genDir string, serving *tls.Certificate, in *intent) (*controlServer, error) {
	dir := filepath.Join(genDir, socketDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, errorx.Wrap(err, "workload: socket directory", errorx.KindUnavailable)
	}
	// The adapter runs as an account of its own, which reaches the
	// socket through the directory and connects through the socket:
	// searchable, and writable. Nothing outside the supervisor's own
	// directory reaches either.
	if err := os.Chmod(dir, 0o711); err != nil {
		return nil, errorx.Wrap(err, "workload: socket directory", errorx.KindUnavailable)
	}
	path := filepath.Join(dir, socketName)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, errorx.Wrap(err, "workload: socket", errorx.KindUnavailable)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, errorx.Wrap(err, "workload: socket", errorx.KindUnavailable)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		_ = ln.Close()
		return nil, errorx.Wrap(err, "workload: socket", errorx.KindUnavailable)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/agents/{agent}/activations", func(w http.ResponseWriter, r *http.Request) {
		s.activate(w, r, in)
	})
	mux.HandleFunc("POST /v1/agents/{agent}/runtime-status", func(w http.ResponseWriter, r *http.Request) {
		s.report(w, r, in)
	})
	config := &tls.Config{Certificates: []tls.Certificate{*serving}, MinVersion: tls.VersionTLS13}
	if s.issuer != nil {
		config.ClientAuth, config.ClientCAs = tls.RequireAndVerifyClientCert, s.issuer
	}
	srv := &http.Server{
		Handler:           contract(s.contract(), mux),
		TLSConfig:         config,
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctl := &controlServer{srv: srv, failed: make(chan error, 1)}
	go func() {
		if err := srv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			ctl.failed <- err
		}
	}()
	return ctl, nil
}

// contract answers the contract the supervisor serves: the token
// mode's or the certificate mode's, never both.
func (s *Supervisor) contract() string {
	if s.cfg.Tokens != nil {
		return TokenExecutionContract
	}
	return ExecutionContract
}

// contract refuses a request made under another contract than served,
// and names served on every answer, refusals included.
func contract(served string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(contractHeader, served)
		if r.Header.Get(contractHeader) != served {
			refuse(w, http.StatusBadRequest, "invalid_request", "this route serves "+served)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// refuse answers a refusal as the contract shapes one.
func refuse(w http.ResponseWriter, status int, kind, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}{kind, message})
}

// caller authenticates the adapter, refusing a request for another
// agent. In the token mode it compares the control token presented, in
// constant time, with the generation's and answers no certificate; in
// the certificate mode it answers the certificate the adapter
// presented, refusing one that names another, which the handshake
// already verified against the agent's issuer.
func (s *Supervisor) caller(w http.ResponseWriter, r *http.Request, in *intent) ([]byte, bool) {
	agent := s.cfg.Desired.Agent.String()
	if r.PathValue("agent") != agent {
		refuse(w, http.StatusForbidden, "not_authorized", "this supervisor serves another agent")
		return nil, false
	}
	if s.cfg.Tokens != nil {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), tokenScheme+" ")
		if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(in.ControlToken)) != 1 {
			refuse(w, http.StatusUnauthorized, "unauthenticated", "the generation's control token is required")
			return nil, false
		}
		return nil, true
	}
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		refuse(w, http.StatusUnauthorized, "unauthenticated", "the agent's certificate is required")
		return nil, false
	}
	leaf := r.TLS.PeerCertificates[0]
	if len(leaf.URIs) != 1 || leaf.URIs[0].String() != agent {
		refuse(w, http.StatusForbidden, "not_authorized", "the certificate names another identity")
		return nil, false
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Raw}), true
}

// decode reads a body strictly: a member the contract does not define
// is refused rather than ignored.
func decode(r *http.Request, into any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

type activationBody struct {
	RequestID      string `json:"requestId"`
	Epoch          string `json:"epoch"`
	Generation     string `json:"generation"`
	ConfigRevision string `json:"configRevision"`
}

type activationAnswer struct {
	ActivationID   string `json:"activationId"`
	TokenVersion   int    `json:"tokenVersion"`
	Token          string `json:"token"`
	GRN            string `json:"grn"`
	Epoch          string `json:"epoch"`
	Generation     string `json:"generation"`
	ConfigRevision string `json:"configRevision"`
}

// activate forwards an activation to garam for the generation this
// intent's runtime is, and for nothing else: the placement token is
// the intent's, the epoch and revision are the ones it started the
// runtime with, the runtime is still the container it started with no
// restart since, and the generation was never asked for before. A
// retry repeats the first request exactly, which garam answers with
// the same activation and a newer token.
func (s *Supervisor) activate(w http.ResponseWriter, r *http.Request, in *intent) {
	leafPEM, ok := s.caller(w, r, in)
	if !ok {
		return
	}
	token, ok := s.placementOf(r)
	if !ok || token == "" {
		refuse(w, http.StatusUnauthorized, "unauthenticated", "the placement token is required")
		return
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(in.PlacementToken)) != 1 {
		refuse(w, http.StatusForbidden, "placement_not_current", "the placement token is not this generation's")
		return
	}
	var body activationBody
	if err := decode(r, &body); err != nil || !requestIDPattern.MatchString(body.RequestID) ||
		!generationPattern.MatchString(body.Generation) || body.Epoch == "" || body.ConfigRevision == "" {
		refuse(w, http.StatusBadRequest, "invalid_request", "requestId, epoch, a 32-hex generation and configRevision are required")
		return
	}
	if body.Epoch != in.Epoch {
		refuse(w, http.StatusConflict, "epoch_superseded", "the runtime reports another epoch")
		return
	}
	if body.ConfigRevision != in.Revision {
		refuse(w, http.StatusForbidden, "placement_not_current", "the runtime reports another revision")
		return
	}

	s.activating.Lock()
	defer s.activating.Unlock()
	if err := s.provenance(r.Context(), in); err != nil {
		if errorx.IsKind(err, errorx.KindUnavailable) {
			refuse(w, http.StatusServiceUnavailable, "undecided", "the runtime could not be read")
			return
		}
		refuse(w, http.StatusForbidden, "placement_not_current", "the runtime is not the one this generation started")
		return
	}
	req, refusal := s.claim(in, body, leafPEM)
	if refusal != nil {
		refusal(w)
		return
	}
	if s.cfg.Tokens != nil {
		agentToken, err := s.cfg.Tokens.Current(r.Context())
		if err != nil {
			slog.Warn("no agent token to forward", slog.String("intent", in.ID), slog.String("error", err.Error()))
			refuse(w, http.StatusServiceUnavailable, "undecided", "garam did not answer the agent token")
			return
		}
		req.Token = agentToken
	}
	a, err := s.garam.Activate(r.Context(), req)
	if err != nil {
		slog.Warn("garam did not activate", slog.String("intent", in.ID),
			slog.String("generation", req.Generation), slog.String("error", err.Error()))
		switch errorx.KindOf(err) {
		case errorx.KindUnavailable, errorx.KindUnauthenticated:
			// garam's answer is unknown, or it refused the supervisor's
			// own certificate or token, which a renewal or an exchange
			// can mend: either way the adapter retries the same request.
			refuse(w, http.StatusServiceUnavailable, "undecided", "garam did not answer the activation")
		case errorx.KindFailedPrecondition:
			refuse(w, http.StatusConflict, "activation_superseded", "garam refused the activation as superseded")
		default:
			refuse(w, http.StatusForbidden, "not_authorized", "garam refused the activation")
		}
		return
	}
	agent := s.cfg.Desired.Agent.String()
	if a.GRN != agent || a.Epoch != req.Epoch || a.Generation != req.Generation ||
		a.ID == "" || a.Token == "" || a.TokenVersion < 1 {
		slog.Error("garam answered an activation of another binding", slog.String("intent", in.ID))
		refuse(w, http.StatusServiceUnavailable, "undecided", "garam's answer did not bind this generation")
		return
	}
	if refusal := s.settle(in, a); refusal != nil {
		refusal(w)
		return
	}
	status := http.StatusCreated
	if a.Replayed {
		status = http.StatusOK
	}
	slog.Info("generation activated", slog.String("intent", in.ID), slog.String("generation", a.Generation),
		slog.String("activation", a.ID), slog.Int("token_version", a.TokenVersion))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(activationAnswer{
		ActivationID: a.ID, TokenVersion: a.TokenVersion, Token: a.Token,
		GRN: agent, Epoch: a.Epoch, Generation: a.Generation, ConfigRevision: in.Revision,
	})
}

// placementOf answers the placement token the request carries: in
// Authorization under the certificate mode, and in a Garam-Placement
// header of its own under the token mode, where the control token
// takes Authorization (ADR-0100).
func (s *Supervisor) placementOf(r *http.Request) (string, bool) {
	if s.cfg.Tokens != nil {
		token := r.Header.Get(placementHeader)
		return token, token != ""
	}
	return strings.CutPrefix(r.Header.Get("Authorization"), placementHeader+" ")
}

// provenance refuses a runtime that is not the container this intent
// started, still running from the same start with no restart since.
func (s *Supervisor) provenance(ctx context.Context, in *intent) error {
	s.mu.Lock()
	rec := in.Runtime
	live := in.live()
	s.mu.Unlock()
	if !live {
		return errorx.New(errorx.KindFailedPrecondition, "workload: the generation is not running")
	}
	if rec.ID == "" || rec.StartedAt.IsZero() {
		return errorx.New(errorx.KindUnavailable, "workload: the runtime's start is not recorded yet")
	}
	c, err := s.engine.InspectContainer(ctx, rec.ID)
	if errors.Is(err, ErrAbsent) {
		return errorx.New(errorx.KindFailedPrecondition, "workload: the runtime is gone")
	}
	if err != nil {
		return errorx.Wrap(err, "workload: runtime", errorx.KindUnavailable)
	}
	if c.ID != rec.ID || c.Status != ContainerRunning || !c.StartedAt.Equal(rec.StartedAt) || c.RestartCount != 0 {
		return errorx.New(errorx.KindFailedPrecondition, "workload: the runtime restarted or stopped")
	}
	return nil
}

// claim records the generation an activation is first asked for, and
// answers the request to send garam: the first one, or a retry of it.
func (s *Supervisor) claim(in *intent, body activationBody, leafPEM []byte) (ActivationRequest, func(http.ResponseWriter)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case in.Generation == "":
		if s.st.seen(body.Generation) {
			return ActivationRequest{}, func(w http.ResponseWriter) {
				refuse(w, http.StatusForbidden, "placement_not_current", "the generation was activated before")
			}
		}
		in.Generation, in.RequestID, in.Replaces = body.Generation, body.RequestID, s.st.LastActivation
		s.st.Generations = append(s.st.Generations, body.Generation)
		// The adapter asks only for a runtime that reports serving, so
		// this is the runtime on a moved store having accepted it
		// under --require-store.
		if mv := s.st.Move; mv.awaiting() && mv.Intent == in.ID {
			mv.Outcome = moveAccepted
		}
		if err := s.save(); err != nil {
			return ActivationRequest{}, undecided(err)
		}
	case in.Generation != body.Generation:
		return ActivationRequest{}, func(w http.ResponseWriter) {
			refuse(w, http.StatusForbidden, "placement_not_current", "the runtime reports another generation")
		}
	case in.RequestID != body.RequestID:
		return ActivationRequest{}, func(w http.ResponseWriter) {
			refuse(w, http.StatusConflict, "request_reused", "this generation was asked for under another request")
		}
	}
	req := ActivationRequest{
		RequestID: in.RequestID, Epoch: in.Epoch, Generation: in.Generation, Replaces: in.Replaces,
	}
	if leafPEM != nil {
		req.CertificatePEM = string(leafPEM)
	}
	return req, nil
}

// settle records the activation garam answered as the agent's most
// recent one.
func (s *Supervisor) settle(in *intent, a *Activation) func(http.ResponseWriter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ActivationID != "" && in.ActivationID != a.ID {
		return func(w http.ResponseWriter) {
			refuse(w, http.StatusConflict, "activation_superseded", "garam answered another activation for this request")
		}
	}
	if in.ActivationID == a.ID {
		return nil
	}
	in.ActivationID, s.st.LastActivation = a.ID, a.ID
	if err := s.save(); err != nil {
		return undecided(err)
	}
	return nil
}

func undecided(err error) func(http.ResponseWriter) {
	slog.Error("workload record not written", slog.String("error", err.Error()))
	return func(w http.ResponseWriter) {
		refuse(w, http.StatusServiceUnavailable, "undecided", "the supervisor could not record the activation")
	}
}

type statusBody struct {
	ActivationID   string    `json:"activationId"`
	GRN            string    `json:"grn"`
	Epoch          string    `json:"epoch"`
	Generation     string    `json:"generation"`
	ConfigRevision string    `json:"configRevision"`
	State          string    `json:"state"`
	StartedAt      time.Time `json:"startedAt"`
	ObservedAt     time.Time `json:"observedAt"`
}

// report takes the runtime's status for the activated generation. It
// activates nothing; it is the standalone host's record of what the
// runtime applied.
func (s *Supervisor) report(w http.ResponseWriter, r *http.Request, in *intent) {
	if _, ok := s.caller(w, r, in); !ok {
		return
	}
	var body statusBody
	if err := decode(r, &body); err != nil || body.GRN != s.cfg.Desired.Agent.String() ||
		(body.State != "serving" && body.State != "draining") {
		refuse(w, http.StatusBadRequest, "invalid_request", "a runtime status of this agent, serving or draining, is required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !in.live() || in.ActivationID == "" || body.ActivationID != in.ActivationID ||
		body.Generation != in.Generation || body.Epoch != in.Epoch || body.ConfigRevision != in.Revision {
		refuse(w, http.StatusConflict, "generation_not_current", "the status is not of the activated generation")
		return
	}
	// The first serving report on this volume is what says its store
	// exists: from here on every start of it requires the store.
	if body.State == "serving" && !s.st.Memory.Served {
		s.st.Memory.Served = true
		if err := s.save(); err != nil {
			undecided(err)(w)
			return
		}
	}
	slog.Info("runtime status", slog.String("intent", in.ID), slog.String("generation", body.Generation),
		slog.String("revision", body.ConfigRevision), slog.String("state", body.State))
	w.WriteHeader(http.StatusNoContent)
}
