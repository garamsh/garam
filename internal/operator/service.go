package operator

import (
	"context"
	"errors"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Config holds where the exchange bounds read the time from, and the
// token endpoint an assertion names as its audience.
type Config struct {
	// Now supplies the current time, so a test can hold it still. Zero
	// takes time.Now.
	Now func() time.Time
	// TokenEndpoint is the absolute https URL of the private_key_jwt
	// exchange, which an assertion's aud equals byte for byte
	// (ADR-0102); [ParseTokenEndpoint] reads it. Empty serves no
	// assertion exchange.
	TokenEndpoint string
}

// Service is how an operator exchanges its key for tokens (ADR-0102),
// and how a token one operator exchanged to be shown to another is
// proven.
type Service interface {
	// ExchangeAssertion answers a token for the operator client whose
	// live key signed assertion, under machine-token.v2's profile
	// (ADR-0102), accepted by garam where audience is nil and by that
	// hosted operator where it names one, for [TokenTTL]. An assertion
	// that authenticates no operator — malformed, unsigned by a live
	// key of client's registration, naming another identity or
	// endpoint, outside its time bounds, or spent — is
	// ErrClientInvalid; an audience that is not another operator of
	// the organization is ErrAudienceInvalid; no configured endpoint is
	// ErrAssertionProfileUnavailable. source is the client address the
	// request arrived from, which bounds how many unverified
	// assertions one address may send; it and the operator each have
	// a bound, answering *Exceeded past either.
	ExchangeAssertion(ctx context.Context, source string, client grn.GRN, assertion string, audience *grn.GRN) (*Token, error)
	// VerifyShown refuses a token presented on another operator's
	// behalf unless it is a live token subject exchanged its key for,
	// to be shown to audience (ADR-0100). One that does not prove
	// subject is ErrTokenNotProven, and a read that
	// could not be made is unavailable.
	VerifyShown(ctx context.Context, subject, audience grn.GRN, token string) error
}

type service struct {
	tokens   TokenStore
	now      func() time.Time
	endpoint string
	// perSource bounds the exchanges each client address sends,
	// checked before anything is read; perClient bounds each operator's
	// authenticated ones (ADR-0102). Each exchange holds the operator's
	// registration, and the bound on those holds is what lets a write
	// waiting on the registration through (#1418).
	perClient *Limiter
	perSource *Limiter
}

// NewService constructs the service over the tokens a key is exchanged
// for.
func NewService(tokens TokenStore, cfg Config) Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &service{tokens: tokens, now: now,
		endpoint: cfg.TokenEndpoint,
		perClient: NewLimiter(LimiterConfig{Limit: ExchangesPerClient, Window: ExchangeWindow,
			MaxKeys: TrackedClients, Now: now}),
		perSource: NewLimiter(LimiterConfig{Limit: ExchangesPerSource, Window: ExchangeWindow,
			MaxKeys: TrackedClients, Now: now}),
	}
}

func (s *service) ExchangeAssertion(ctx context.Context, source string, client grn.GRN, assertion string, audience *grn.GRN) (*Token, error) {
	if s.endpoint == "" {
		return nil, ErrAssertionProfileUnavailable
	}
	if err := s.perSource.Check(source); err != nil {
		return nil, err
	}
	if client.ServiceType != ServiceType {
		return nil, ErrClientInvalid
	}
	parsed, err := ParseClientAssertion(assertion)
	if err != nil {
		return nil, err
	}
	key, err := s.tokens.VerificationKey(ctx, client, parsed.KID())
	if err != nil {
		return nil, err
	}
	verified, err := parsed.Verify(key, client, s.endpoint)
	if err != nil {
		return nil, err
	}
	// Counted once the key verified, so only its holder spends the
	// client's budget: a stranger knowing the GRN cannot lock the
	// operator out of refreshing.
	if err := s.perClient.Check(client.String()); err != nil {
		return nil, err
	}
	if err := validAudience(client, audience); err != nil {
		return nil, err
	}
	value, err := secret(TokenPrefix)
	if err != nil {
		return nil, errorx.Wrap(err, "operator token")
	}
	// The dates are the database's, stamped where the assertion's are
	// checked against the same clock.
	token := &Token{Value: value, Operator: client, Audience: audience}
	if err := s.tokens.RecordAssertionToken(ctx, verified, token); err != nil {
		return nil, err
	}
	return token, nil
}

// validAudience refuses an audience that is not another operator of
// subject's organization. A token minted for a hosted operator is
// shown to it, so the audience is a peer and never the caller itself.
func validAudience(subject grn.GRN, audience *grn.GRN) error {
	if audience != nil && (audience.ServiceType != ServiceType ||
		audience.Organization != subject.Organization || *audience == subject) {
		return ErrAudienceInvalid
	}
	return nil
}

func (s *service) VerifyShown(ctx context.Context, subject, audience grn.GRN, token string) error {
	// A value no exchange could have produced reads nothing.
	if !WellFormed(token, TokenPrefix) {
		return ErrTokenNotProven
	}
	holder, err := s.tokens.ResolveToken(ctx, HashToken(token))
	if errors.Is(err, ErrTokenInvalid) {
		return ErrTokenNotProven
	}
	if err != nil {
		return err
	}
	if holder.Operator != subject || holder.Audience == nil || *holder.Audience != audience {
		return ErrTokenNotProven
	}
	return nil
}
