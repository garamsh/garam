// Package cluster owns what garam declares for the instances of
// garam-bot-connector to apply and what they report back (ADR-0187,
// ADR-0188), and the tokens a connector exchanges for the bots placed
// on its instance (ADR-0189). The instance itself, its registration
// and its keys are internal/app's. All of it moves to garam's bot
// domain under iam:bot.place and iam:bot.host (ADR-0192 decision 10,
// #2082).
package cluster

import (
	"context"
	"time"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/operator"
)

// WatchesPerCluster bounds the declaration polls one instance sends per
// operator.ExchangeWindow (ADR-0187 decision 7): three pages a second
// through a full set, and far past one held poll's pace.
const WatchesPerCluster = 300

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
		watches:       limiter(WatchesPerCluster),
		reportsLimit:  limiter(ReportsPerCluster),
		botTokenLimit: limiter(BotTokensPerCluster),
	}
}
