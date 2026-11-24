package cluster_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/operator"
)

// actorToken is the connector's token botTokens resolves to self.
var actorToken = cluster.TokenPrefix + strings.Repeat("ab", 32)

// botTokens is a CredentialStore resolving actorToken to self, holding
// placed as the declarations live on it, and recording each bot token
// it was asked to keep.
type botTokens struct {
	cluster.CredentialStore
	placed   map[string]bool
	resolved []string
	recorded []recordedBot
}

type recordedBot struct {
	place    grn.GRN
	id, hash string
	ttl      time.Duration
}

func (s *botTokens) ResolveToken(_ context.Context, hash string) (cluster.TokenHolder, error) {
	s.resolved = append(s.resolved, hash)
	if hash != cluster.HashSecret(actorToken) {
		return cluster.TokenHolder{}, cluster.ErrTokenInvalid
	}
	return cluster.TokenHolder{Cluster: self}, nil
}

func (s *botTokens) RecordBotToken(_ context.Context, g grn.GRN, id, hash string, ttl time.Duration) (time.Time, error) {
	s.recorded = append(s.recorded, recordedBot{place: g, id: id, hash: hash, ttl: ttl})
	if !s.placed[id] {
		return time.Time{}, cluster.ErrBotNotPlaced
	}
	return time.Now().Add(ttl), nil
}

func newBotService() (cluster.Service, *botTokens) {
	store := &botTokens{placed: map[string]bool{"decl-a": true}}
	return cluster.NewService(store, nil, nil, cluster.Config{}), store
}

var placedBot = cluster.BotGRN(self.Organization, "decl-a").String()

// TestMintBotToken_AnswersATokenForABotPlacedOnTheActorsRecord is the
// success path (ADR-0189 decision 2): the token is drawn behind its
// prefix, its hash and not its value is kept against the actor's record
// for an hour, and the answer names the bot as sub and the record as
// act.sub.
func TestMintBotToken_AnswersATokenForABotPlacedOnTheActorsRecord(t *testing.T) {
	svc, store := newBotService()
	token, err := svc.MintBotToken(context.Background(), cluster.BotExchange{ActorToken: actorToken, Subject: placedBot})
	require.NoError(t, err)
	assert.True(t, cluster.WellFormed(token.Value, cluster.BotTokenPrefix), token.Value)
	assert.Equal(t, placedBot, token.Bot.String())
	assert.Equal(t, self, token.Place)
	assert.Equal(t, []string{}, token.Scope)
	require.Len(t, store.recorded, 1)
	assert.Equal(t, recordedBot{place: self, id: "decl-a", hash: cluster.HashSecret(token.Value), ttl: time.Hour},
		store.recorded[0], "the hash is kept, never the value")
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
		{"a bot of another organization", cluster.BotGRN("other", "decl-a").String(), false},
		{"an agent record's GRN", cluster.AgentGRN(self.Organization, "decl-a").String(), false},
		{"the record itself", self.String(), false},
		{"a value that is no GRN", "decl-a", false},
		{"a bot no live declaration places here", cluster.BotGRN(self.Organization, "decl-gone").String(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := newBotService()
			_, err := svc.MintBotToken(context.Background(), cluster.BotExchange{ActorToken: actorToken, Subject: tc.subject})
			assert.ErrorIs(t, err, cluster.ErrBotNotPlaced)
			assert.Equal(t, tc.reachesStore, len(store.recorded) == 1)
		})
	}
}

// TestMintBotToken_AuthenticatesTheActorBeforeAnythingElse holds that
// an actor token that is malformed is refused without a read, one that
// does not resolve is refused as every request's token is, and a scope
// is refused before any token is read.
func TestMintBotToken_AuthenticatesTheActorBeforeAnythingElse(t *testing.T) {
	svc, store := newBotService()
	_, err := svc.MintBotToken(context.Background(), cluster.BotExchange{ActorToken: "garam_ot_x", Subject: placedBot})
	assert.ErrorIs(t, err, cluster.ErrTokenInvalid)
	assert.Empty(t, store.resolved, "a malformed actor token is not looked up")

	_, err = svc.MintBotToken(context.Background(), cluster.BotExchange{
		ActorToken: cluster.TokenPrefix + strings.Repeat("cd", 32), Subject: placedBot})
	assert.ErrorIs(t, err, cluster.ErrTokenInvalid)
	assert.Empty(t, store.recorded)

	store.resolved = nil
	_, err = svc.MintBotToken(context.Background(), cluster.BotExchange{ActorToken: actorToken, Subject: placedBot, Scope: "tools:read"})
	assert.ErrorIs(t, err, cluster.ErrScopeUndefined)
	assert.Empty(t, store.resolved, "a scope is refused before the actor is read")
}

// TestMintBotToken_IsBoundedPerCluster holds BotTokensPerCluster.
func TestMintBotToken_IsBoundedPerCluster(t *testing.T) {
	svc, store := newBotService()
	exchange := cluster.BotExchange{ActorToken: actorToken, Subject: placedBot}
	for range cluster.BotTokensPerCluster {
		_, err := svc.MintBotToken(context.Background(), exchange)
		require.NoError(t, err)
	}
	_, err := svc.MintBotToken(context.Background(), exchange)
	var exceeded *operator.Exceeded
	assert.ErrorAs(t, err, &exceeded)
	assert.Len(t, store.recorded, cluster.BotTokensPerCluster, "the exchange past the bound draws nothing")
}
