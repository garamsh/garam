package cluster

import (
	"context"
	"errors"
	"time"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// IssueRegistrationToken draws g a registration token, stores its hash
// in credentials, and answers the token, once, with when it expires
// (ADR-0184 decision 3). Any token g held before is spent.
func IssueRegistrationToken(ctx context.Context, credentials CredentialStore, g grn.GRN) (string, time.Time, error) {
	token, err := NewSecret(RegistrationTokenPrefix)
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt, err := credentials.IssueRegistrationToken(ctx, g, HashSecret(token), RegistrationTokenTTL)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
}

func (s *service) Register(ctx context.Context, source, token string, jwk []byte) (grn.GRN, error) {
	// A listener with no exchange has nowhere to send the connector
	// (REG-6), so it spends no token.
	if s.cfg.TokenEndpoint == "" {
		return grn.GRN{}, operator.ErrAssertionProfileUnavailable
	}
	if err := s.registrations.Check(source); err != nil {
		return grn.GRN{}, err
	}
	// A value no mint could have produced names no row, so it is
	// refused as an unknown token is, before the store is read.
	if !WellFormed(token, RegistrationTokenPrefix) {
		return grn.GRN{}, ErrRegistrationRefused
	}
	// The token is spent before the key is read, so it is spent however
	// the key fares (REG-3): presenting it is its one attempt.
	g, err := s.credentials.SpendRegistrationToken(ctx, HashSecret(token))
	if err != nil {
		return grn.GRN{}, err
	}
	key, err := ParsePublicJWK(jwk)
	if err != nil {
		return grn.GRN{}, err
	}
	// A record deleted between the spend and the bind is refused as a
	// token that names no record is (REG-4), not answered 404, which
	// the route does not declare.
	if err := s.credentials.BindKey(ctx, g, key); errors.Is(err, ErrClusterNotFound) {
		return grn.GRN{}, ErrRegistrationRefused
	} else if err != nil {
		return grn.GRN{}, err
	}
	return g, nil
}
