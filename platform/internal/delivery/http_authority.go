package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
)

// contractHeader names the contract a request and its answer are made
// under, on garam's routes and on the lifecycle authority's alike.
const contractHeader = "Garam-Contract-Version"

// placementHeader carries the placement token under
// agent-execution.v2, where the control token takes Authorization
// (ADR-0100).
const placementHeader = "Garam-Placement"

// HTTPAuthority is the lifecycle authority's two routes over HTTPS
// (ADR-0084). Who the caller is, is what the credential presents: the
// control token read from its file before every call (ADR-0100). The
// placement token rides activation alone, and garam's execution token
// and garam's agent token never go here.
type HTTPAuthority struct {
	client      *http.Client
	credential  Credential
	contract    string
	activations *url.URL
	status      *url.URL
}

// NewHTTPAuthority constructs the client against the authority's base
// address, for the one agent this adapter runs beside.
func NewHTTPAuthority(client *http.Client, base string, agent grn.GRN, credential Credential) (*HTTPAuthority, error) {
	if client == nil || credential == nil {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: control client and credential are required")
	}
	if err := agent.Validate(); err != nil {
		return nil, errorx.Wrap(err, "delivery: agent", errorx.KindInvalidArgument)
	}
	root, err := url.Parse(base)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: control address", errorx.KindInvalidArgument)
	}
	if root.Scheme != "https" || root.Host == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: control address must be an absolute https URL")
	}
	served := root.JoinPath("v1", "agents", agent.String())
	return &HTTPAuthority{
		client:      client,
		credential:  credential,
		contract:    TokenExecutionContract,
		activations: served.JoinPath("activations"),
		status:      served.JoinPath("runtime-status"),
	}, nil
}

// Contract answers the contract the client calls under.
func (a *HTTPAuthority) Contract() string { return a.contract }

// activationRequest is the activation body ADR-0084 defines.
type activationRequest struct {
	RequestID      string `json:"requestId"`
	Epoch          string `json:"epoch"`
	Generation     string `json:"generation"`
	ConfigRevision string `json:"configRevision"`
}

// activationAnswer is what the authority answers an activation with.
type activationAnswer struct {
	ActivationID   string `json:"activationId"`
	TokenVersion   int    `json:"tokenVersion"`
	Token          string `json:"token"`
	GRN            string `json:"grn"`
	Epoch          string `json:"epoch"`
	Generation     string `json:"generation"`
	ConfigRevision string `json:"configRevision"`
}

// Activate asks for the binding's activation. An answer that binds
// anything else than what was asked is refused with its token unused.
func (a *HTTPAuthority) Activate(ctx context.Context, requestID string, b Binding, placementToken string) (*Activation, error) {
	payload, err := json.Marshal(activationRequest{
		RequestID: requestID, Epoch: b.Epoch, Generation: b.Generation, ConfigRevision: b.Revision,
	})
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: activation request", errorx.KindInternal)
	}
	resp, err := a.post(ctx, a.activations, payload, placementToken)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, controlRefused(resp)
	}
	var answer activationAnswer
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBody)).Decode(&answer); err != nil {
		return nil, errorx.Wrap(err, "delivery: activation answer", errorx.KindFailedPrecondition)
	}
	if answer.GRN != b.Agent.String() || answer.Epoch != b.Epoch ||
		answer.Generation != b.Generation || answer.ConfigRevision != b.Revision {
		return nil, errorx.New(errorx.KindFailedPrecondition, "delivery: control answered an activation of another binding")
	}
	if answer.ActivationID == "" || answer.Token == "" || answer.TokenVersion < 1 {
		return nil, errorx.New(errorx.KindFailedPrecondition, "delivery: control answered an incomplete activation")
	}
	return &Activation{ID: answer.ActivationID, TokenVersion: answer.TokenVersion, Token: Token(answer.Token), Binding: b}, nil
}

// statusReport is the runtime-status body ADR-0084 defines.
type statusReport struct {
	ActivationID   string    `json:"activationId"`
	GRN            string    `json:"grn"`
	Epoch          string    `json:"epoch"`
	Generation     string    `json:"generation"`
	ConfigRevision string    `json:"configRevision"`
	State          string    `json:"state"`
	StartedAt      time.Time `json:"startedAt"`
	ObservedAt     time.Time `json:"observedAt"`
}

// Report carries the runtime's status as it reported it.
func (a *HTTPAuthority) Report(ctx context.Context, activation string, s *RuntimeStatus, observedAt time.Time) error {
	payload, err := json.Marshal(statusReport{
		ActivationID: activation, GRN: s.Agent.String(), Epoch: s.Epoch, Generation: s.Generation,
		ConfigRevision: s.Revision, State: string(s.State), StartedAt: s.StartedAt.UTC(),
		ObservedAt: observedAt.UTC(),
	})
	if err != nil {
		return errorx.Wrap(err, "delivery: status report", errorx.KindInternal)
	}
	resp, err := a.post(ctx, a.status, payload, "")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusConflict:
		// generation_not_current: control holds a newer generation, and
		// nothing this one reports is accepted again (ADR-0084).
		return errorx.Wrap(ErrGenerationSuperseded, controlRefused(resp).Error())
	}
	return controlRefused(resp)
}

// post makes one call under the contract, carrying the placement token
// where one is given, and refuses an answer that does not name the
// contract before anything else is read from it. A refused control
// token is undecided: its assignee replaces it, and the next call reads
// the file again.
func (a *HTTPAuthority) post(ctx context.Context, target *url.URL, payload []byte, placementToken string) (*http.Response, error) {
	authorization, err := a.credential.Authorization()
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: control request", errorx.KindInternal)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(contractHeader, a.contract)
	req.Header.Set("Authorization", authorization)
	if placementToken != "" {
		req.Header.Set(placementHeader, placementToken)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: control call", errorx.KindUnavailable)
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		defer func() { _ = resp.Body.Close() }()
		return nil, errorx.New(errorx.KindUnavailable, "delivery: control answered "+strconv.Itoa(resp.StatusCode))
	}
	if resp.StatusCode == http.StatusUnauthorized {
		defer func() { _ = resp.Body.Close() }()
		return nil, errorx.Wrap(ErrTokenRefused, controlRefused(resp).Error())
	}
	if resp.Header.Get(contractHeader) != a.contract {
		defer func() { _ = resp.Body.Close() }()
		return nil, errorx.Wrap(ErrUnsupportedPeer, "delivery: control answered "+strconv.Itoa(resp.StatusCode))
	}
	return resp, nil
}

// controlRefused renders a definite refusal with the kind control
// named, which is what the adapter logs it by.
func controlRefused(resp *http.Response) error {
	said := "control refused " + strconv.Itoa(resp.StatusCode)
	var refusal struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBody)).Decode(&refusal); err == nil && refusal.Kind != "" {
		said += " " + refusal.Kind + ": " + refusal.Message
	}
	return errorx.New(errorx.KindFailedPrecondition, "delivery: "+said)
}
