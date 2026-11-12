package integration_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/policy"
	"github.com/garamsh/garam/cowork/internal/policy/policytest"
	"github.com/garamsh/garam/cowork/internal/policyfeed"
	feedgrpc "github.com/garamsh/garam/cowork/internal/policyfeed/grpc"
	feedpg "github.com/garamsh/garam/cowork/internal/policyfeed/postgres"
	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
)

// feedRig is the feed over real Postgres, delivering to a policy
// service and token endpoint that are in-process fakes.
type feedRig struct {
	pool     *pgxpool.Pool
	server   *policytest.Server
	endpoint *policytest.Endpoint
	client   *policy.Client
	store    *feedpg.Store
	sw       *feedpg.Switch
}

func newFeedRig(t *testing.T) *feedRig {
	t.Helper()
	p := pool(t)
	keyFile, pub := policytest.Key(t)
	endpoint := policytest.NewEndpoint(t, pub)
	server := policytest.Serve(t, endpoint)
	cfg := policy.Config{Addr: server.Addr, CAFile: server.CAFile, TokenURL: endpoint.URL, KeyFile: keyFile}
	tokens, err := policy.NewTokens(cfg, time.Now)
	require.NoError(t, err)
	client, err := policy.Dial(cfg, tokens)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return &feedRig{
		pool: p, server: server, endpoint: endpoint, client: client,
		store: feedpg.NewStore(p), sw: feedpg.NewSwitch(p),
	}
}

func (r *feedRig) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := r.pool.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

// deliver runs one pass through the real sender and client.
func (r *feedRig) deliver(t *testing.T) policyfeed.Result {
	t.Helper()
	return r.deliverVia(t, feedgrpc.NewSender(r.client))
}

func (r *feedRig) deliverVia(t *testing.T, sender policyfeed.Sender) policyfeed.Result {
	t.Helper()
	result, err := r.store.Deliver(context.Background(), policyfeed.BatchLimit, sender)
	require.NoError(t, err)
	return result
}

// generation is the mark an entity holds, 0 for none.
func (r *feedRig) generation(t *testing.T, typ, org, id string) int64 {
	t.Helper()
	var g int64
	err := r.pool.QueryRow(context.Background(),
		`SELECT coalesce(max(generation), 0) FROM policy_outbox WHERE entity_type = $1 AND org = $2 AND entity_id = $3`,
		typ, org, id).Scan(&g)
	require.NoError(t, err)
	return g
}

func (r *feedRig) marks(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, r.pool.QueryRow(context.Background(), `SELECT count(*) FROM policy_outbox`).Scan(&n))
	return n
}

// source is one table the feed watches: its entity type, and the
// statements that insert, change and delete one row of it.
type source struct {
	table, typ, insert, update, delete string
}

var sources = []source{
	{"channels", "channel",
		`INSERT INTO channels (org, id, kind, name, created_by, created_at) VALUES ($1, $2, 'public', 'general', 'u', now())`,
		`UPDATE channels SET name = 'renamed' WHERE org = $1 AND id = $2`,
		`DELETE FROM channels WHERE org = $1 AND id = $2`},
	{"pages", "page",
		`INSERT INTO pages (org, id, position, created_by, created_at, updated_at) VALUES ($1, $2, 0, 'u', now(), now())`,
		`UPDATE pages SET title = 'retitled' WHERE org = $1 AND id = $2`,
		`DELETE FROM pages WHERE org = $1 AND id = $2`},
	{"tasks", "task",
		`INSERT INTO tasks (org, id, title, status, created_by, created_at, updated_at) VALUES ($1, $2, 't', 'todo', 'u', now(), now())`,
		`UPDATE tasks SET status = 'done' WHERE org = $1 AND id = $2`,
		`DELETE FROM tasks WHERE org = $1 AND id = $2`},
	{"artifacts", "artifact",
		`INSERT INTO artifacts (org, id, kind, title, latest, created_by, created_at, updated_at) VALUES ($1, $2, 'html', 'a', 1, 'u', now(), now())`,
		`UPDATE artifacts SET title = 'b' WHERE org = $1 AND id = $2`,
		`DELETE FROM artifacts WHERE org = $1 AND id = $2`},
}

func TestPolicyFeed_EveryWriteToAWatchedTableMarksItsEntity(t *testing.T) {
	for _, src := range sources {
		t.Run(src.table, func(t *testing.T) {
			r := newFeedRig(t)
			_, err := r.sw.Start(context.Background())
			require.NoError(t, err)

			r.exec(t, src.insert, "acme", "abcdefghijkl")
			assert.EqualValues(t, 1, r.generation(t, src.typ, "acme", "abcdefghijkl"), "insert")

			r.deliver(t)
			require.Zero(t, r.marks(t), "a delivered mark is dropped")

			r.exec(t, src.update, "acme", "abcdefghijkl")
			assert.EqualValues(t, 1, r.generation(t, src.typ, "acme", "abcdefghijkl"), "update")

			r.deliver(t)
			r.exec(t, src.delete, "acme", "abcdefghijkl")
			assert.EqualValues(t, 1, r.generation(t, src.typ, "acme", "abcdefghijkl"), "delete")
		})
	}
}

func TestPolicyFeed_ADeletedPageMarksThePagesItsForeignKeyTook(t *testing.T) {
	r := newFeedRig(t)
	r.exec(t, sources[1].insert, "acme", "parentpageaa")
	r.exec(t, `INSERT INTO pages (org, id, parent_id, position, created_by, created_at, updated_at)
		VALUES ('acme', 'childpageaaa', 'parentpageaa', 0, 'u', now(), now())`)
	_, err := r.sw.Start(context.Background())
	require.NoError(t, err)
	r.deliver(t)

	r.exec(t, sources[1].delete, "acme", "parentpageaa")

	assert.Positive(t, r.generation(t, "page", "acme", "childpageaaa"))
}

func TestPolicyFeed_NothingIsMarkedWhileTheFeedIsOff(t *testing.T) {
	r := newFeedRig(t)
	for _, src := range sources {
		r.exec(t, src.insert, "acme", "abcdefghijkl")
	}

	assert.Zero(t, r.marks(t))
	assert.Empty(t, r.server.Applied())
	r.deliver(t)
	assert.Empty(t, r.server.Applied(), "an off feed delivers nothing")
}

func TestPolicyFeed_StartMarksEveryLiveEntityAndStopDropsThem(t *testing.T) {
	r := newFeedRig(t)
	for _, src := range sources {
		r.exec(t, src.insert, "acme", "abcdefghijkl")
	}

	marked, err := r.sw.Start(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 4, marked)
	on, err := r.sw.Enabled(context.Background())
	require.NoError(t, err)
	assert.True(t, on)

	removed, err := r.sw.Stop(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 4, removed)
	assert.Zero(t, r.marks(t))
}

func TestPolicyFeed_DeliversEachOrganizationUnderItsOwnToken(t *testing.T) {
	r := newFeedRig(t)
	r.exec(t, sources[1].insert, "acme", "abcdefghijkl")
	r.exec(t, sources[2].insert, "acme", "abcdefghijkl")
	r.exec(t, sources[1].insert, "zeta", "mnopqrstuvwx")
	_, err := r.sw.Start(context.Background())
	require.NoError(t, err)

	result := r.deliver(t)

	assert.Equal(t, 3, result.Marks)
	applied := r.server.Applied()
	require.Len(t, applied, 2, "one request per organization")
	seen := map[string]int{}
	for _, call := range applied {
		org, ok := r.endpoint.Org(call.Token)
		require.True(t, ok)
		assert.Equal(t, call.Org, org, "the token is the organization's own")
		for _, ch := range call.Request.GetChanges() {
			assert.True(t, strings.HasPrefix(ch.GetEntity().GetId(), "grn:"+org+"/cowork/"),
				"%s is sent under another organization's token", ch.GetEntity().GetId())
			up := ch.GetUpsert()
			require.NotNil(t, up)
			assert.Equal(t, "grn:root/iam/org/"+org, up.GetOrg().GetId())
			require.Len(t, up.GetParents(), 1)
			assert.Equal(t, "Iam::Org", up.GetParents()[0].GetType())
		}
		seen[org] = len(call.Request.GetChanges())
	}
	assert.Equal(t, map[string]int{"acme": 2, "zeta": 1}, seen)
	assert.Zero(t, r.marks(t))
}

func TestPolicyFeed_ADeletedEntityIsSentAsADelete(t *testing.T) {
	r := newFeedRig(t)
	_, err := r.sw.Start(context.Background())
	require.NoError(t, err)
	r.exec(t, sources[1].insert, "acme", "abcdefghijkl")
	r.deliver(t)
	r.exec(t, sources[1].delete, "acme", "abcdefghijkl")

	r.deliver(t)

	applied := r.server.Applied()
	require.Len(t, applied, 2)
	ch := applied[1].Request.GetChanges()[0]
	assert.Equal(t, "Cowork::Page", ch.GetEntity().GetType())
	assert.NotNil(t, ch.GetDelete())
	assert.Greater(t, ch.GetSequence(), applied[0].Request.GetChanges()[0].GetSequence())
}

// writingSender updates a page while the pass holds the entity's mark,
// as a request landing mid-pass does.
type writingSender struct {
	policyfeed.Sender
	write func()
}

func (s writingSender) Send(ctx context.Context, org string, seq int64, entries []policyfeed.Entry) ([]policyfeed.Refusal, error) {
	s.write()
	return s.Sender.Send(ctx, org, seq, entries)
}

func TestPolicyFeed_AWriteLandingMidPassSurvivesIt(t *testing.T) {
	r := newFeedRig(t)
	_, err := r.sw.Start(context.Background())
	require.NoError(t, err)
	r.exec(t, sources[1].insert, "acme", "abcdefghijkl")
	r.exec(t, sources[1].insert, "acme", "mnopqrstuvwx")

	r.deliverVia(t, writingSender{
		Sender: feedgrpc.NewSender(r.client),
		write:  func() { r.exec(t, sources[1].update, "acme", "abcdefghijkl") },
	})

	assert.EqualValues(t, 2, r.generation(t, "page", "acme", "abcdefghijkl"),
		"the page written mid-pass keeps its mark, at the generation that write made")
	assert.Zero(t, r.generation(t, "page", "acme", "mnopqrstuvwx"), "the page not written is delivered")
	r.deliver(t)
	assert.Zero(t, r.marks(t), "the next pass delivers the survivor")
}

// failingSender fails every send for one organization, as a token the
// platform no longer issues does.
type failingSender struct {
	policyfeed.Sender
	org string
}

func (s failingSender) Send(ctx context.Context, org string, seq int64, entries []policyfeed.Entry) ([]policyfeed.Refusal, error) {
	if org == s.org {
		return nil, errors.New("token refused")
	}
	return s.Sender.Send(ctx, org, seq, entries)
}

func TestPolicyFeed_AFailingOrganizationDoesNotHoldTheOthers(t *testing.T) {
	r := newFeedRig(t)
	_, err := r.sw.Start(context.Background())
	require.NoError(t, err)
	r.exec(t, sources[1].insert, "acme", "abcdefghijkl")
	r.exec(t, sources[1].insert, "acme", "mnopqrstuvwx")
	r.exec(t, sources[1].insert, "zeta", "yzabcdefghij")
	sender := failingSender{Sender: feedgrpc.NewSender(r.client), org: "zeta"}

	for pass := 1; pass <= 2; pass++ {
		result := r.deliverVia(t, sender)

		require.Len(t, result.Failed, 1, "pass %d", pass)
		assert.Equal(t, "zeta", result.Failed[0].Org)
		if pass == 1 {
			assert.Equal(t, []string{"acme"}, result.Delivered)
		}
		assert.Zero(t, r.generation(t, "page", "acme", "abcdefghijkl"), "acme's marks are delivered and gone")
		assert.Zero(t, r.generation(t, "page", "acme", "mnopqrstuvwx"))
		assert.EqualValues(t, 1, r.generation(t, "page", "zeta", "yzabcdefghij"), "zeta's mark stays")
	}
	assert.Len(t, r.server.Applied(), 1, "acme's run went in once; zeta's never did")

	assert.Empty(t, r.deliver(t).Failed, "zeta recovers when its send does")
	assert.Zero(t, r.marks(t))
}

func TestPolicyFeed_ARefusedEntityIsDeletedAtAHigherSequence(t *testing.T) {
	r := newFeedRig(t)
	const bad = "grn:acme/cowork/page/mnopqrstuvwx"
	r.server.Refuse = func(c *policyv1.EntityChange) string {
		if c.GetEntity().GetId() == bad && c.GetUpsert() != nil {
			return "the service will not take this page"
		}
		return ""
	}
	_, err := r.sw.Start(context.Background())
	require.NoError(t, err)
	r.exec(t, sources[1].insert, "acme", "abcdefghijkl")
	r.exec(t, sources[1].insert, "acme", "mnopqrstuvwx")

	result := r.deliver(t)

	require.Len(t, result.Refused, 1)
	assert.Equal(t, "mnopqrstuvwx", result.Refused[0].Entity.ID)
	applied := r.server.Applied()
	require.Len(t, applied, 2, "the batch, then the refused entity's delete")
	first, second := applied[0].Request.GetChanges(), applied[1].Request.GetChanges()
	require.Len(t, second, 1)
	assert.Equal(t, bad, second[0].GetEntity().GetId())
	assert.NotNil(t, second[0].GetDelete())
	assert.Greater(t, second[0].GetSequence(), first[0].GetSequence())
	assert.Zero(t, r.marks(t), "both entities' marks are dropped")
}
