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
	"strconv"
	"strings"

	"github.com/garamsh/garam/internal/errorx"
)

// maxEventBytes bounds what one event on the platform's stream may cost
// this reader. Past it the event is truncated, which the reader that
// parses it drops: the events this loop acts on are a kind and a list
// of identifiers, and the ones that are large are the agent talking.
const maxEventBytes = 1 << 20

// HTTPGateway is the agent platform's gateway over loopback: the
// ingress that takes a message and the event stream a turn's end
// arrives on.
//
// The identifier the ingress answers with is the only pairing there is.
// A caller cannot supply one, so a redelivered message arrives at the
// platform as a new message and nothing there collapses the two.
type HTTPGateway struct {
	client   *http.Client
	messages *url.URL
	events   *url.URL
}

// NewHTTPGateway constructs the gateway client. The agent identifier is
// the one the gateway itself serves, which is not garam's GRN for the
// same agent: the gateway serves exactly one agent and answers 404 for
// any other identifier, so this is a value of the platform's own.
func NewHTTPGateway(client *http.Client, base, agent string) (*HTTPGateway, error) {
	if client == nil {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: gateway client is required")
	}
	if agent == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: gateway agent identifier is required")
	}
	root, err := url.Parse(base)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: gateway address", errorx.KindInvalidArgument)
	}
	if !root.IsAbs() || root.Host == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: gateway address must be an absolute URL")
	}
	return &HTTPGateway{
		client:   client,
		messages: root.JoinPath("agents", agent, "messages"),
		events:   root.JoinPath("agents", agent, "events"),
	}, nil
}

// ingress is what the gateway takes: the message content and nothing
// about who is sending, which the connection says.
type ingress struct {
	Content string `json:"content"`
}

// accepted is what the gateway answers a taken message with. The
// identifier is minted there.
type accepted struct {
	ID string `json:"id"`
}

// Post hands one message to the agent. A 202 means it entered a
// volatile buffer rather than that it will be processed, which is why
// nothing is completed here.
func (g *HTTPGateway) Post(ctx context.Context, body string) (GatewayID, error) {
	payload, err := json.Marshal(ingress{Content: body})
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
	if resp.StatusCode != http.StatusAccepted {
		return "", refused(resp)
	}
	var answer accepted
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBody)).Decode(&answer); err != nil {
		return "", errorx.Wrap(err, "delivery: gateway answer", errorx.KindInternal)
	}
	if answer.ID == "" {
		return "", errorx.New(errorx.KindInternal, "delivery: gateway accepted a message without naming it")
	}
	return GatewayID(answer.ID), nil
}

// Subscribe opens one stream of the agent's events.
func (g *HTTPGateway) Subscribe(ctx context.Context) (TurnStream, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.events.String(), nil)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: gateway request", errorx.KindInvalidArgument)
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: gateway call", errorx.KindUnavailable)
	}
	if resp.StatusCode != http.StatusOK {
		defer func() { _ = resp.Body.Close() }()
		return nil, refused(resp)
	}
	return &eventStream{body: resp.Body, lines: bufio.NewReader(resp.Body)}, nil
}

// refused renders a gateway answer this loop cannot use. Every one of
// them is transient as far as the loop is concerned: nothing was
// delivered, so the claim is left to lapse and the message arrives
// again.
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
		messages := make([]GatewayID, 0, len(payload.MessageIDs))
		for _, id := range payload.MessageIDs {
			messages = append(messages, GatewayID(id))
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
