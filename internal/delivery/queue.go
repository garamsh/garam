package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// maxAnswerBody bounds what is read from an answer this loop only
// inspects — a refusal, and the identifier the gateway mints. Both are
// small, and the bound is what stops an unexpected answer from being
// held in memory whole.
const maxAnswerBody = 4 << 10

// HTTPQueue is garam's queue over the machine API (api/machine.yaml):
// the claim, its renewal and its completion, for the one agent this
// adapter runs beside.
//
// It holds no credential. Who garam takes the caller to be is what the
// client certificate on the connection names (ADR-0014), so the client
// passed in is where the agent's identity lives, and the GRN here is
// only the queue this adapter reads.
type HTTPQueue struct {
	client *http.Client
	claims *url.URL
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
func NewHTTPQueue(client *http.Client, base string, agent grn.GRN) (*HTTPQueue, error) {
	if client == nil {
		return nil, errorx.New(errorx.KindInvalidArgument, "delivery: queue client is required")
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
	return &HTTPQueue{client: client, claims: root.JoinPath("agents", agent.String(), "claims")}, nil
}

// Claim waits on garam for a message addressed to the agent.
func (q *HTTPQueue) Claim(ctx context.Context) (*Claim, error) {
	resp, err := q.post(ctx, q.claims)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return decodeClaim(resp.Body)
	case http.StatusNoContent:
		return nil, ErrNoMessageReady
	default:
		return nil, answered(resp)
	}
}

// Renew carries the hold forward, no further than the ceiling garam
// measures it against.
func (q *HTTPQueue) Renew(ctx context.Context, id ClaimID) (*Claim, error) {
	resp, err := q.post(ctx, q.claims.JoinPath(string(id), "renewal"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, answered(resp)
	}
	return decodeClaim(resp.Body)
}

// Complete reports the claimed message delivered.
func (q *HTTPQueue) Complete(ctx context.Context, id ClaimID) error {
	resp, err := q.post(ctx, q.claims.JoinPath(string(id), "completion"))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return answered(resp)
	}
	return nil
}

// post makes one call to garam. Every operation here is a POST with no
// body: what a caller may do is decided by the certificate and the
// path, and none of the three carries a payload.
func (q *HTTPQueue) post(ctx context.Context, target *url.URL) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), nil)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: garam request", errorx.KindInvalidArgument)
	}
	resp, err := q.client.Do(req)
	if err != nil {
		return nil, errorx.Wrap(err, "delivery: garam call", errorx.KindUnavailable)
	}
	return resp, nil
}

// decodeClaim reads the hold garam answered with. The spec's shape is
// where it stops being untyped: the loop sees a Claim and never a body.
func decodeClaim(body io.Reader) (*Claim, error) {
	var answer machinegen.Claim
	if err := json.NewDecoder(body).Decode(&answer); err != nil {
		return nil, errorx.Wrap(err, "delivery: garam answer", errorx.KindInternal)
	}
	if answer.Id == "" {
		return nil, errorx.New(errorx.KindInternal, "delivery: garam answered a claim with no identifier")
	}
	return &Claim{ID: ClaimID(answer.Id), ExpiresAt: answer.ExpiresAt, Body: answer.Message.Body}, nil
}

// answered renders a refusal as an error carrying what garam said. A
// hold that is gone gets the sentinel the loop acts on; every other
// refusal is transient as far as the loop is concerned — it retries,
// and the line it logs carries garam's own words.
func answered(resp *http.Response) error {
	if resp.StatusCode == http.StatusConflict {
		return ErrClaimLapsed
	}
	said := "garam answered " + strconv.Itoa(resp.StatusCode)
	var refusal machinegen.Error
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxAnswerBody)).Decode(&refusal); err == nil && refusal.Kind != "" {
		said += " " + refusal.Kind + ": " + refusal.Message
	}
	return errorx.New(errorx.KindUnavailable, "delivery: "+said)
}
