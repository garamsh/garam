// Package bot is garam's bot domain (ADR-0192 decision 10): what
// garam declares for an app's instances to apply and what they report
// back (ADR-0187, ADR-0188), and the tokens a connector exchanges for
// the bots placed on its instance (ADR-0189), each keyed by the
// instance's GRN. A declaration is placed under iam:bot.place. The
// instance itself, its registration, its keys and its last contact
// are internal/app's.
package bot

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// WatchesPerInstance bounds the declaration polls one instance sends
// per operator.ExchangeWindow (ADR-0187 decision 7): three pages a
// second through a full set, and far past one held poll's pace.
const WatchesPerInstance = 300

// Config holds the clock, garam's audience for a bot's token, and how a
// declarations poll is paged and held.
type Config struct {
	// Now supplies the current time; zero takes time.Now.
	Now func() time.Time
	// TokenEndpoint is the machine listener's token endpoint, the
	// audience a bot's token is for.
	TokenEndpoint string
	// Wakes is where a held declarations poll waits for a write to its
	// record; nil holds each poll until its recheck.
	Wakes *Wakes
	// DeclarationPageSize is how many changes a page answers; zero
	// takes DeclarationPageSize.
	DeclarationPageSize int
	// DeclarationRecheck is how often a held poll reads again unwoken;
	// zero takes DeclarationRecheck.
	DeclarationRecheck time.Duration
	// Decisions decides whether an exchange's actor's installation
	// hosts bots; nil refuses every exchange as unavailable.
	Decisions policydecision.Service
}

// Service is how a connector reads what is declared for its instance
// (ADR-0187), reports what it holds (ADR-0188) and exchanges its token
// for a placed bot's (ADR-0189). A member's half is [Declare].
type Service interface {
	// WatchDeclarations answers caller's changes after after, holding
	// an empty page up to wait (DECL-1 to DECL-6). A full set's page is
	// answered at once, empty or not, and a record deleted meanwhile is
	// operator.ErrClientInvalid.
	WatchDeclarations(ctx context.Context, caller grn.GRN, after *string, wait time.Duration) (Page, error)
	// Report keeps r as caller's report where it was observed after
	// the one kept, with only what names a declaration placed on caller
	// (REP-5, REP-8). Past ReportsPerInstance it answers
	// *operator.Exceeded, and a record deleted meanwhile is
	// operator.ErrClientInvalid.
	Report(ctx context.Context, caller grn.GRN, r Report) error
	// MintBotToken answers a token for the bot a connector's exchange
	// names, to the record whose token is its actor, where a live
	// declaration places the bot on that record (ADR-0189 decision 2,
	// AUTH-5) and the record's installation hosts bots (ADR-0192
	// decision 10). Past BotTokensPerInstance it answers
	// *operator.Exceeded.
	MintBotToken(ctx context.Context, exchange BotExchange) (*BotToken, error)
	// ResolveBotToken answers the live bot token value is, with the
	// bot and the key it is bound to, or ErrBotTokenInvalid. It proves
	// nothing about the holder: a resource server verifies the proof
	// of that key before it acts (ADR-0191 decision 6).
	ResolveBotToken(ctx context.Context, value string) (HeldBotToken, error)
}

type service struct {
	credentials  app.CredentialStore
	declarations DeclarationStore
	reports      ReportStore
	botTokens    BotTokenStore
	cfg          Config
	// watches bounds each instance's declaration polls, reportsLimit
	// its reports and botTokenLimit its bot token exchanges.
	watches       *operator.Limiter
	reportsLimit  *operator.Limiter
	botTokenLimit *operator.Limiter
}

// NewService constructs the service over the credentials an actor
// token is resolved against, the declarations placed on an instance,
// the reports it sends and its bots' tokens.
func NewService(credentials app.CredentialStore, declarations DeclarationStore, reports ReportStore,
	botTokens BotTokenStore, cfg Config) Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
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
	return &service{credentials: credentials, declarations: declarations, reports: reports,
		botTokens: botTokens, cfg: cfg,
		watches:       limiter(WatchesPerInstance),
		reportsLimit:  limiter(ReportsPerInstance),
		botTokenLimit: limiter(BotTokensPerInstance),
	}
}
