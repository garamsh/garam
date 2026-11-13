// Package grpc sends the policy feed to the policy service over its
// gRPC API (ADR-0140 §7), each call carrying a token the platform
// signs (ADR-0138).
package grpc

import (
	"context"
	"crypto/x509"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
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
	c.Operation = &policyv1.EntityChange_Upsert{Upsert: upsert}
	return c
}

func ref(e policyfeed.Entity) *policyv1.EntityRef {
	return &policyv1.EntityRef{Type: e.Type, Id: e.ID}
}

// Dial connects to the policy service at addr, trusting the CA in
// caFile and presenting a token signed by key on each call.
func Dial(addr, caFile, issuer string, key *signer.Key) (*grpc.ClientConn, error) {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, errorx.Wrap(err, "read policy CA file", errorx.KindInvalidArgument)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errorx.New(errorx.KindInvalidArgument, "policy CA file holds no certificate")
	}
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(pool, "")),
		grpc.WithPerRPCCredentials(&bearer{key: key, issuer: issuer, now: time.Now}))
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
