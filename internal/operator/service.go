package operator

import (
	"context"
	"errors"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// renewalFraction is the share of a certificate's validity that is its
// renewal window, counted back from expiry. A third of it: wide enough
// that an operator polling on any cadence its own lifetime tolerates
// meets it many times before running out, and narrow enough that a
// certificate stays a certificate rather than a permit to hold one.
const renewalFraction = 3

// Config holds where the renewal rule reads the time from, and the
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

// Service is what an operator's own certificate goes through — the
// out-of-band mint that starts a lineage, and the renewal that carries
// it on — and the exchange of its key for tokens.
//
// Both record the certificate they issued, and that record is the whole
// of the authorization here: a renewal is admitted for the certificate
// garam last issued and for no other copy of the same identity. That
// is what a renewal has to bound by itself — deleting the operator
// ends every copy at once (ADR-0070), but it ends the operator too, so
// a chain of renewals nothing narrows would make a stolen certificate
// permanent for as long as the organization still wants the operator
// it was taken from.
type Service interface {
	// Issue mints an operator's certificate out of band and adopts it,
	// taking the lineage from whatever the operator held before
	// (ADR-0032).
	Issue(ctx context.Context, subject grn.GRN) (*IssuedCertificate, error)
	// Renew issues the successor to the certificate a connection
	// authenticated with, and refuses one that has expired, one still
	// outside its renewal window, and one garam has already replaced.
	Renew(ctx context.Context, c Credential) (*IssuedCertificate, error)
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
	// to be shown to audience — the token-borne form of the
	// certificate machine.Service.Verify proves (ADR-0100). One that
	// does not prove subject is ErrTokenNotProven, and a read that
	// could not be made is unavailable.
	VerifyShown(ctx context.Context, subject, audience grn.GRN, token string) error
}

type service struct {
	store    CertificateStore
	tokens   TokenStore
	issuer   Issuer
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

// NewService constructs the service over the record it keeps of each
// operator's certificate, the tokens a key is exchanged for, and the
// issuer it signs through.
func NewService(store CertificateStore, tokens TokenStore, issuer Issuer, cfg Config) Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &service{store: store, tokens: tokens, issuer: issuer, now: now,
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

func (s *service) Issue(ctx context.Context, subject grn.GRN) (*IssuedCertificate, error) {
	if subject.ServiceType != ServiceType {
		return nil, ErrNotAnOperator
	}
	issued, err := s.issuer.Issue(ctx, subject)
	if err != nil {
		return nil, err
	}
	if err := s.store.Adopt(ctx, current(subject, issued.Serial, s.now())); err != nil {
		return nil, err
	}
	return issued, nil
}

func (s *service) Renew(ctx context.Context, c Credential) (*IssuedCertificate, error) {
	now := s.now()
	if err := renewable(c, now); err != nil {
		return nil, err
	}
	issued, err := s.issuer.Issue(ctx, c.Subject)
	if err != nil {
		return nil, err
	}
	// Signed before the record moves, so a caller that has lost the
	// lineage receives nothing: the certificate it would have had is
	// discarded here, with the private key that exists nowhere but
	// inside this call.
	if err := s.store.Advance(ctx, current(c.Subject, issued.Serial, now), c.Serial); err != nil {
		return nil, err
	}
	return issued, nil
}

// current is the record an issued certificate leaves behind.
func current(subject grn.GRN, serial Serial, now time.Time) Current {
	return Current{Operator: subject, Serial: serial, IssuedAt: now.UTC()}
}

// renewable reports whether the presented certificate may be replaced,
// reading the window off the certificate itself so the rule holds
// whatever lifetime signed it.
//
// Expiry is the empty end of that window rather than a case beside it.
// An expired certificate authenticates nothing, so no identity reaches
// the route holding one (ADR-0014), and the boundary is stated here as
// well, so that reaching the rule any other way meets the same refusal.
func renewable(c Credential, now time.Time) error {
	if c.Subject.ServiceType != ServiceType {
		return ErrNotAnOperator
	}
	// A credential naming no certificate is one a token proved, and a
	// renewal replaces a certificate (ADR-0100): without this its zero
	// NotBefore would read as a lifetime begun at year one, every
	// moment of which is inside the window.
	if c.Serial == "" || c.NotBefore.IsZero() {
		return ErrNoCertificate
	}
	remaining := c.NotAfter.Sub(now)
	switch {
	case remaining <= 0:
		return ErrCertificateExpired
	case remaining > c.NotAfter.Sub(c.NotBefore)/renewalFraction:
		return ErrRenewalTooEarly
	}
	return nil
}
