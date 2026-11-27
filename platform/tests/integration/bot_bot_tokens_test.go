package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	apppg "github.com/garamsh/garam/platform/internal/app/postgres"
	"github.com/garamsh/garam/platform/internal/bot"
	botpg "github.com/garamsh/garam/platform/internal/bot/postgres"
)

// actor is a connected record with a live key and a token exchanged
// under it, as a connector holds after AUTH-1.
type actor struct {
	g     grn.GRN
	kid   string
	token string
}

// connectedActors registers n records of acme, each under its own key,
// and exchanges a token for each.
func connectedActors(t *testing.T, p *pgxpool.Pool, n int) []actor {
	t.Helper()
	ctx := context.Background()
	s := apppg.New(p)
	keys := connectorKeys(t)
	records := declarationRecords(t, p, n)
	out := make([]actor, 0, n)
	for i, g := range records {
		require.NoError(t, s.BindKey(ctx, g, keys[i]))
		token := &app.Token{Instance: g, Value: "garam_ct_actor-" + g.Identifier}
		require.NoError(t, s.RecordAssertionToken(ctx, signed(t, g, liveKey(t, s, g, keys[i]).ID, "jti-"+g.Identifier), token))
		out = append(out, actor{g: g, kid: keys[i].KID(), token: token.Value})
	}
	return out
}

// mint records a bot token named value for id through a's token.
func (a actor) mint(ctx context.Context, s *botpg.Store, id, value string) error {
	_, err := s.RecordBotToken(ctx, a.g, id, app.HashSecret(a.token), app.HashSecret(value), bot.BotTokenTTL)
	return err
}

// botTokenHashes answers the hashes bot_tokens holds.
func botTokenHashes(t *testing.T, p *pgxpool.Pool) []string {
	t.Helper()
	var out []string
	require.NoError(t, p.QueryRow(context.Background(),
		`SELECT coalesce(array_agg(token_hash ORDER BY token_hash), '{}') FROM bot_tokens`).Scan(&out))
	return out
}

// workspaceClass is an interface-1 WorkspaceClass named name, a kind
// that places no bot.
func workspaceClass(t *testing.T, name string) bot.DeclaredObject {
	t.Helper()
	o, err := bot.ParseDeclaredObject([]byte(`{"apiVersion":"bot.garam.sh/v1alpha1","kind":"WorkspaceClass",
		"metadata":{"namespace":"team-a","name":"` + name + `"},
		"spec":{"provider":"mvmd","resources":{"cpu":2,"memory":"4Gi","disk":"20Gi"}}}`))
	require.NoError(t, err)
	return o
}

// TestBotTokens_OnlyALiveBotDeclarationOfTheRecordPlacesABot
// holds AUTH-5 at the store: a bot token is kept, as its hash, for a
// bot a live Bot declaration of the record places, and for none placed
// on another record, tombstoned, never declared, or declared as another
// kind.
func TestBotTokens_OnlyALiveBotDeclarationOfTheRecordPlacesABot(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	actors := connectedActors(t, p, 2)
	a, other := actors[0], actors[1]
	for _, d := range []struct {
		on     grn.GRN
		id     string
		object bot.DeclaredObject
	}{
		{a.g, "live", object(t, "a")}, {a.g, "gone", object(t, "b")}, {other.g, "theirs", object(t, "c")},
		{a.g, "wsclass", workspaceClass(t, "small")},
	} {
		_, _, err := s.Declare(ctx, d.on, d.id, d.object)
		require.NoError(t, err)
	}
	_, err := s.Tombstone(ctx, a.g, "gone")
	require.NoError(t, err)

	require.NoError(t, a.mint(ctx, s, "live", "garam_bt_live"), "the control: a live Bot declaration of the record")
	var kept struct {
		declaration string
		key         int64
	}
	require.NoError(t, p.QueryRow(ctx, `SELECT declaration_id, key_id FROM bot_tokens WHERE token_hash = $1`,
		app.HashSecret("garam_bt_live")).Scan(&kept.declaration, &kept.key))
	assert.Equal(t, "live", kept.declaration, "the hash is kept, never the value")
	assert.Equal(t, liveKey(t, apppg.New(p), a.g, connectorKeys(t)[0]).ID, kept.key, "under the key the actor's token rests on")

	for _, id := range []string{"gone", "theirs", "never", "wsclass"} {
		assert.ErrorIs(t, a.mint(ctx, s, id, "garam_bt_"+id), bot.ErrBotNotPlaced, id)
	}
	assert.Equal(t, []string{app.HashSecret("garam_bt_live")}, botTokenHashes(t, p), "no refused exchange kept anything")
}

// TestBotTokens_ATokenGoesWithWhatRevokesIt holds that a bot
// token goes in the transaction that tombstones its declaration,
// revokes the connector key it was minted under, re-registers its
// record or deletes it, and that an actor whose key is revoked mints
// nothing. Each step leaves the other tokens, the control.
func TestBotTokens_ATokenGoesWithWhatRevokesIt(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	actors := connectedActors(t, p, 2)
	a, other := actors[0], actors[1]
	for _, d := range []struct {
		on       actor
		id, name string
	}{{a, "first", "a"}, {a, "second", "b"}, {other, "third", "c"}, {other, "fourth", "d"}} {
		_, _, err := s.Declare(ctx, d.on.g, d.id, object(t, d.name))
		require.NoError(t, err)
		require.NoError(t, d.on.mint(ctx, s, d.id, "garam_bt_"+d.id))
	}
	hashes := func(values ...string) []string {
		out := make([]string, 0, len(values))
		for _, v := range values {
			out = append(out, app.HashSecret("garam_bt_"+v))
		}
		assert.ElementsMatch(t, out, botTokenHashes(t, p))
		return out
	}
	hashes("first", "second", "third", "fourth")

	_, err := s.Tombstone(ctx, a.g, "first")
	require.NoError(t, err)
	hashes("second", "third", "fourth")

	_, _, err = s.Declare(ctx, a.g, "second", workspaceClass(t, "small"))
	require.NoError(t, err)
	hashes("third", "fourth")

	_, _, err = s.Declare(ctx, a.g, "fifth", object(t, "e"))
	require.NoError(t, err)
	require.NoError(t, a.mint(ctx, s, "fifth", "garam_bt_fifth"))
	require.NoError(t, other.mint(ctx, s, "third", "garam_bt_again"))
	require.NoError(t, apppg.New(p).RevokeKey(ctx, other.g, other.kid))
	hashes("fifth")
	assert.ErrorIs(t, other.mint(ctx, s, "fourth", "garam_bt_late"), app.ErrTokenInvalid,
		"an actor whose key is revoked mints nothing")

	require.NoError(t, apppg.New(p).BindKey(ctx, a.g, connectorKeys(t)[2]))
	hashes()
}

// TestBotTokens_AnActorWhoseKeyRetiredMintsNothing holds that the
// token is written only under a live key, read in the writing
// transaction: an actor token whose key's rollover has passed, though
// the token itself has not expired, mints nothing.
func TestBotTokens_AnActorWhoseKeyRetiredMintsNothing(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	a := connectedActors(t, p, 1)[0]
	_, _, err := s.Declare(ctx, a.g, "live", object(t, "a"))
	require.NoError(t, err)
	require.NoError(t, a.mint(ctx, s, "live", "garam_bt_before"), "the control: the key is live")

	tag, err := p.Exec(ctx, `UPDATE instance_keys SET retires_at = now() - interval '1 second' WHERE instance_grn = $1`, a.g.String())
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected())
	assert.ErrorIs(t, a.mint(ctx, s, "live", "garam_bt_after"), app.ErrTokenInvalid)
	assert.Equal(t, []string{app.HashSecret("garam_bt_before")}, botTokenHashes(t, p))
}

// TestBotTokens_AnExpiredTokenIsSweptAndTheRecordTakesItsTokens
// holds that a record's expired tokens go on its next exchange, and
// that deleting the record takes the rest.
func TestBotTokens_AnExpiredTokenIsSweptAndTheRecordTakesItsTokens(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	a := connectedActors(t, p, 1)[0]
	_, _, err := s.Declare(ctx, a.g, "live", object(t, "a"))
	require.NoError(t, err)
	require.NoError(t, a.mint(ctx, s, "live", "garam_bt_old"))
	_, err = p.Exec(ctx, `UPDATE bot_tokens SET issued_at = issued_at - interval '2 hours',
		expires_at = expires_at - interval '2 hours'`)
	require.NoError(t, err)
	require.NoError(t, a.mint(ctx, s, "live", "garam_bt_new"))
	assert.Equal(t, []string{app.HashSecret("garam_bt_new")}, botTokenHashes(t, p), "the expired token was swept")

	require.NoError(t, apppg.New(p).DeleteInstance(ctx, a.g))
	assert.Empty(t, botTokenHashes(t, p), "a deleted record takes its bots' tokens")
}

// TestBotTokens_ADeclarationIDNamingAnAgentRecordIsRefused holds
// ADR-0189's shared identifier space: a declaration whose identifier
// names an agent record of the organization is refused, and writes
// nothing, while another identifier is the control.
func TestBotTokens_ADeclarationIDNamingAnAgentRecordIsRefused(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	g := declarationRecords(t, p, 1)[0]
	_, err := p.Exec(ctx, `INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
		VALUES ($1, $2, 'pending', 'unknown', now(), now())`,
		bot.AgentGRN(g.Organization, "mira").String(), orgRootGRN(g.Organization).String())
	require.NoError(t, err)

	_, _, err = s.Declare(ctx, g, "mira", object(t, "mira"))
	assert.ErrorIs(t, err, bot.ErrDeclarationIDHeldByAgent)
	_, err = s.GetDeclaration(ctx, g, "mira")
	assert.ErrorIs(t, err, bot.ErrDeclarationNotFound, "the refused declaration wrote nothing")

	_, created, err := s.Declare(ctx, g, "reviewer", object(t, "mira"))
	require.NoError(t, err, "the control: an identifier no agent record holds")
	assert.True(t, created)
}

// TestBotTokens_AWithdrawalOfBotHostingRevokesTheInstallationsTokens
// holds ADR-0192 decision 10's withdrawal at the store: every bot token
// minted for an instance of the installation goes, so a token minted
// before the withdrawal is held by nothing afterwards. Revoking another
// installation's tokens is the control, and takes none of them.
func TestBotTokens_AWithdrawalOfBotHostingRevokesTheInstallationsTokens(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	actors := connectedActors(t, p, 2)
	for i, a := range actors {
		id := "bot-" + string(rune('a'+i))
		_, _, err := s.Declare(ctx, a.g, id, object(t, id))
		require.NoError(t, err)
		require.NoError(t, a.mint(ctx, s, id, "garam_bt_"+id))
	}
	require.Len(t, botTokenHashes(t, p), 2)

	other := grn.New("acme", app.InstallationType, "deployer")
	require.NoError(t, s.RevokeInstallationBotTokens(ctx, other))
	assert.Len(t, botTokenHashes(t, p), 2, "the control: another installation's withdrawal takes none")

	installation := grn.New("acme", app.InstallationType, "garam-bot-connector")
	require.NoError(t, s.RevokeInstallationBotTokens(ctx, installation))
	assert.Empty(t, botTokenHashes(t, p), "every token of the installation's instances goes")
}
