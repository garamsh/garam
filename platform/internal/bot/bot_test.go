package bot_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/operator"
)

// actorToken is the connector's token botTokens resolves to self.
var actorToken = app.TokenPrefix + strings.Repeat("ab", 32)

// botTokens is a CredentialStore resolving actorToken to self, holding
// placed as the declarations live on it, and recording each bot token
// it was asked to keep.
type botTokens struct {
	app.CredentialStore
	placed   map[string]bool
	resolved []string
	// actorGone has RecordBotToken find the actor's key withdrawn since
	// it resolved.
	actorGone bool
	recorded  []recordedBot
}

type recordedBot struct {
	place           grn.GRN
	id, actor, hash string
	ttl             time.Duration
}

func (s *botTokens) ResolveToken(_ context.Context, hash string) (app.TokenHolder, error) {
	s.resolved = append(s.resolved, hash)
	if hash != app.HashSecret(actorToken) {
		return app.TokenHolder{}, app.ErrTokenInvalid
	}
	return app.TokenHolder{Instance: self}, nil
}

func (s *botTokens) RecordBotToken(_ context.Context, g grn.GRN, id, actor, hash string, ttl time.Duration) (time.Time, error) {
	s.recorded = append(s.recorded, recordedBot{place: g, id: id, actor: actor, hash: hash, ttl: ttl})
	if s.actorGone {
		return time.Time{}, app.ErrTokenInvalid
	}
	if !s.placed[id] {
		return time.Time{}, bot.ErrBotNotPlaced
	}
	return time.Now().Add(ttl), nil
}

func newBotService() (bot.Service, *botTokens) {
	store := &botTokens{placed: map[string]bool{"decl-a": true}}
	return bot.NewService(store, nil, nil, store, bot.Config{TokenEndpoint: tokenURL}), store
}

var placedBot = bot.BotGRN(self.Organization, "decl-a").String()

// TestMintBotToken_AnswersATokenForABotPlacedOnTheActorsRecord is the
// success path (ADR-0189 decision 2): the token is drawn behind its
// prefix, its hash and not its value is kept against the actor's record
// for an hour, and the answer names the bot as sub and the record as
// act.sub.
func TestMintBotToken_AnswersATokenForABotPlacedOnTheActorsRecord(t *testing.T) {
	svc, store := newBotService()
	token, err := svc.MintBotToken(context.Background(), bot.BotExchange{ActorToken: actorToken, Subject: placedBot})
	require.NoError(t, err)
	assert.True(t, app.WellFormed(token.Value, bot.BotTokenPrefix), token.Value)
	assert.Equal(t, placedBot, token.Bot.String())
	assert.Equal(t, self, token.Place)
	assert.Equal(t, []string{}, token.Scope)
	require.Len(t, store.recorded, 1)
	assert.Equal(t, recordedBot{place: self, id: "decl-a", actor: app.HashSecret(actorToken),
		hash: app.HashSecret(token.Value), ttl: time.Hour},
		store.recorded[0], "the hash is kept, never the value, under the actor that asked")
	assert.WithinDuration(t, time.Now().Add(time.Hour), token.ExpiresAt, time.Minute)
}

// TestMintBotToken_RefusesWhatAUTH5Refuses holds each refusal to the
// success path as its control: a subject that is not a bot of the
// actor's organization never reaches the store, and one the store finds
// placed on no live declaration of the record is refused as it is.
func TestMintBotToken_RefusesWhatAUTH5Refuses(t *testing.T) {
	for _, tc := range []struct {
		name, subject string
		reachesStore  bool
	}{
		{"a bot of another organization", bot.BotGRN("other", "decl-a").String(), false},
		{"an agent record's GRN", bot.AgentGRN(self.Organization, "decl-a").String(), false},
		{"the record itself", self.String(), false},
		{"a value that is no GRN", "decl-a", false},
		{"a bot no live declaration places here", bot.BotGRN(self.Organization, "decl-gone").String(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newBotService()
			_, err := svc.MintBotToken(context.Background(), bot.BotExchange{ActorToken: actorToken, Subject: tc.subject})
			assert.ErrorIs(t, err, bot.ErrBotNotPlaced)
			assert.Equal(t, tc.reachesStore, len(store.recorded) == 1)
		})
	}
}

// TestMintBotToken_AuthenticatesTheActorBeforeAnythingElse holds that
// an actor token that is malformed is refused without a read, one that
// does not resolve is refused, one whose key is withdrawn before the
// token is written is refused, and a scope is refused before any token
// is read. Each actor refusal is ErrActorTokenRefused.
func TestMintBotToken_AuthenticatesTheActorBeforeAnythingElse(t *testing.T) {
	svc, store := newBotService()
	_, err := svc.MintBotToken(context.Background(), bot.BotExchange{ActorToken: "garam_ot_x", Subject: placedBot})
	assert.ErrorIs(t, err, bot.ErrActorTokenRefused)
	assert.Empty(t, store.resolved, "a malformed actor token is not looked up")

	_, err = svc.MintBotToken(context.Background(), bot.BotExchange{
		ActorToken: app.TokenPrefix + strings.Repeat("cd", 32), Subject: placedBot})
	assert.ErrorIs(t, err, bot.ErrActorTokenRefused)
	assert.Empty(t, store.recorded)

	store.actorGone = true
	_, err = svc.MintBotToken(context.Background(), bot.BotExchange{ActorToken: actorToken, Subject: placedBot})
	assert.ErrorIs(t, err, bot.ErrActorTokenRefused, "a key withdrawn between resolving and writing")
	store.actorGone = false

	store.resolved = nil
	_, err = svc.MintBotToken(context.Background(), bot.BotExchange{ActorToken: actorToken, Subject: placedBot, Scope: "tools:read"})
	assert.ErrorIs(t, err, bot.ErrScopeUndefined)
	assert.Empty(t, store.resolved, "a scope is refused before the actor is read")
}

// TestMintBotToken_TakesGaramsAudienceOrNone holds ADR-0189 decision
// 2's audience: garam's, the token endpoint's URL, and an absent one
// are minted for, and any other is refused before the actor is read.
func TestMintBotToken_TakesGaramsAudienceOrNone(t *testing.T) {
	garams, other, empty := tokenURL, "https://ai.garam.sh", ""
	for _, tc := range []struct {
		name     string
		audience *string
		refused  bool
	}{
		{"garam's audience", &garams, false},
		{"none", nil, false},
		{"another audience", &other, true},
		{"an empty audience", &empty, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newBotService()
			_, err := svc.MintBotToken(context.Background(),
				bot.BotExchange{ActorToken: actorToken, Subject: placedBot, Audience: tc.audience})
			if tc.refused {
				assert.ErrorIs(t, err, bot.ErrAudienceRefused)
				assert.Empty(t, store.resolved, "refused before the actor is read")
				return
			}
			assert.NoError(t, err)
		})
	}
}

// TestMintBotToken_IsBoundedPerInstance holds BotTokensPerInstance.
func TestMintBotToken_IsBoundedPerInstance(t *testing.T) {
	svc, store := newBotService()
	exchange := bot.BotExchange{ActorToken: actorToken, Subject: placedBot}
	for range bot.BotTokensPerInstance {
		_, err := svc.MintBotToken(context.Background(), exchange)
		require.NoError(t, err)
	}
	_, err := svc.MintBotToken(context.Background(), exchange)
	var exceeded *operator.Exceeded
	assert.ErrorAs(t, err, &exceeded)
	assert.Len(t, store.recorded, bot.BotTokensPerInstance, "the exchange past the bound draws nothing")
}
