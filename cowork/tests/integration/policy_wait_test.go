package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/mcp"
	"github.com/garamsh/garam/cowork/internal/policyfeed"
	feedgrpc "github.com/garamsh/garam/cowork/internal/policyfeed/grpc"
)

// waitRig is cowork's server whose creates wait on the policy feed
// (ADR-0166), over a policy service that is an in-process fake. The
// delivery loop is the one serve runs, except that it idles for an hour
// between passes: a pass runs when something wakes it, which only a
// request waiting on its entity does, so what has been delivered when
// a request answers is what that request waited for.
type waitRig struct {
	*stack
	feed *feedRig
}

// newWaitRig starts the feed as it is in a deployment, with live
// entities seeded before it starts.
func newWaitRig(t *testing.T, bound time.Duration, run bool, seed ...string) *waitRig {
	t.Helper()
	feed := newFeedRig(t)
	svc := policyfeed.NewService(feed.store, feedgrpc.NewSender(feed.client),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		policyfeed.Config{Poll: time.Hour, MaxBackoff: time.Hour})
	if run {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { _ = svc.Run(ctx); close(done) }()
		t.Cleanup(func() { cancel(); <-done })
	}
	s := newStackWith(t, policyfeed.NewGate(feed.store, svc, bound))
	for _, insert := range seed {
		// The creator is a GRN, which a route that reads the row parses.
		feed.exec(t, strings.Replace(insert, "'u'", "'grn:root/iam/user/ada'", 1), "acme", "abcdefghijkl")
	}
	_, err := feed.sw.Start(context.Background())
	require.NoError(t, err)
	return &waitRig{stack: s, feed: feed}
}

// delivered is the type and GRN of every entity the service was sent,
// in order.
func (r *waitRig) delivered() []string {
	var out []string
	for _, call := range r.feed.server.Applied() {
		for _, ch := range call.Request.GetChanges() {
			out = append(out, ch.GetEntity().GetType()+" "+ch.GetEntity().GetId())
		}
	}
	return out
}

// stored is the one identifier a table holds.
func (r *waitRig) stored(t *testing.T, table string) string {
	t.Helper()
	var id string
	require.NoError(t, r.pool.QueryRow(context.Background(), `SELECT id FROM `+table).Scan(&id))
	return id
}

// creation is a route that creates an entity: the request it takes,
// and what the service holds of it.
var creations = []struct {
	name, table, path, body, typ, grnType string
}{
	{"page", "pages", "/orgs/acme/pages", `{"title":"Home"}`, "Cowork::Page", "page"},
	{"channel", "channels", "/orgs/acme/channels", `{"kind":"public","name":"general"}`, "Cowork::Channel", "channel"},
	{"task", "tasks", "/orgs/acme/tasks", `{"title":"Follow up"}`, "Cowork::Task", "task"},
	{"artifact", "artifacts", "/orgs/acme/artifacts", `{"kind":"html","title":"Chart","html":"<p>v1</p>"}`, "Cowork::Artifact", "artifact"},
}

func TestPolicyWait_ACreateAnswersAfterTheServiceHoldsItsEntity(t *testing.T) {
	for _, c := range creations {
		t.Run(c.name, func(t *testing.T) {
			r := newWaitRig(t, policyfeed.Bound, true)

			status, body := r.call(t, "ada", http.MethodPost, c.path, c.body)

			require.Equal(t, http.StatusCreated, status, body)
			id := r.stored(t, c.table)
			assert.Equal(t, []string{c.typ + " grn:acme/cowork/" + c.grnType + "/" + id}, r.delivered())
			assert.Zero(t, r.feed.marks(t))
		})
	}
}

func TestPolicyWait_ACreateThroughATool_AnswersAfterTheServiceHoldsItsEntity(t *testing.T) {
	tools := []struct {
		name, table, typ, grnType string
		args                      map[string]any
	}{
		{"create_page", "pages", "Cowork::Page", "page", map[string]any{"org": "acme", "title": "Home"}},
		{"create_task", "tasks", "Cowork::Task", "task", map[string]any{"org": "acme", "title": "Follow up"}},
		{"create_artifact", "artifacts", "Cowork::Artifact", "artifact",
			map[string]any{"org": "acme", "kind": "html", "title": "Report", "html": "<h1>Q3</h1>"}},
	}
	for _, c := range tools {
		t.Run(c.name, func(t *testing.T) {
			r := newWaitRig(t, policyfeed.Bound, true)

			res := r.agent(t, "ada").call(c.name, c.args)

			require.False(t, res.IsError, textOf(res))
			id := r.stored(t, c.table)
			assert.Equal(t, []string{c.typ + " grn:acme/cowork/" + c.grnType + "/" + id}, r.delivered())
			assert.Zero(t, r.feed.marks(t))
		})
	}
}

func TestPolicyWait_AtTheBoundACreateIsAnsweredUnavailableNamingWhatItStored(t *testing.T) {
	for _, c := range creations {
		t.Run(c.name, func(t *testing.T) {
			r := newWaitRig(t, 50*time.Millisecond, false)

			status, body := r.call(t, "ada", http.MethodPost, c.path, c.body)

			id := r.stored(t, c.table)
			assert.Equal(t, http.StatusServiceUnavailable, status, body)
			assert.JSONEq(t, `{"kind":"unavailable","reason":"change_not_applied",
				"message":"the change is stored and the policy service has not yet applied it",
				"resource":"grn:acme/cowork/`+c.grnType+`/`+id+`"}`, body)
			assert.Equal(t, 1, r.feed.marks(t), "the entity is still marked, so the loop goes on delivering it")
		})
	}
}

func TestPolicyWait_AtTheBoundAToolIsAnsweredThatItStoredWhatItNames(t *testing.T) {
	r := newWaitRig(t, 50*time.Millisecond, false)

	res := r.agent(t, "ada").call("create_page", map[string]any{"org": "acme", "title": "Home"})

	require.True(t, res.IsError)
	assert.Equal(t, "unavailable", res.Meta[mcp.KindKey])
	assert.Equal(t, "create_page: created as grn:acme/cowork/page/"+r.stored(t, "pages")+
		", which may not be readable for a moment; read it, do not create it again", textOf(res))
}

// calling is call with a bound on how long the answer may take, so a
// request that waits fails the test and does not hold it.
func (r *waitRig) calling(t *testing.T, method, path, body string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, r.url+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "garam_session", Value: "ada"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "the request waited")
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestPolicyWait_AChangeThatFedNothingNewAnswersAtOnce(t *testing.T) {
	// The mark each of these raises is of an entity the service holds
	// already, and the bound is an hour: a request that waited on it
	// would not answer within the test.
	pageInsert, taskInsert := sources[1].insert, sources[2].insert
	cases := []struct {
		name, method, path, body string
		want                     int
	}{
		{"a page's retitling", http.MethodPatch, "/orgs/acme/pages/abcdefghijkl", `{"title":"New"}`, http.StatusOK},
		{"a task's update", http.MethodPatch, "/orgs/acme/tasks/abcdefghijkl", `{"status":"doing"}`, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newWaitRig(t, time.Hour, false, pageInsert, taskInsert)
			r.feed.deliver(t)

			got := r.calling(t, c.method, c.path, c.body)

			assert.Equal(t, c.want, got)
			assert.Equal(t, 1, r.feed.marks(t), "the change did raise a mark, which nothing waited on")
		})
	}
}

// deletedPages is the GRN of every page the service was told to
// drop, in order.
func (r *waitRig) deletedPages() []string {
	var out []string
	for _, call := range r.feed.server.Applied() {
		for _, ch := range call.Request.GetChanges() {
			if ch.GetDelete() != nil && ch.GetEntity().GetType() == "Cowork::Page" {
				out = append(out, ch.GetEntity().GetId())
			}
		}
	}
	return out
}

func TestPolicyWait_APageDeleteAnswersAfterTheServiceDropsIt(t *testing.T) {
	r := newWaitRig(t, policyfeed.Bound, true, sources[1].insert)
	r.feed.deliver(t) // the service holds the page before it is deleted
	require.Empty(t, r.deletedPages())

	status, body := r.call(t, "ada", http.MethodDelete, "/orgs/acme/pages/abcdefghijkl", "")

	require.Equal(t, http.StatusNoContent, status, body)
	assert.Equal(t, []string{"grn:acme/cowork/page/abcdefghijkl"}, r.deletedPages())
	assert.Zero(t, r.feed.marks(t))
}

func TestPolicyWait_AtTheBoundAPageDeleteIsAnsweredUnavailableNamingThePage(t *testing.T) {
	r := newWaitRig(t, 50*time.Millisecond, false, sources[1].insert)
	r.feed.deliver(t)

	status, body := r.call(t, "ada", http.MethodDelete, "/orgs/acme/pages/abcdefghijkl", "")

	assert.Equal(t, http.StatusServiceUnavailable, status, body)
	assert.JSONEq(t, `{"kind":"unavailable","reason":"change_not_applied",
		"message":"the change is stored and the policy service has not yet applied it",
		"resource":"grn:acme/cowork/page/abcdefghijkl"}`, body)
	assert.Equal(t, 1, r.feed.marks(t), "the page's deletion is still marked, so the loop goes on delivering it")
}

// A create that waited still answers the entity it created.
func TestPolicyWait_AWaitingCreateIsAnsweredTheEntityItCreated(t *testing.T) {
	r := newWaitRig(t, policyfeed.Bound, true)

	_, body := r.call(t, "ada", http.MethodPost, "/orgs/acme/pages", `{"title":"Home"}`)

	var p struct{ ID, Title string }
	require.NoError(t, json.Unmarshal([]byte(body), &p))
	assert.Equal(t, r.stored(t, "pages"), p.ID)
	assert.Equal(t, "Home", p.Title)
}
