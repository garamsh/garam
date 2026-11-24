package cluster

import (
	"context"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// ExchangeAssertion runs the operator's private_key_jwt profile over a
// cluster's keys (ADR-0184 decision 5): the same parse, the same
// verification and the same bounds, against the record's own store.
func (s *service) ExchangeAssertion(ctx context.Context, source string, client grn.GRN, assertion string) (*Token, error) {
	if s.cfg.TokenEndpoint == "" {
		return nil, operator.ErrAssertionProfileUnavailable
	}
	if err := s.perSource.Check(source); err != nil {
		return nil, err
	}
	if !IsClusterGRN(client) {
		return nil, operator.ErrClientInvalid
	}
	parsed, err := operator.ParseClientAssertion(assertion)
	if err != nil {
		return nil, err
	}
	key, err := s.credentials.VerificationKey(ctx, client, parsed.KID())
	if err != nil {
		return nil, err
	}
	verified, err := parsed.Verify(key.Verifier(), client, s.cfg.TokenEndpoint)
	if err != nil {
		return nil, err
	}
	// Counted once the key verified, so a stranger knowing the GRN
	// cannot spend the cluster's budget.
	if err := s.perClient.Check(client.String()); err != nil {
		return nil, err
	}
	value, err := NewSecret(TokenPrefix)
	if err != nil {
		return nil, err
	}
	token := &Token{Value: value, Cluster: client}
	if err := s.credentials.RecordAssertionToken(ctx, verified, token); err != nil {
		return nil, err
	}
	return token, nil
}
