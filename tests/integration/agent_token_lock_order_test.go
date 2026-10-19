package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// agentTokenRow is an agent token a test writes directly for the
// fixture's agent: the audience it names if any, and when it was issued
// and expires, as SQL intervals from now.
type agentTokenRow struct {
	hash                string
	audience            *grn.GRN
	issuedAt, expiresAt string
}

// insertAgentTokenRow records row for f's agent at its assignee and
// epoch now.
func insertAgentTokenRow(t *testing.T, f fence, row agentTokenRow) {
	t.Helper()
	var serial [16]byte
	_, err := rand.Read(serial[:])
	require.NoError(t, err)
	var audience *string
	if row.audience != nil {
		a := row.audience.String()
		audience = &a
	}
	_, err = f.p.Exec(context.Background(), `INSERT INTO agent_tokens
		(token_hash, serial, agent_grn, assignee_grn, epoch, audience_grn, issued_at, expires_at)
		SELECT $1, $2, a.agent_grn, a.assignee_grn, a.epoch, $3, now() + $4::interval, now() + $5::interval
		FROM agent_assignments a WHERE a.agent_grn = $6`,
		row.hash, "t"+hex.EncodeToString(serial[:]), audience, row.issuedAt, row.expiresAt, f.agent.String())
	require.NoError(t, err)
}

// requireAgentTokenHeapOrder fails unless the agent tokens sit in the
// heap in the order given.
func requireAgentTokenHeapOrder(t *testing.T, p *pgxpool.Pool, hashes ...string) {
	t.Helper()
	var ordered bool
	require.NoError(t, p.QueryRow(context.Background(),
		`SELECT array_agg(token_hash ORDER BY ctid) = $1 FROM agent_tokens WHERE token_hash = ANY($1)`, hashes).Scan(&ordered))
	require.True(t, ordered, "the tokens sit in the heap in the order %v", hashes)
}

// mintFor mints a token of f's agent by its controller through agents,
// for audience, and answers it with the mint's error.
func mintFor(ctx context.Context, agents *agent.PostgresStore, f fence, audience *grn.GRN) (*agent.AccessToken, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	now := time.Now()
	token := &agent.AccessToken{Value: agent.AccessTokenPrefix + hex.EncodeToString(b[:]), Agent: f.agent,
		Audience: audience, IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	return token, agents.MintAccessToken(ctx, f.controller, token)
}

// minted is what a mint under test answered.
type minted struct {
	token *agent.AccessToken
	err   error
}

// TestAgentTokenLockOrder_AnAudiencesDeletionAndAMintSweepingTokensNamingItBothComplete
// is #1434's audience cascade against a mint. Deleting the audience
// took the agent's tokens naming it in heap order, holding only the
// audience's registration; the agent's mint swept its expired tokens in
// issue order; two expired tokens naming the audience sit in opposite
// orders in the two. Parked between them, each needed the token the
// other held. Both now take them in token_hash order.
func TestAgentTokenLockOrder_AnAudiencesDeletionAndAMintSweepingTokensNamingItBothComplete(t *testing.T) {
	ctx := context.Background()
	f, _ := newLineageFence(t)
	p := indexScanPool(t)
	agents := agent.NewPostgresStore(p)
	audience := registerOperator(t, f.p, scopedGRN("acme", agent.ServiceTypeOperator, "hosted"))
	// Heap order, which a scan for the audience's tokens takes, is late,
	// park-deletion, early. Issue order, which the mint's sweep takes,
	// is early, park-mint, late.
	for _, row := range []agentTokenRow{
		{hash: "late", audience: &audience, issuedAt: "-2 hours", expiresAt: "-1 hour"},
		{hash: "park-deletion", audience: &audience, issuedAt: "-1 hour", expiresAt: "1 hour"},
		{hash: "early", audience: &audience, issuedAt: "-4 hours", expiresAt: "-3 hours"},
		{hash: "park-mint", issuedAt: "-3 hours", expiresAt: "-2 hours"},
	} {
		insertAgentTokenRow(t, f, row)
	}
	requireAgentTokenHeapOrder(t, f.p, "late", "park-deletion", "early")
	requirePlanUses(t, p, "agent_tokens_agent_grn_issued_at_idx",
		`DELETE FROM agent_tokens WHERE agent_grn = $1 AND expires_at <= now()`, f.agent.String())

	parked, err := f.p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = parked.Rollback(ctx) }()
	_, err = parked.Exec(ctx, `SELECT 1 FROM agent_tokens WHERE token_hash = ANY($1) FOR UPDATE`,
		[]string{"park-deletion", "park-mint"})
	require.NoError(t, err)
	deleted := make(chan error, 1)
	go func() { deleted <- operator.NewPostgresStore(p).Delete(ctx, audience) }()
	awaitSettled(t, f.p, 1, returned(deleted))
	mints := make(chan minted, 1)
	go func() {
		token, err := mintFor(ctx, agents, f, nil)
		mints <- minted{token, err}
	}()
	awaitSettled(t, f.p, 2, returned(deleted), returned(mints))
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, deleted), "the audience's deletion completes")
	done := receive(t, mints)
	require.NoError(t, done.err, "the mint completes rather than deadlocking")
	assert.Equal(t, 0, countRows(t, f.p, `SELECT count(*) FROM agent_tokens WHERE audience_grn = $1`, audience.String()))
	_, err = f.agents.ResolveAccessToken(ctx, done.token.Hash())
	assert.NoError(t, err, "the control: the minted token resolves")
}

// TestAgentTokenLockOrder_AnAudiencesDeletionAndAMintNamingItBothComplete
// is the audience cascade against a mint naming that audience. The mint
// swept an expired token naming the audience and only then, at its
// insert's foreign key, waited on the audience's registration, which
// the deletion held while its cascade headed for the swept token. The
// mint now writes its token before it deletes any, so it waits on the
// registration holding none, and finds its audience gone.
func TestAgentTokenLockOrder_AnAudiencesDeletionAndAMintNamingItBothComplete(t *testing.T) {
	ctx := context.Background()
	f, _ := newLineageFence(t)
	audience := registerOperator(t, f.p, scopedGRN("acme", agent.ServiceTypeOperator, "hosted"))
	// a-park comes first in heap order and in token_hash order, so the
	// deletion reaches it before b-swept either way.
	for _, row := range []agentTokenRow{
		{hash: "a-park", audience: &audience, issuedAt: "-1 hour", expiresAt: "1 hour"},
		{hash: "b-swept", audience: &audience, issuedAt: "-3 hours", expiresAt: "-2 hours"},
	} {
		insertAgentTokenRow(t, f, row)
	}
	requireAgentTokenHeapOrder(t, f.p, "a-park", "b-swept")

	parked, err := f.p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = parked.Rollback(ctx) }()
	_, err = parked.Exec(ctx, `SELECT 1 FROM agent_tokens WHERE token_hash = 'a-park' FOR UPDATE`)
	require.NoError(t, err)
	deleted := make(chan error, 1)
	go func() { deleted <- operator.NewPostgresStore(f.p).Delete(ctx, audience) }()
	awaitSettled(t, f.p, 1, returned(deleted))
	mints := make(chan minted, 1)
	go func() {
		token, err := mintFor(ctx, f.agents, f, &audience)
		mints <- minted{token, err}
	}()
	awaitSettled(t, f.p, 2, returned(deleted), returned(mints))
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, deleted), "the audience's deletion completes rather than deadlocking")
	assert.ErrorIs(t, receive(t, mints).err, agent.ErrAccessTokenAudience,
		"the mint waited for the deletion and found its audience gone")
	assert.Equal(t, 0, countRows(t, f.p, `SELECT count(*) FROM agent_tokens WHERE audience_grn = $1`, audience.String()))
}

// TestAgentTokenLockOrder_AnOrganizationDeletionAndAMintNamingAnAudienceBothComplete
// is a mint naming an audience against its organization's deletion. The
// mint holds its agent's execution row and then, at its insert's
// foreign key, waits on the audience's registration. 000046 had the
// deletion hold every operator's registration before its cascade
// reached the agent's execution, the opposite order. The deletion now
// holds the agents' execution rows before the operators', so the mint
// waits for it holding nothing and finds its agent gone.
func TestAgentTokenLockOrder_AnOrganizationDeletionAndAMintNamingAnAudienceBothComplete(t *testing.T) {
	ctx := context.Background()
	f, _ := newLineageFence(t)
	audience := registerOperator(t, f.p, scopedGRN("acme", agent.ServiceTypeOperator, "hosted"))

	parked, err := f.p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = parked.Rollback(ctx) }()
	_, err = parked.Exec(ctx, `SELECT grn FROM operators WHERE grn = $1 FOR UPDATE`, audience.String())
	require.NoError(t, err)
	deleted := make(chan error, 1)
	go func() { deleted <- orgpg.New(f.p).DeleteOrg(ctx, orgRootGRN("acme")) }()
	awaitSettled(t, f.p, 1, returned(deleted))
	mints := make(chan minted, 1)
	go func() {
		token, err := mintFor(ctx, f.agents, f, &audience)
		mints <- minted{token, err}
	}()
	awaitSettled(t, f.p, 2, returned(deleted), returned(mints))
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, deleted), "the organization's deletion completes rather than deadlocking")
	assert.ErrorIs(t, receive(t, mints).err, agent.ErrNotAssigned,
		"the mint waited for the deletion and found its agent gone")
	assert.Equal(t, 0, countRows(t, f.p, `SELECT count(*) FROM agent_tokens`))
}

// TestAgentTokenLockOrder_AnAudiencesDeletionAndAMintTrimmingTokensNamingItBothComplete
// holds a mint's trim to the token_hash order the audience's deletion
// takes the tokens naming it in (000047). The trim reaches the agent's
// expired tokens by issue order through the agent's index; two of them
// naming the audience sit in that order opposite to their token_hash
// order, either side of the deletion's parked token, so a trim walking
// the index would hold the one the deletion needs next.
func TestAgentTokenLockOrder_AnAudiencesDeletionAndAMintTrimmingTokensNamingItBothComplete(t *testing.T) {
	ctx := context.Background()
	f, _ := newLineageFence(t)
	p := indexScanPool(t)
	agents := agent.NewPostgresStore(p)
	audience := registerOperator(t, f.p, scopedGRN("acme", agent.ServiceTypeOperator, "hosted"))
	// Issue order, which the agent's index walks, is c-late, park-mint,
	// a-early; token_hash order is a-early, b-park, c-late.
	for _, row := range []agentTokenRow{
		{hash: "c-late", audience: &audience, issuedAt: "-4 hours", expiresAt: "-3 hours"},
		{hash: "park-mint", issuedAt: "-3 hours", expiresAt: "-2 hours"},
		{hash: "a-early", audience: &audience, issuedAt: "-2 hours", expiresAt: "-1 hour"},
		{hash: "b-park", audience: &audience, issuedAt: "-1 hour", expiresAt: "1 hour"},
	} {
		insertAgentTokenRow(t, f, row)
	}
	requirePlanUses(t, p, "agent_tokens_agent_grn_issued_at_idx",
		`SELECT token_hash FROM agent_tokens WHERE agent_grn = $1 FOR UPDATE`, f.agent.String())

	parked, err := f.p.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = parked.Rollback(ctx) }()
	_, err = parked.Exec(ctx, `SELECT 1 FROM agent_tokens WHERE token_hash = ANY($1) FOR UPDATE`,
		[]string{"b-park", "park-mint"})
	require.NoError(t, err)
	deleted := make(chan error, 1)
	go func() { deleted <- operator.NewPostgresStore(f.p).Delete(ctx, audience) }()
	awaitSettled(t, f.p, 1, returned(deleted))
	mints := make(chan minted, 1)
	go func() {
		token, err := mintFor(ctx, agents, f, nil)
		mints <- minted{token, err}
	}()
	awaitSettled(t, f.p, 2, returned(deleted), returned(mints))
	require.NoError(t, parked.Rollback(ctx))

	require.NoError(t, receive(t, deleted), "the audience's deletion completes")
	done := receive(t, mints)
	require.NoError(t, done.err, "the mint completes rather than deadlocking")
	assert.Equal(t, 0, countRows(t, f.p, `SELECT count(*) FROM agent_tokens WHERE expires_at <= now()`),
		"the trim took every expired token the deletion left")
	_, err = f.agents.ResolveAccessToken(ctx, done.token.Hash())
	assert.NoError(t, err, "the control: the minted token resolves")
}
