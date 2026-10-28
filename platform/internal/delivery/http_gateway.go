package delivery

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
)

// maxEventBytes bounds what one event on the platform's stream may cost
// this reader. Past it the event is truncated, which the reader that
// parses it drops: the events this loop acts on are a kind and a list
// of identifiers, and the ones that are large are the agent talking.
const maxEventBytes = 1 << 20

// HTTPGateway is the agent platform's gateway over loopback: the
// ingress that takes a message, the lookup of one it took, the event
// stream a turn's end arrives on, and the runtime's status.
//
// Every message is posted under garam's identifier for it, which the
// runtime keeps as the caller's own, so a redelivery is recognised
// there rather than run as a new message.
type HTTPGateway struct {
	client   *http.Client
	agent    grn.GRN
	messages *url.URL
	events   *url.URL
	status   *url.URL
}

// NewHTTPGateway constructs the gateway client. The runtime serves the
// agent under the same GRN garam knows it by, and answers 404 for any
// other.
func NewHTTPGateway(client *http.Client, base string, agent grn.GRN) (*HTTPGateway, error) {
	if client == nil {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: gateway client is required")
	}
	if err := agent.Validate(); err != nil {
		return nil, errorx.Wrap(err, "delivery: agent", errorx.KindInvalidArgument)
	}
	root, err := url.Parse(base)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: gateway address", errorx.KindInvalidArgument)
	}
	if !root.IsAbs() || root.Host == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: gateway address must be an absolute URL")
	}
	served := root.JoinPath("agents", agent.String())
	return &HTTPGateway{
		client:   client,
		agent:    agent,
		messages: served.JoinPath("messages"),
		events:   served.JoinPath("events"),
		status:   served.JoinPath("status"),
	}, nil
}

// ingress is what the gateway takes: the caller's identifier for the
// message, its content, and garam's signature over that content when
// there is one to send. The member is absent rather than empty
// otherwise, because a runtime that predates it refuses it however it
// is filled.
type ingress struct {
	ID        string `json:"id"`
	Content   string `json:"content"`
	Signature string `json:"signature,omitempty"`
}

// progress is what the gateway answers a post and a lookup with.
type progress struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// Post hands one message to the agent. A 202 means it entered a
// volatile buffer rather than that it will be processed, which is why
// nothing is completed on it; a 200 is a replay of a message whose turn
// already committed, and runs nothing.
func (g *HTTPGateway) Post(ctx context.Context, id MessageID, content, signature string) (Progress, error) {
	payload, err := json.Marshal(ingress{ID: string(id), Content: content, Signature: signature})
	if err != nil {
		return "", errorx.Wrap(err, "delivery: gateway request", errorx.KindInvalidArgument)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.messages.String(), bytes.NewReader(payload))
	if err != nil {
		return "", errorx.Wrap(err, "delivery: gateway request", errorx.KindInvalidArgument)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return "", errorx.Wrap(err, "delivery: gateway call", errorx.KindUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return "", refused(resp)
	}
	return readProgress(resp.Body, id)
}

// Lookup asks the runtime how far it has got with a message.
func (g *HTTPGateway) Lookup(ctx context.Context, id MessageID) (Progress, error) {
	resp, err := g.get(ctx, g.messages.JoinPath(string(id)), "application/json")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	return readProgress(resp.Body, id)
}

// readProgress reads an answer about one message, which must name the
// message asked about: one naming another is not an answer to it.
func readProgress(body io.Reader, id MessageID) (Progress, error) {
	var answer progress
	if err := json.NewDecoder(io.LimitReader(body, maxAnswerBody)).Decode(&answer); err != nil {
		return "", errorx.Wrap(err, "delivery: gateway answer", errorx.KindInternal)
	}
	if answer.ID != string(id) {
		return "", errorx.New(errorx.KindInternal, "delivery: gateway answered about another message")
	}
	switch answer.Status {
	case "queued", "pending":
		return ProgressPending, nil
	case "completed":
		return ProgressCompleted, nil
	case "unknown":
		return ProgressUnknown, nil
	}
	return "", errorx.New(errorx.KindInternal, "delivery: gateway answered a status it does not define")
}

// runtimeStatusVersion is the status shape this adapter reads. Another
// one is refused rather than read as this one (ADR-0079).
const runtimeStatusVersion = 1

// runtimeStatus is the runtime's status body as it defines it.
type runtimeStatus struct {
	ContractVersion int       `json:"contract_version"`
	AgentID         string    `json:"agent_id"`
	AssignmentEpoch string    `json:"assignment_epoch"`
	Generation      string    `json:"generation"`
	ConfigRevision  string    `json:"config_revision"`
	State           string    `json:"state"`
	StartedAt       time.Time `json:"started_at"`
}

// Status reads which process of the runtime is serving the agent. A
// runtime serving another agent, of another status version, or that
// minted no generation is refused, because nothing can be activated
// for it.
func (g *HTTPGateway) Status(ctx context.Context) (*RuntimeStatus, error) {
	resp, err := g.get(ctx, g.status, "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var answer runtimeStatus
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBody)).Decode(&answer); err != nil {
		return nil, errorx.Wrap(err, "delivery: runtime status", errorx.KindInternal)
	}
	if answer.ContractVersion != runtimeStatusVersion {
		return nil, errorx.New(errorx.KindFailedPrecondition,
			"delivery: runtime status version "+strconv.Itoa(answer.ContractVersion)+" is not supported")
	}
	if answer.AgentID != g.agent.String() {
		return nil, errorx.New(errorx.KindFailedPrecondition, "delivery: the runtime serves another agent")
	}
	if !generationPattern.MatchString(answer.Generation) {
		return nil, errorx.New(errorx.KindFailedPrecondition, "delivery: the runtime reports no generation")
	}
	state := RuntimeState(answer.State)
	if state != RuntimeServing && state != RuntimeDraining {
		return nil, errorx.New(errorx.KindFailedPrecondition, "delivery: the runtime reports a state it does not define")
	}
	return &RuntimeStatus{
		Agent:      g.agent,
		Epoch:      answer.AssignmentEpoch,
		Generation: answer.Generation,
		Revision:   answer.ConfigRevision,
		State:      state,
		StartedAt:  answer.StartedAt,
	}, nil
}

// generationPattern is a runtime start nonce as garam's activation
// takes it.
var generationPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// get makes one read of the gateway and refuses any answer but 200.
func (g *HTTPGateway) get(ctx context.Context, target *url.URL, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: gateway request", errorx.KindInvalidArgument)
	}
	req.Header.Set("Accept", accept)
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: gateway call", errorx.KindUnavailable)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, refused(resp)
	}
	return resp, nil
}

// Subscribe opens one stream of the agent's events.
func (g *HTTPGateway) Subscribe(ctx context.Context) (TurnStream, error) {
	resp, err := g.get(ctx, g.events, "text/event-stream")
	if err != nil {
		return nil, err
	}
	return &eventStream{body: resp.Body, lines: bufio.NewReader(resp.Body)}, nil
}

// refused renders a gateway answer this loop cannot use. Every one of
// them is transient as far as the loop is concerned: nothing was
// delivered, so the claim is left to lapse and the message arrives
// again. A refused signature is one of them. garam signs each claim
// afresh, so the next attempt carries a signature of its own, and a
// verifier that cannot read garam's keys yet may by then.
func refused(resp *http.Response) error {
	said := "gateway answered " + strconv.Itoa(resp.StatusCode)
	var problem struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBody)).Decode(&problem); err == nil && problem.Error != "" {
		said += ": " + problem.Error
	}
	return errorx.New(errorx.KindUnavailable, "delivery: "+said)
}

// eventStream reads server-sent events from one open response.
type eventStream struct {
	body  io.ReadCloser
	lines *bufio.Reader
}

// Next returns the next turn that ended. Everything else on the stream
// is skipped: only a terminal event names the messages a turn drained,
// so nothing else can complete anything.
func (s *eventStream) Next() (TurnEnd, error) {
	for {
		data, err := s.event()
		if err != nil {
			return TurnEnd{}, err
		}
		var payload struct {
			Kind       string   `json:"kind"`
			MessageIDs []string `json:"message_ids"`
		}
		if err := json.Unmarshal(data, &payload); err != nil {
			// The stream is the platform's, and an event this reader
			// cannot parse is dropped rather than ending the
			// subscription, which would cost the completions behind it.
			continue
		}
		outcome := TurnOutcome(payload.Kind)
		if outcome != TurnFinished && outcome != TurnFailed {
			continue
		}
		messages := make([]MessageID, 0, len(payload.MessageIDs))
		for _, id := range payload.MessageIDs {
			messages = append(messages, MessageID(id))
		}
		return TurnEnd{Outcome: outcome, Messages: messages}, nil
	}
}

// Close ends the subscription.
func (s *eventStream) Close() error {
	if err := s.body.Close(); err != nil {
		return errorx.Wrap(err, "delivery: gateway stream", errorx.KindUnavailable)
	}
	return nil
}

// event returns the data of the next event, joining the data lines the
// way server-sent events are read.
func (s *eventStream) event() ([]byte, error) {
	var data []byte
	for {
		line, err := s.line()
		if err != nil {
			return nil, err
		}
		if line == "" {
			if len(data) > 0 {
				return data, nil
			}
			continue
		}
		value, ok := strings.CutPrefix(line, "data:")
		if !ok {
			continue
		}
		if len(data) > 0 {
			data = append(data, '\n')
		}
		data = append(data, strings.TrimPrefix(value, " ")...)
	}
}

// line returns one line of the stream, dropping what is past
// maxEventBytes rather than holding it. A truncated line is not valid
// JSON, so the event it belongs to is dropped by the reader above.
func (s *eventStream) line() (string, error) {
	var line []byte
	for {
		chunk, err := s.lines.ReadSlice('\n')
		if len(line) < maxEventBytes {
			line = append(line, chunk...)
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if err != nil {
			return "", errorx.Wrap(err, "delivery: gateway stream", errorx.KindUnavailable)
		}
		return strings.TrimRight(string(line), "\r\n"), nil
	}
}
