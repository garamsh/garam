package policy

import (
	"context"
	"crypto/x509"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	"github.com/garamsh/garam/lib/errorx"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
)

// Client calls the policy service as one organization's cowork: each
// call carries the caller token of the organization it names, so a
// call is never made under another's.
type Client struct {
	conn   *grpc.ClientConn
	api    policyv1.PolicyServiceClient
	tokens *Tokens
}

// Dial connects to the service at cfg.Addr over TLS, trusting the CA
// in cfg.CAFile. The connection is made lazily, as grpc makes it.
func Dial(cfg Config, tokens *Tokens) (*Client, error) {
	pem, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return nil, errorx.Wrap(err, "policy: read CA file", errorx.KindInvalidArgument)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, errorx.New(errorx.KindInvalidArgument, "policy: CA file holds no certificate")
	}
	conn, err := grpc.NewClient(cfg.Addr, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(pool, "")))
	if err != nil {
		return nil, errorx.Wrap(err, "policy: dial", errorx.KindInvalidArgument)
	}
	return &Client{conn: conn, api: policyv1.NewPolicyServiceClient(conn), tokens: tokens}, nil
}

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Evaluations asks the service the batch in req as org's cowork.
func (c *Client) Evaluations(ctx context.Context, org string, req *policyv1.EvaluationsRequest) (*policyv1.EvaluationsResponse, error) {
	opt, err := c.credentials(ctx, org)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.Evaluations(ctx, req, opt)
	if err != nil {
		return nil, wrapCall(err, "Evaluations")
	}
	return resp, nil
}

// ApplyEntityChanges feeds the service the changes in req as org's
// cowork.
func (c *Client) ApplyEntityChanges(ctx context.Context, org string, req *policyv1.ApplyEntityChangesRequest) (*policyv1.ApplyEntityChangesResponse, error) {
	opt, err := c.credentials(ctx, org)
	if err != nil {
		return nil, err
	}
	resp, err := c.api.ApplyEntityChanges(ctx, req, opt)
	if err != nil {
		return nil, wrapCall(err, "ApplyEntityChanges")
	}
	return resp, nil
}

func (c *Client) credentials(ctx context.Context, org string) (grpc.CallOption, error) {
	token, err := c.tokens.Token(ctx, org)
	if err != nil {
		return nil, err
	}
	return grpc.PerRPCCredentials(bearer(token)), nil
}

// bearer presents one token on one call.
type bearer string

func (b bearer) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(b)}, nil
}

func (b bearer) RequireTransportSecurity() bool { return true }

// wrapCall names a failed call by the kind its status says: a refusal
// of cowork's token or request is its own, anything else is the
// service being unavailable.
func wrapCall(err error, call string) error {
	kind := errorx.KindUnavailable
	switch status.Code(err) {
	case codes.Unauthenticated, codes.PermissionDenied:
		kind = errorx.KindPermissionDenied
	case codes.InvalidArgument:
		kind = errorx.KindInvalidArgument
	}
	return errorx.Wrap(err, "policy service "+call, kind)
}
