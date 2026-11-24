package cluster

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/operator"
)

// The typ values of a rotation's two JWSs (ROT-1).
const (
	TypeKeyRotation = "key-rotation+jwt"
	TypeKeyProof    = "key-proof+jwt"
)

// RegistrationsPerSource is the default bound on the registrations one
// client address sends per operator.ExchangeWindow, counted before the
// token is read (ADR-0184 decision 3, REG-8). Config.RegistrationLimit
// replaces it.
const RegistrationsPerSource = 10

// WatchesPerCluster bounds the declaration polls one cluster sends per
// operator.ExchangeWindow (ADR-0187 decision 7): three pages a second
// through a full set, and far past one held poll's pace.
const WatchesPerCluster = 300

// RotationsPerCluster bounds the rotation requests one cluster sends
// per operator.ExchangeWindow, counted before a signature is verified.
// A connector rotates once a key's life and repeats only a lost answer
// (ROT-5), so the bound is far past what one needs.
const RotationsPerCluster = 10

// Config holds the clock, the two absolute URLs a cluster's signed
// requests name as their audience, and how a declarations poll is
// paged and held.
type Config struct {
	// Now supplies the current time; zero takes time.Now.
	Now func() time.Time
	// TokenEndpoint is the private_key_jwt exchange's URL, which a
	// registration answers as tokenUrl and an assertion's aud names.
	TokenEndpoint string
	// RotationURL is the key rotation route's absolute URL, which both
	// of a rotation's JWSs name as aud (ROT-1).
	RotationURL string
	// RegistrationLimit bounds the registrations one client address
	// sends per operator.ExchangeWindow; zero takes
	// RegistrationsPerSource. A deployment always has a bound.
	RegistrationLimit int
	// Wakes is where a held declarations poll waits for a write to its
	// record; nil holds each poll until its recheck.
	Wakes *Wakes
	// DeclarationPageSize is how many changes a page answers; zero
	// takes DeclarationPageSize.
	DeclarationPageSize int
	// DeclarationRecheck is how often a held poll reads again unwoken;
	// zero takes DeclarationRecheck.
	DeclarationRecheck time.Duration
}

// Service is how a cluster's connector registers, exchanges its key
// for tokens, rotates it (ADR-0184 decisions 3 to 6) and reads what is
// declared for it (ADR-0187). A member's halves are
// [IssueRegistrationToken] and [Declare].
type Service interface {
	// Register spends token and binds jwk to its cluster, answering
	// the cluster. source is the client's address: past
	// RegistrationsPerSource it answers *operator.Exceeded before the
	// token is read, so a throttled request spends nothing.
	Register(ctx context.Context, source, token string, jwk []byte) (grn.GRN, error)
	// ExchangeAssertion answers a token for the cluster client whose
	// live key signed assertion, under machine-token.v2's profile.
	ExchangeAssertion(ctx context.Context, source string, client grn.GRN, assertion string) (*Token, error)
	// Rotate adds jwk as caller's successor key, proved by
	// authorization, signed by a live key and naming jwk's thumbprint,
	// and proof, signed by jwk (ROT-1 to ROT-4).
	Rotate(ctx context.Context, caller grn.GRN, jwk []byte, authorization, proof string) (Rotation, error)
	// WatchDeclarations answers caller's changes after after, holding
	// an empty page up to wait (DECL-1 to DECL-6). A full set's page is
	// answered at once, empty or not, and a record deleted meanwhile is
	// operator.ErrClientInvalid.
	WatchDeclarations(ctx context.Context, caller grn.GRN, after *string, wait time.Duration) (Page, error)
	// Report keeps r as caller's report where it was observed after
	// the one kept, with only what names a declaration placed on caller
	// (REP-5, REP-8). Past ReportsPerCluster it answers
	// *operator.Exceeded, and a record deleted meanwhile is
	// operator.ErrClientInvalid.
	Report(ctx context.Context, caller grn.GRN, r Report) error
	// MintBotToken answers a token for the bot a connector's exchange
	// names, to the record whose token is its actor, where a live
	// declaration places the bot on that record (ADR-0189 decision 2,
	// AUTH-5). Past BotTokensPerCluster it answers
	// *operator.Exceeded.
	MintBotToken(ctx context.Context, exchange BotExchange) (*BotToken, error)
}

type service struct {
	credentials  CredentialStore
	declarations DeclarationStore
	reports      ReportStore
	cfg          Config
	// perSource bounds registrations and exchanges per address before
	// anything is read; perClient each cluster's verified exchanges.
	registrations *operator.Limiter
	perSource     *operator.Limiter
	perClient     *operator.Limiter
	// rotations bounds each cluster's rotation requests, watches its
	// declaration polls, reportsLimit its reports and botTokens its bot
	// token exchanges.
	rotations    *operator.Limiter
	watches      *operator.Limiter
	reportsLimit *operator.Limiter
	botTokens    *operator.Limiter
}

// NewService constructs the service over a cluster's credentials, the
// declarations placed on it and the reports it sends.
func NewService(credentials CredentialStore, declarations DeclarationStore, reports ReportStore, cfg Config) Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.RegistrationLimit == 0 {
		cfg.RegistrationLimit = RegistrationsPerSource
	}
	if cfg.Wakes == nil {
		cfg.Wakes = NewWakes()
	}
	if cfg.DeclarationPageSize == 0 {
		cfg.DeclarationPageSize = DeclarationPageSize
	}
	if cfg.DeclarationRecheck == 0 {
		cfg.DeclarationRecheck = DeclarationRecheck
	}
	limiter := func(limit int) *operator.Limiter {
		return operator.NewLimiter(operator.LimiterConfig{Limit: limit, Window: operator.ExchangeWindow,
			MaxKeys: operator.TrackedClients, Now: cfg.Now})
	}
	return &service{credentials: credentials, declarations: declarations, reports: reports, cfg: cfg,
		registrations: limiter(cfg.RegistrationLimit),
		perSource:     limiter(operator.ExchangesPerSource),
		perClient:     limiter(operator.ExchangesPerClient),
		rotations:     limiter(RotationsPerCluster),
		watches:       limiter(WatchesPerCluster),
		reportsLimit:  limiter(ReportsPerCluster),
		botTokens:     limiter(BotTokensPerCluster),
	}
}
