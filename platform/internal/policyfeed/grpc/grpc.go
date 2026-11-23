// Package grpc sends the policy feed to the policy service over its
// gRPC API (ADR-0140 §7), each call carrying a token the platform
// signs (ADR-0138).
package grpc

import (
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/policyfeed"
	"github.com/garamsh/garam/platform/internal/signer"
)

const (
	// CallTimeout is the deadline of one ApplyEntityChanges call.
	CallTimeout = 30 * time.Second
	// TokenLifetime is how long the token of one call lives. It is
	// short because a token is signed for each call.
	TokenLifetime = 60 * time.Second

	// Subject and Audience are the token profile's fixed claims
	// (ADR-0138 item 2).
	Subject  = "platform"
	Audience = "policy"
)

// keepaliveParams is what keeps a connection to a lost Node from
// waiting for the operating system: a blackholed TCP connection fails
// after about Time plus Timeout, 13 s, and the feed's connection, idle
// between passes, notices at all only because PermitWithoutStream is
// set (ADR-0178 §The policy-service client after a lost Node, item 1).
// Time is grpc-go's minimum: a smaller one is raised to it, and a zero
// one is how dial leaves the keepalive out, as the control of the test
// that shows what it buys does.
var keepaliveParams = keepalive.ClientParameters{
	Time:                10 * time.Second,
	Timeout:             3 * time.Second,
	PermitWithoutStream: true,
}

// retriedMethods are the read-only decision calls. Only these are
// attempted twice on UNAVAILABLE: a write or a mutating relay call is
// attempted once, since a call that reached a lost Node may have been
// applied there, and the feed is idempotent by sequence and not by
// retry (ADR-0178 item 3).
var retriedMethods = []string{
	policyv1.PolicyService_Evaluate_FullMethodName,
	policyv1.PolicyService_Evaluations_FullMethodName,
}

// serviceConfig balances over every address the target resolves to and
// retries retriedMethods once on UNAVAILABLE (2 attempts in all). The
// deadlines are the callers' and are not set here.
func serviceConfig() string {
	names := make([]string, len(retriedMethods))
	for i, full := range retriedMethods {
		service, method, _ := strings.Cut(strings.TrimPrefix(full, "/"), "/")
		names[i] = fmt.Sprintf(`{"service":%q,"method":%q}`, service, method)
	}
	return `{"loadBalancingConfig":[{"round_robin":{}}],"methodConfig":[{"name":[` +
		strings.Join(names, ",") +
		`],"retryPolicy":{"maxAttempts":2,"initialBackoff":"0.1s","maxBackoff":"1s",` +
		`"backoffMultiplier":2,"retryableStatusCodes":["UNAVAILABLE"]}}]}`
}

// target is addr resolved through DNS, which returns every pod's
// address of a headless Service, unless addr names a resolver already.
// With the default `passthrough` the whole name would be one address
// and nothing would balance.
func target(addr string) string {
	if strings.Contains(addr, "://") {
		return addr
	}
	return "dns:///" + addr
}

// Sender implements policyfeed.Sender over a policy service client.
type Sender struct {
	client  policyv1.PolicyServiceClient
	timeout time.Duration
}

// Compile-time check that Sender satisfies policyfeed.Sender.
var _ policyfeed.Sender = (*Sender)(nil)

// NewSender constructs a sender over client.
func NewSender(client policyv1.PolicyServiceClient) *Sender {
	return &Sender{client: client, timeout: CallTimeout}
}

// Send applies entries at sequence in one call.
func (s *Sender) Send(
	ctx context.Context, sequence int64, entries []policyfeed.Entry,
) ([]policyfeed.Refusal, error) {
	changes := make([]*policyv1.EntityChange, len(entries))
	for i, e := range entries {
		changes[i] = change(e, sequence)
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	resp, err := s.client.ApplyEntityChanges(ctx,
		&policyv1.ApplyEntityChangesRequest{Changes: changes})
	if err != nil {
		return nil, wrapCall(err, "ApplyEntityChanges")
	}
	refused := make([]policyfeed.Refusal, 0, len(resp.GetRefusals()))
	for _, r := range resp.GetRefusals() {
		if int(r.GetIndex()) >= len(entries) {
			return nil, errorx.New(errorx.KindInternal,
				fmt.Sprintf("policy service refused change %d of %d", r.GetIndex(), len(entries)))
		}
		refused = append(refused, policyfeed.Refusal{
			Entity: entries[r.GetIndex()].Entity,
			Reason: r.GetError(),
		})
	}
	return refused, nil
}

func change(e policyfeed.Entry, sequence int64) *policyv1.EntityChange {
	c := &policyv1.EntityChange{Entity: ref(e.Entity), Sequence: sequence}
	if e.State == nil {
		c.Operation = &policyv1.EntityChange_Delete{Delete: &policyv1.EntityDelete{}}
		return c
	}
	upsert := &policyv1.EntityUpsert{Parents: make([]*policyv1.EntityRef, len(e.State.Parents))}
	if e.State.Org != nil {
		upsert.Org = ref(*e.State.Org)
	}
	if e.State.Members != nil {
		upsert.Members = ref(*e.State.Members)
	}
	for i, p := range e.State.Parents {
		upsert.Parents[i] = ref(p)
	}
	for _, o := range e.State.Consents {
		upsert.Consents = append(upsert.Consents, ref(o))
	}
	upsert.Operations = e.State.Operations
	if e.State.Agent != nil {
		upsert.Agent = ref(*e.State.Agent)
	}
	c.Operation = &policyv1.EntityChange_Upsert{Upsert: upsert}
	return c
}

func ref(e policyfeed.Entity) *policyv1.EntityRef {
	return &policyv1.EntityRef{Type: e.Type, Id: e.ID}
}

// Dial connects to the policy service at addr, trusting the CA in
// caFile and presenting a token signed by key on each call. The
// connection balances over every address addr resolves to, detects a
// lost peer by keepalive, and retries the read-only decision calls once
// on UNAVAILABLE (ADR-0178). opts are added to the connection's own
// options.
func Dial(
	addr, caFile, issuer string, key *signer.Key, opts ...grpc.DialOption,
) (*grpc.ClientConn, error) {
	return dial(addr, caFile, issuer, key, keepaliveParams, opts...)
}

// dial is Dial with the keepalive named, which is what lets a test
// dial without it.
func dial(
	addr, caFile, issuer string, key *signer.Key, params keepalive.ClientParameters,
	opts ...grpc.DialOption,
) (*grpc.ClientConn, error) {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errorx.Wrap(err, "read policy CA file", errorx.KindInvalidArgument)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errorx.New(errorx.KindInvalidArgument, "policy CA file holds no certificate")
	}
	dialOpts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(pool, "")),
		grpc.WithPerRPCCredentials(&bearer{key: key, issuer: issuer, now: time.Now}),
		grpc.WithDefaultServiceConfig(serviceConfig()),
	}
	if params.Time > 0 {
		dialOpts = append(dialOpts, grpc.WithKeepaliveParams(params))
	}
	conn, err := grpc.NewClient(target(addr), append(dialOpts, opts...)...)
	if err != nil {
		return nil, errorx.Wrap(err, "dial policy service", errorx.KindInvalidArgument)
	}
	return conn, nil
}

// bearer signs a token for each call.
type bearer struct {
	key    *signer.Key
	issuer string
	now    func() time.Time
}

func (b *bearer) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	token, err := b.key.Sign(signer.Claims{
		Issuer:   b.issuer,
		Subject:  Subject,
		Audience: Audience,
		IssuedAt: b.now(),
		Lifetime: TokenLifetime,
	})
	if err != nil {
		return nil, err
	}
	return map[string]string{"authorization": "Bearer " + token}, nil
}

func (b *bearer) RequireTransportSecurity() bool { return true }

// wrapCall names a failed call by the kind its status says, so a
// caller can tell a deadline from a refusal of the platform's token.
func wrapCall(err error, method string) error {
	kind := errorx.KindInternal
	switch status.Code(err) {
	case codes.DeadlineExceeded:
		kind = errorx.KindDeadlineExceeded
	case codes.Canceled:
		kind = errorx.KindCanceled
	case codes.Unauthenticated, codes.PermissionDenied:
		kind = errorx.KindPermissionDenied
	case codes.InvalidArgument:
		kind = errorx.KindInvalidArgument
	}
	return errorx.Wrap(err, "policy service "+method, kind)
}
