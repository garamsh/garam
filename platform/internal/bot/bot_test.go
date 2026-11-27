package bot_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/operator"
	"github.com/garamsh/garam/platform/internal/policydecision"
)

// actorToken is the connector's token botTokens resolves to self.
var actorToken = app.TokenPrefix + strings.Repeat("ab", 32)

// jkt is the thumbprint of the key the bot's process signed the
// exchange's proof with, which the transport verified.
const jkt = "0ZcOCORZNYy-DWpqq30jZyJGHTN0d2HglBV3uiguA4I"

// spendable spends a proof that was never spent.
func spendable(context.Context) error { return nil }

// botTokens is a CredentialStore resolving actorToken to self, holding
// placed as the declarations live on it, and recording each bot token
// it was asked to keep.
type botTokens struct {
	app.CredentialStore
	placed   map[string]bool
	resolved []string
	// actorGone has RecordBotToken find the actor's key withdrawn since
	// it resolved, and asksNot has the actor's app not ask to host bots.
	actorGone bool
	asksNot   bool
	recorded  []recordedBot
}

type recordedBot struct {
	place                grn.GRN
	id, actor, hash, jkt string
	ttl                  time.Duration
}

func (s *botTokens) ResolveToken(_ context.Context, hash string) (app.TokenHolder, error) {
	s.resolved = append(s.resolved, hash)
	if hash != app.HashSecret(actorToken) {
		return app.TokenHolder{}, app.ErrTokenInvalid
	}
	return app.TokenHolder{Instance: self, Installation: selfInstallation, RequestsBotHosting: !s.asksNot}, nil
}

func (s *botTokens) RecordBotToken(_ context.Context, g grn.GRN, id, actor, hash, jkt string, ttl time.Duration) (time.Time, error) {
	s.recorded = append(s.recorded, recordedBot{place: g, id: id, actor: actor, hash: hash, jkt: jkt, ttl: ttl})
	if s.actorGone {
		return time.Time{}, app.ErrTokenInvalid
	}
	if !s.placed[id] {
		return time.Time{}, bot.ErrBotNotPlaced
	}
	return time.Now().Add(ttl), nil
}

func (s *botTokens) RevokeInstallationBotTokens(context.Context, grn.GRN) error { return nil }

func (s *botTokens) ResolveBotToken(context.Context, string) (bot.HeldBotToken, error) {
	return bot.HeldBotToken{}, bot.ErrBotTokenInvalid
}

func newBotService() (bot.Service, *botTokens) {
	svc, store, _ := newHostedBotService()
	return svc, store
}

// newHostedBotService is newBotService with the installation's hosting
// the test changes.
func newHostedBotService() (bot.Service, *botTokens, *hosting) {
	store := &botTokens{placed: map[string]bool{"decl-a": true}}
	hosts := &hosting{hosts: map[grn.GRN]bool{selfInstallation: true}}
	return bot.NewService(store, nil, nil, store, bot.Config{TokenEndpoint: tokenURL, Decisions: hosts}), store, hosts
}

var placedBot = bot.BotGRN(self.Organization, "decl-a").String()

// TestMintBotToken_AnswersATokenForABotPlacedOnTheActorsRecord is the
// success path (ADR-0189 decision 2): the token is drawn behind its
// prefix, its hash and not its value is kept against the actor's record
// for an hour, and the answer names the bot as sub and the record as
// act.sub.
func TestMintBotToken_AnswersATokenForABotPlacedOnTheActorsRecord(t *testing.T) {
	svc, store := newBotService()
	token, err := svc.MintBotToken(context.Background(), bot.BotExchange{KeyThumbprint: jkt, SpendProof: spendable, ActorToken: actorToken, Subject: placedBot})
	require.NoError(t, err)
	assert.True(t, app.WellFormed(token.Value, bot.BotTokenPrefix), token.Value)
	assert.Equal(t, placedBot, token.Bot.String())
	assert.Equal(t, self, token.Place)
	assert.Equal(t, []string{}, token.Scope)
	assert.Equal(t, jkt, token.KeyThumbprint, "the token is bound to the proof's key")
	require.Len(t, store.recorded, 1)
	assert.Equal(t, recordedBot{place: self, id: "decl-a", actor: app.HashSecret(actorToken),
		hash: app.HashSecret(token.Value), jkt: jkt, ttl: time.Hour},
		store.recorded[0], "the hash is kept, never the value, under the actor that asked, bound to the key")
	assert.WithinDuration(t, time.Now().Add(time.Hour), token.ExpiresAt, time.Minute)
}

// TestMintBotToken_RefusesAnExchangeBoundToNoKey holds ADR-0191
// decision 1 in the domain: an exchange naming no key a proof showed,
// or carrying no proof to spend, is refused before the actor is read,
// so no unbound token is kept.
func TestMintBotToken_RefusesAnExchangeBoundToNoKey(t *testing.T) {
	for name, exchange := range map[string]bot.BotExchange{
		"no key":   {ActorToken: actorToken, Subject: placedBot, SpendProof: spendable},
		"no spend": {ActorToken: actorToken, Subject: placedBot, KeyThumbprint: jkt},
	} {
		t.Run(name, func(t *testing.T) {
			svc, store := newBotService()
			_, err := svc.MintBotToken(context.Background(), exchange)
			require.Error(t, err)
			assert.Empty(t, store.resolved, "the actor is not read")
			assert.Empty(t, store.recorded, "no token is kept")
		})
	}
}

// TestMintBotToken_SpendsTheProofOnlyOnceTheActorAuthenticated holds
// ADR-0191 decision 4's order: an actor that authenticates nothing,
// and a bot not placed, spend no proof; a placed bot's exchange spends
// it before its token is kept, and a spent proof keeps none.
func TestMintBotToken_SpendsTheProofOnlyOnceTheActorAuthenticated(t *testing.T) {
	spends := 0
	count := func(context.Context) error { spends++; return nil }
	svc, store := newBotService()
	_, err := svc.MintBotToken(context.Background(), bot.BotExchange{KeyThumbprint: jkt, SpendProof: count,
		ActorToken: app.TokenPrefix + strings.Repeat("cd", 32), Subject: placedBot})
	assert.ErrorIs(t, err, bot.ErrActorTokenRefused)
	_, err = svc.MintBotToken(context.Background(), bot.BotExchange{KeyThumbprint: jkt, SpendProof: count,
		ActorToken: actorToken, Subject: bot.BotGRN("other", "decl-a").String()})
	assert.ErrorIs(t, err, bot.ErrBotNotPlaced)
	assert.Zero(t, spends, "nothing authenticated, nothing spent")

	_, err = svc.MintBotToken(context.Background(), bot.BotExchange{KeyThumbprint: jkt, SpendProof: count,
		ActorToken: actorToken, Subject: placedBot})
	require.NoError(t, err)
	assert.Equal(t, 1, spends)

	replayed := errors.New("spent")
	store.recorded = nil
	_, err = svc.MintBotToken(context.Background(), bot.BotExchange{KeyThumbprint: jkt,
		SpendProof: func(context.Context) error { return replayed }, ActorToken: actorToken, Subject: placedBot})
	assert.ErrorIs(t, err, replayed)
	assert.Empty(t, store.recorded, "a spent proof keeps no token")
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
			_, err := svc.MintBotToken(context.Background(), bot.BotExchange{KeyThumbprint: jkt, SpendProof: spendable, ActorToken: actorToken, Subject: tc.subject})
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
	_, err := svc.MintBotToken(context.Background(), bot.BotExchange{KeyThumbprint: jkt, SpendProof: spendable, ActorToken: "garam_ot_x", Subject: placedBot})
	assert.ErrorIs(t, err, bot.ErrActorTokenRefused)
	assert.Empty(t, store.resolved, "a malformed actor token is not looked up")

	_, err = svc.MintBotToken(context.Background(), bot.BotExchange{KeyThumbprint: jkt, SpendProof: spendable,
		ActorToken: app.TokenPrefix + strings.Repeat("cd", 32), Subject: placedBot})
	assert.ErrorIs(t, err, bot.ErrActorTokenRefused)
	assert.Empty(t, store.recorded)

	store.actorGone = true
	_, err = svc.MintBotToken(context.Background(), bot.BotExchange{KeyThumbprint: jkt, SpendProof: spendable, ActorToken: actorToken, Subject: placedBot})
	assert.ErrorIs(t, err, bot.ErrActorTokenRefused, "a key withdrawn between resolving and writing")
	store.actorGone = false

	store.resolved = nil
	_, err = svc.MintBotToken(context.Background(), bot.BotExchange{KeyThumbprint: jkt, SpendProof: spendable, ActorToken: actorToken, Subject: placedBot, Scope: "tools:read"})
	assert.ErrorIs(t, err, bot.ErrScopeUndefined)
	assert.Empty(t, store.resolved, "a scope is refused before the actor is read")
}

// TestMintBotToken_RefusesAnActorWhoseInstallationHostsNoBots holds
// ADR-0192 decision 10's gate on the exchange: the question is
// iam:bot.host with the actor's installation as principal and as
// resource, read past the cache, and an installation not granted it
// draws no token, nor one whose app does not ask to host bots though a
// permit grants it. The same exchange minted while it was granted is
// the control.
func TestMintBotToken_RefusesAnActorWhoseInstallationHostsNoBots(t *testing.T) {
	svc, store, hosts := newHostedBotService()
	exchange := bot.BotExchange{KeyThumbprint: jkt, SpendProof: spendable, ActorToken: actorToken, Subject: placedBot}
	_, err := svc.MintBotToken(context.Background(), exchange)
	require.NoError(t, err, "the control")
	require.Len(t, hosts.asked, 1)
	assert.Equal(t, policydecision.Question{Subject: selfInstallation, Action: "iam:bot.host",
		Resource: selfInstallation, Current: true}, hosts.asked[0], "asked past the service's cache")

	hosts.hosts[selfInstallation] = false
	_, err = svc.MintBotToken(context.Background(), exchange)
	assert.ErrorIs(t, err, bot.ErrNotHosting)
	assert.Len(t, store.recorded, 1, "the refused exchange draws no token")

	hosts.hosts[selfInstallation], store.asksNot = true, true
	_, err = svc.MintBotToken(context.Background(), exchange)
	assert.ErrorIs(t, err, bot.ErrNotHosting, "a permit does not host bots for an app that does not ask to")
	assert.Len(t, store.recorded, 1)
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
				bot.BotExchange{KeyThumbprint: jkt, SpendProof: spendable, ActorToken: actorToken, Subject: placedBot, Audience: tc.audience})
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
	exchange := bot.BotExchange{KeyThumbprint: jkt, SpendProof: spendable, ActorToken: actorToken, Subject: placedBot}
	for range bot.BotTokensPerInstance {
		_, err := svc.MintBotToken(context.Background(), exchange)
		require.NoError(t, err)
	}
	_, err := svc.MintBotToken(context.Background(), exchange)
	var exceeded *operator.Exceeded
	assert.ErrorAs(t, err, &exceeded)
	assert.Len(t, store.recorded, bot.BotTokensPerInstance, "the exchange past the bound draws nothing")
}
