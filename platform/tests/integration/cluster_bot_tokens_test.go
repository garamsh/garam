package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
	clusterpg "github.com/garamsh/garam/platform/internal/cluster/postgres"
)

// TestClusterBotTokens_OnlyALiveDeclarationOfTheRecordPlacesABot holds
// AUTH-5 at the store: a bot token is kept, as its hash, for a bot a
// live declaration of the record places, and for none placed on another
// record, tombstoned, or never declared.
func TestClusterBotTokens_OnlyALiveDeclarationOfTheRecordPlacesABot(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := clusterpg.New(p)
	records := declarationRecords(t, p, s, 2)
	g, other := records[0], records[1]
	for _, d := range []struct {
		on       grn.GRN
		id, name string
	}{{g, "live", "a"}, {g, "gone", "b"}, {other, "theirs", "c"}} {
		_, _, err := s.Declare(ctx, d.on, d.id, object(t, d.name))
		require.NoError(t, err)
	}
	_, err := s.Tombstone(ctx, g, "gone")
	require.NoError(t, err)

	expires, err := s.RecordBotToken(ctx, g, "live", cluster.HashSecret("garam_bt_live"), cluster.BotTokenTTL)
	require.NoError(t, err, "the control: a live declaration of the record")
	assert.WithinDuration(t, time.Now().Add(time.Hour), expires, time.Minute)
	var kept []string
	require.NoError(t, p.QueryRow(ctx, `SELECT array_agg(token_hash) FROM cluster_bot_tokens WHERE cluster_grn = $1
		AND declaration_id = 'live'`, g.String()).Scan(&kept))
	assert.Equal(t, []string{cluster.HashSecret("garam_bt_live")}, kept, "the hash is kept, never the value")

	for _, id := range []string{"gone", "theirs", "never"} {
		_, err := s.RecordBotToken(ctx, g, id, cluster.HashSecret("garam_bt_"+id), cluster.BotTokenTTL)
		assert.ErrorIs(t, err, cluster.ErrBotNotPlaced, id)
	}
	var count int
	require.NoError(t, p.QueryRow(ctx, `SELECT count(*) FROM cluster_bot_tokens`).Scan(&count))
	assert.Equal(t, 1, count, "no refused exchange kept anything")
}

// TestClusterBotTokens_AnExpiredTokenIsSweptAndTheRecordTakesItsTokens
// holds that a record's expired tokens go on its next exchange, and
// that deleting the record takes the rest.
func TestClusterBotTokens_AnExpiredTokenIsSweptAndTheRecordTakesItsTokens(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := clusterpg.New(p)
	g := declarationRecords(t, p, s, 1)[0]
	_, _, err := s.Declare(ctx, g, "live", object(t, "a"))
	require.NoError(t, err)
	_, err = s.RecordBotToken(ctx, g, "live", cluster.HashSecret("garam_bt_old"), cluster.BotTokenTTL)
	require.NoError(t, err)
	_, err = p.Exec(ctx, `UPDATE cluster_bot_tokens SET issued_at = issued_at - interval '2 hours',
		expires_at = expires_at - interval '2 hours'`)
	require.NoError(t, err)
	_, err = s.RecordBotToken(ctx, g, "live", cluster.HashSecret("garam_bt_new"), cluster.BotTokenTTL)
	require.NoError(t, err)
	hashes := func() []string {
		var out []string
		require.NoError(t, p.QueryRow(ctx, `SELECT coalesce(array_agg(token_hash), '{}') FROM cluster_bot_tokens`).Scan(&out))
		return out
	}
	assert.Equal(t, []string{cluster.HashSecret("garam_bt_new")}, hashes(), "the expired token was swept")

	require.NoError(t, s.Delete(ctx, g))
	assert.Empty(t, hashes(), "a deleted record takes its bots' tokens")
}

// TestClusterBotTokens_ADeclarationIDNamingAnAgentRecordIsRefused holds
// ADR-0189's shared identifier space: a declaration whose identifier
// names an agent record of the organization is refused, and writes
// nothing, while another identifier and the same one in another
// organization are the controls.
func TestClusterBotTokens_ADeclarationIDNamingAnAgentRecordIsRefused(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := clusterpg.New(p)
	g := declarationRecords(t, p, s, 1)[0]
	_, err := p.Exec(ctx, `INSERT INTO agents (grn, org_grn, provisioning_state, runtime_state, created_at, updated_at)
		VALUES ($1, $2, 'pending', 'unknown', now(), now())`,
		cluster.AgentGRN(g.Organization, "mira").String(), orgRootGRN(g.Organization).String())
	require.NoError(t, err)

	_, _, err = s.Declare(ctx, g, "mira", object(t, "mira"))
	assert.ErrorIs(t, err, cluster.ErrDeclarationIDHeldByAgent)
	_, err = s.GetDeclaration(ctx, g, "mira")
	assert.ErrorIs(t, err, cluster.ErrDeclarationNotFound, "the refused declaration wrote nothing")

	_, created, err := s.Declare(ctx, g, "reviewer", object(t, "mira"))
	require.NoError(t, err, "the control: an identifier no agent record holds")
	assert.True(t, created)
}
