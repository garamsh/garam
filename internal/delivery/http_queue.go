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

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// maxAnswerBody bounds what is read from an answer this loop only
// inspects — a refusal, and the identifier the gateway mints. Both are
// small, and the bound is what stops an unexpected answer from being
// held in memory whole.
const maxAnswerBody = 4 << 10

// DeliveryContract is the outbox submission contract garam's
// `POST /outbox-messages` serves (ADR-0083). There is no other route an
// outbox reply is offered on.
const DeliveryContract = "durable-delivery.v1"

// HTTPQueue is garam's queue over the machine API (api/machine.yaml):
// the claim, its renewal and its completion, for the one agent this
// adapter runs beside, and the outbox reply it posts for that agent.
//
// Who garam takes the caller to be is what the client certificate on
// the connection names (ADR-0014), so the client passed in is where the
// agent's identity lives, and the GRN here is only the queue this
// adapter reads. Every call presents what the execution holds, and a
// refusal of it is recorded there before it is answered.
type HTTPQueue struct {
	client    *http.Client
	execution *Execution
	claims    *url.URL
	outbox    *url.URL
}

// NewHTTPQueue constructs the queue client against garam's machine
// listener.
//
// The client must not carry a timeout shorter than garam's claim wait:
// a claim is a long poll that answers 204 when nothing arrived, so a
// client timeout under it turns every empty poll into an error. Each
// call carries its own deadline instead, applied by the caller —
// [Config.ClaimTimeout] for the poll and [Config.CallTimeout] for the
// rest — so a peer that accepts and never answers is bounded without
// the poll being cut short.
func NewHTTPQueue(client *http.Client, base string, agent grn.GRN, execution *Execution) (*HTTPQueue, error) {
	if client == nil || execution == nil {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: queue client and execution are required")
	}
	if err := agent.Validate(); err != nil {
		return nil, errorx.Wrap(err, "delivery: agent", errorx.KindInvalidArgument)
	}
	root, err := url.Parse(base)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: garam address", errorx.KindInvalidArgument)
	}
	if !root.IsAbs() || root.Host == "" {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: garam address must be an absolute URL")
	}
	return &HTTPQueue{
		client:    client,
		execution: execution,
		claims:    root.JoinPath("agents", agent.String(), "claims"),
		outbox:    root.JoinPath("outbox-messages"),
	}, nil
}

// Claim waits on garam for a message addressed to the agent. A 409
// here is the execution fence and nothing else: no hold exists yet to
// have lapsed (ADR-0084).
func (q *HTTPQueue) Claim(ctx context.Context) (*Claim, error) {
	resp, presented, err := q.post(ctx, q.claims, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return decodeClaim(resp.Body)
	case http.StatusNoContent:
		return nil, ErrNoMessageReady
	case http.StatusConflict:
		return nil, q.fenced(resp, presented)
	default:
		return nil, q.answered(resp, presented)
	}
}

// Renew carries the hold forward, no further than the ceiling garam
// measures it against.
func (q *HTTPQueue) Renew(ctx context.Context, id ClaimID) (*Claim, error) {
	resp, presented, err := q.post(ctx, q.claims.JoinPath(string(id), "renewal"), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return decodeClaim(resp.Body)
	case http.StatusConflict:
		return nil, ErrClaimLapsed
	default:
		return nil, q.answered(resp, presented)
	}
}

// Complete reports the claimed message delivered.
func (q *HTTPQueue) Complete(ctx context.Context, id ClaimID) error {
	resp, presented, err := q.post(ctx, q.claims.JoinPath(string(id), "completion"), nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNoContent:
		return nil
	case http.StatusConflict:
		return ErrClaimLapsed
	default:
		return q.answered(resp, presented)
	}
}

// outboxSubmission is the body `POST /outbox-messages` takes.
type outboxSubmission struct {
	Recipient      string    `json:"recipient"`
	IdempotencyKey string    `json:"idempotencyKey"`
	Body           string    `json:"body"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Post offers one outbox reply. The answers durable-delivery.v1 pins
// are told apart by status and kind, never by text: an answer outside
// the contract is a server that does not serve the route, 400
// out_of_range a creation instant garam's clock has not reached yet,
// and every other refusal keeps the entry.
func (q *HTTPQueue) Post(ctx context.Context, o Outbound) (*Stored, error) {
	payload, err := json.Marshal(outboxSubmission{
		Recipient: o.Recipient.String(), IdempotencyKey: o.IdempotencyKey, Body: o.Body, CreatedAt: o.CreatedAt.UTC(),
	})
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: outbox submission", errorx.KindInternal)
	}
	resp, presented, err := q.post(ctx, q.outbox, payload)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
		resp.StatusCode >= http.StatusInternalServerError {
		// The fence and an undecided answer are the listener's, whatever
		// contract the route serves.
		return nil, q.answered(resp, presented)
	}
	// A server older than the route answers 404 naming no contract, and
	// one newer names another: neither is offered the entry again.
	if resp.Header.Get(contractHeader) != DeliveryContract {
		return nil, errorx.Wrap(ErrUnsupportedPeer, "delivery: garam answered outside "+DeliveryContract)
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, q.answered(resp, presented)
	}
	var stored machinegen.Message
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxStoredBody)).Decode(&stored); err != nil {
		return nil, errorx.Wrap(err, "delivery: garam answer", errorx.KindUnavailable)
	}
	return &Stored{ID: stored.Id, Sender: stored.Sender, Recipient: stored.Recipient, IdempotencyKey: stored.IdempotencyKey}, nil
}

// maxStoredBody bounds the stored message garam answers a submission
// with, which carries the body it stored: four bytes for each of the
// 65536 characters garam admits, with room for the fields beside it.
const maxStoredBody = 1 << 20

// post makes one call to garam, presenting what the execution holds,
// and answers the token it presented beside the response.
func (q *HTTPQueue) post(ctx context.Context, target *url.URL, payload []byte) (*http.Response, Token, error) {
	presented, err := q.execution.Present()
	if err != nil {
		return nil, "", err
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), body)
	if err != nil {
		return nil, "", errorx.Wrap(err, "delivery: garam request", errorx.KindInvalidArgument)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(contractHeader, DeliveryContract)
	}
	if presented != "" {
		req.Header.Set("Authorization", "Garam-Execution "+string(presented))
	}
	resp, err := q.client.Do(req)
	if err != nil {
		return nil, "", errorx.Wrap(err, "delivery: garam call", errorx.KindUnavailable)
	}
	return resp, presented, nil
}

// decodeClaim reads the hold garam answered with. The spec's shape is
// where it stops being untyped: the loop sees a Claim and never a body.
func decodeClaim(body io.Reader) (*Claim, error) {
	var answer machinegen.Claim
	if err := json.NewDecoder(body).Decode(&answer); err != nil {
		return nil, errorx.Wrap(err, "delivery: garam answer", errorx.KindInternal)
	}
	if answer.Id == "" || answer.Message.Id == "" {
		return nil, errorx.New(errorx.KindInternal, "delivery: garam answered a claim with no identifier")
	}
	sender, err := grn.Parse(answer.Message.Sender)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: garam answered a claim with no sender", errorx.KindInternal)
	}
	return &Claim{
		ID:        ClaimID(answer.Id),
		ExpiresAt: answer.ExpiresAt,
		Message:   MessageID(answer.Message.Id),
		Sender:    sender,
		Body:      answer.Message.Body,
	}, nil
}

// fenced records a refusal of what the call presented, and answers it.
func (q *HTTPQueue) fenced(resp *http.Response, presented Token) error {
	q.execution.Refused(presented)
	text, _ := said(resp)
	return errorx.Wrap(ErrFenced, text)
}

// answered renders a refusal as an error carrying what garam said. A
// refused token or credential is the fence, recorded on the execution;
// a 5xx is undecided; every other refusal carries garam's kind, which
// the claim loop treats as transient and the forwarder sorts. Only
// out_of_range clears with time.
func (q *HTTPQueue) answered(resp *http.Response, presented Token) error {
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return q.fenced(resp, presented)
	}
	text, kind := said(resp)
	switch {
	case resp.StatusCode >= http.StatusInternalServerError:
		return errorx.New(errorx.KindUnavailable, "delivery: "+text)
	case kind == errorx.KindOutOfRange.String():
		return errorx.New(errorx.KindOutOfRange, "delivery: "+text)
	}
	return errorx.New(errorx.KindFailedPrecondition, "delivery: "+text)
}

// said is what garam answered, with its own words where it gave any,
// and the kind it named.
func said(resp *http.Response) (string, string) {
	text := "garam answered " + strconv.Itoa(resp.StatusCode)
	var refusal machinegen.Error
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBody)).Decode(&refusal); err == nil && refusal.Kind != "" {
		text += " " + refusal.Kind + ": " + refusal.Message
	}
	return text, refusal.Kind
}
