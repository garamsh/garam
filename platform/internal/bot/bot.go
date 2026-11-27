package bot

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
)

// The RFC 8693 exchange a connector makes for a bot garam declared and
// placed on its record (ADR-0189 decision 2).
const (
	// GrantTypeTokenExchange is RFC 8693's grant.
	GrantTypeTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"
	// TokenTypeAccessToken is the actor token's type, a connector's own
	// access token, and the type of the token issued.
	TokenTypeAccessToken = "urn:ietf:params:oauth:token-type:access_token"
	// TokenTypeBot is the subject token's type, garam's own: its value
	// is the bot's GRN.
	TokenTypeBot = "urn:garam:params:oauth:token-type:bot"
	// BotServiceType is the GRN type of a bot garam declared,
	// grn:<org>/iam/bot/<declaration id>.
	BotServiceType = "bot"
	// BotKind is the declared kind that places a bot; no other kind
	// does (ADR-0189 decision 1).
	BotKind = "Bot"
	// BotTokenPrefix marks a bot's token.
	BotTokenPrefix = "garam_bt_"
	// BotTokenTTL is how long a bot's token is accepted for, the most
	// garam-bot ADR 0037 decision 1 allows.
	BotTokenTTL = time.Hour
	// BotTokensPerInstance bounds the bot tokens one record exchanges
	// for per operator.ExchangeWindow: ten a second, so a connector
	// starting a few hundred bots at once is not held back, far past
	// one exchange per bot an hour.
	BotTokensPerInstance = 600
)

// agentServiceType is the GRN type ADR-0179 renames to BotServiceType;
// an agent record's identifier is held in the space a declaration's is.
const agentServiceType = "agent"

var (
	// ErrBotNotPlaced answers a bot no live Bot declaration places on
	// the exchanging record (AUTH-5): one never declared, declared as
	// another kind, placed on another record, tombstoned, or of
	// another organization.
	ErrBotNotPlaced = errorx.New(errorx.KindPermissionDenied, "no live declaration places the bot on this instance")
	// ErrActorTokenRefused answers an actor token that authenticates
	// no instance: malformed, unknown, expired, or exchanged
	// under a key since withdrawn.
	ErrActorTokenRefused = errorx.New(errorx.KindUnauthenticated, "the actor token authenticates no instance")
	// ErrAudienceRefused answers an audience other than garam's, the
	// token endpoint's URL.
	ErrAudienceRefused = errorx.New(errorx.KindInvalidArgument, "a bot's token is for garam's audience alone")
	// ErrDeclarationIDHeldByAgent answers a declaration whose identifier
	// names an agent record of the organization: declared bots and the
	// agent records ADR-0179 renames share one identifier space.
	ErrDeclarationIDHeldByAgent = errorx.New(errorx.KindAlreadyExists, "the identifier names a bot the organization already holds")
)

// BotGRN is the GRN of the bot declaration id places in organization.
func BotGRN(organization, id string) grn.GRN {
	return grn.New(organization, BotServiceType, id)
}

// AgentGRN is the GRN an agent record of organization under id holds
// until ADR-0179's migration moves it to BotGRN.
func AgentGRN(organization, id string) grn.GRN {
	return grn.New(organization, agentServiceType, id)
}

// BotToken is a bot's token as issued: its value, shown once, and what
// it was issued for. Its claims are the ones garam-bot's
// bot-system.md §Authentication names.
type BotToken struct {
	Value string
	// Bot is the token's sub, and Place its act.sub.
	Bot   grn.GRN
	Place grn.GRN
	// Scope is what was asked and allowed.
	Scope     []string
	ExpiresAt time.Time
}

// BotExchange is what a connector's exchange carries, off its form.
type BotExchange struct {
	// ActorToken is the connector's own access token.
	ActorToken string
	// Subject is the subject token's value, the bot's GRN.
	Subject string
	// Audience is the audience asked for, nil where none was named,
	// which asks for garam's.
	Audience *string
	// Scope is what the bot is to call, space-separated; empty asks
	// for nothing.
	Scope string
}

// MintBotToken answers a token for the bot exchange's subject names,
// to the record whose token is its actor (ADR-0189 decision 2). The
// actor is read as every request's token is, and again as the token
// is written, so a withdrawn key or a deleted record refuses it with
// ErrActorTokenRefused. A record whose installation hosts no bots is
// ErrNotHosting, and a bot no live Bot declaration places on that
// record ErrBotNotPlaced. The audience is garam's, the token
// endpoint's URL, or none; another is ErrAudienceRefused. A scope is
// refused, none being defined yet.
func (s *service) MintBotToken(ctx context.Context, exchange BotExchange) (*BotToken, error) {
	if !app.WellFormed(exchange.ActorToken, app.TokenPrefix) {
		return nil, ErrActorTokenRefused
	}
	if exchange.Audience != nil && (s.cfg.TokenEndpoint == "" || *exchange.Audience != s.cfg.TokenEndpoint) {
		return nil, ErrAudienceRefused
	}
	if strings.TrimSpace(exchange.Scope) != "" {
		return nil, ErrScopeUndefined
	}
	actor := app.HashSecret(exchange.ActorToken)
	holder, err := s.credentials.ResolveToken(ctx, actor)
	if errors.Is(err, app.ErrTokenInvalid) {
		return nil, ErrActorTokenRefused
	}
	if err != nil {
		return nil, err
	}
	place := holder.Instance
	if err := s.botTokenLimit.Check(place.String()); err != nil {
		return nil, err
	}
	if err := Hosts(ctx, s.cfg.Decisions, holder.Installation); err != nil {
		return nil, err
	}
	bot, err := grn.Parse(exchange.Subject)
	if err != nil || bot != BotGRN(place.Organization, bot.Identifier) {
		return nil, ErrBotNotPlaced
	}
	value, err := app.NewSecret(BotTokenPrefix)
	if err != nil {
		return nil, err
	}
	expiresAt, err := s.botTokens.RecordBotToken(ctx, place, bot.Identifier, actor, app.HashSecret(value), BotTokenTTL)
	if errors.Is(err, app.ErrTokenInvalid) {
		return nil, ErrActorTokenRefused
	}
	if err != nil {
		return nil, err
	}
	return &BotToken{Value: value, Bot: bot, Place: place, Scope: []string{}, ExpiresAt: expiresAt}, nil
}

// ErrScopeUndefined answers a scope: none is defined for a bot yet, and
// nothing is implied.
var ErrScopeUndefined = errorx.New(errorx.KindInvalidArgument, "no scope is defined for a bot's token yet")
