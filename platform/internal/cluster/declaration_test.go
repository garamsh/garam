package cluster_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/cluster"
	"github.com/garamsh/garam/platform/internal/operator"
)

// bot is a declared object DECL-7 to DECL-9 admit; each case of the
// refusal table below changes one member of it.
const bot = `{"apiVersion":"garam.sh/v1alpha1","kind":"Bot",
	"metadata":{"namespace":"team-a","name":"reviewer","labels":{"tier":"one"}},
	"spec":{"replicas":12345678901234567890,"tokenSecretRef":"bot-token/value","nested":[{"apiKeySecret":"keys/api"}]}}`

func TestParseDeclaredObject_KeepsDECL7sMembersAndEveryNumberAsWritten(t *testing.T) {
	o, err := cluster.ParseDeclaredObject([]byte(strings.Replace(bot, `"kind":"Bot",`,
		`"kind":"Bot","status":{"ready":true},`, 1)))
	require.NoError(t, err)
	assert.Equal(t, "Bot", o.Kind())
	assert.Equal(t, "team-a", o.Namespace())
	assert.Equal(t, "reviewer", o.Name())
	var kept map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(o.JSON(), &kept))
	assert.NotContains(t, kept, "status", "a member DECL-7 does not name is dropped")
	assert.Contains(t, string(kept["spec"]), "12345678901234567890", "a number is not rounded through float64")
	assert.Contains(t, string(kept["metadata"]), `"labels":{"tier":"one"}`)
}

func TestParseDeclaredObject_RefusesWhatDECL7To9Refuse(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
	}{
		{"no apiVersion", `"apiVersion":"garam.sh/v1alpha1",`, ``},
		{"a kind not declared", `"kind":"Bot"`, `"kind":"Deployment"`},
		{"a namespace that is no label", `"namespace":"team-a"`, `"namespace":"Team.A"`},
		{"an empty name", `"name":"reviewer"`, `"name":""`},
		{"a name that is no subdomain", `"name":"reviewer"`, `"name":"Reviewer_1"`},
		{"no spec", `"spec":{"replicas"`, `"other":{"replicas"`},
		{"a spec that is null", `"spec":{"replicas":12345678901234567890,"tokenSecretRef":"bot-token/value","nested":[{"apiKeySecret":"keys/api"}]}`, `"spec":null`},
		{"an image (DECL-9)", `"replicas"`, `"image":"x","replicas"`},
		{"an image list (DECL-9)", `"replicas"`, `"Images":[],"replicas"`},
		{"a nested image (DECL-9)", `{"apiKeySecret":"keys/api"}`, `{"sidecarImage":"x"}`},
		{"a secret's value (DECL-8)", `"bot-token/value"`, `"hunter2"`},
		{"a secret that is no string (DECL-8)", `"keys/api"`, `{"name":"keys","key":"api"}`},
		{"a secret with no key (DECL-8)", `"bot-token/value"`, `"bot-token/"`},
		{"not an object", bot, `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.Replace(bot, tc.from, tc.to, 1)
			require.NotEqual(t, bot, raw, "the case changes the object")
			_, err := cluster.ParseDeclaredObject([]byte(raw))
			assert.Equal(t, errorx.KindInvalidArgument, errorx.KindOf(err), "%v", err)
		})
	}
	_, err := cluster.ParseDeclaredObject([]byte(bot))
	assert.NoError(t, err, "the control: the unchanged object is admitted")
}

func TestParseDeclaredObject_RefusesAnObjectOverItsBound(t *testing.T) {
	big := strings.Replace(bot, `"replicas"`, `"note":"`+strings.Repeat("x", cluster.DeclaredObjectMaxBytes)+`","replicas"`, 1)
	_, err := cluster.ParseDeclaredObject([]byte(big))
	assert.Equal(t, errorx.KindInvalidArgument, errorx.KindOf(err))
}

func TestCheckDeclarationID_TakesWhatAChangeCarries(t *testing.T) {
	drawn, err := cluster.NewDeclarationID()
	require.NoError(t, err)
	for _, id := range []string{drawn, "a", "Bot.reviewer_1-x", strings.Repeat("a", 128)} {
		assert.NoError(t, cluster.CheckDeclarationID(id), id)
	}
	for _, id := range []string{"", strings.Repeat("a", 129), "a/b", "a b", "ü"} {
		assert.Error(t, cluster.CheckDeclarationID(id), id)
	}
}

func TestParseCursor_ReadsOnlyThisRecordsLiveCursors(t *testing.T) {
	at := cluster.Position{Head: 7, PurgedThrough: 2}
	full, err := cluster.ParseCursor(nil, at)
	require.NoError(t, err)
	assert.Equal(t, cluster.Cursor{Full: true, Base: 7, LastSent: -1}, full, "an absent cursor starts a full set at the head")
	for raw, want := range map[string]cluster.Cursor{
		"c.3":    {Base: 3, LastSent: 3},
		"c.7":    {Base: 7, LastSent: 7},
		"f.5.6":  {Full: true, Base: 5, LastSent: 6},
		"f.5.-1": {Full: true, Base: 5, LastSent: -1},
	} {
		got, err := cluster.ParseCursor(&raw, at)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
		assert.Equal(t, raw, got.String(), "a cursor reads back as written")
	}
	for _, raw := range []string{"c", "c.", "c.x", "c.8", "c.-2", "f.5", "f.5.6.7", "x.3", "c.3.4"} {
		_, err := cluster.ParseCursor(&raw, at)
		assert.ErrorIs(t, err, cluster.ErrCursorInvalid, raw)
	}
	for _, raw := range []string{"c.2", "c.0", "f.2.5"} {
		_, err := cluster.ParseCursor(&raw, at)
		assert.ErrorIs(t, err, cluster.ErrCursorExpired, raw)
	}
}

func apply(seq int64, id string) cluster.Entry {
	return cluster.Entry{Seq: seq, Change: cluster.Change{Op: cluster.OpApply, ID: id, Generation: 1, Object: json.RawMessage(`{}`)}}
}

func TestLogPage_AnswersAtMostAPageAndTheCursorAfterItsLast(t *testing.T) {
	at := cluster.Cursor{Base: 4, LastSent: 4}
	page := cluster.LogPage(at, []cluster.Entry{apply(5, "a"), apply(6, "b"), apply(7, "c")}, 2)
	assert.True(t, page.More)
	assert.Equal(t, "c.6", page.Cursor)
	assert.Len(t, page.Changes, 2)

	page = cluster.LogPage(at, []cluster.Entry{apply(5, "a"), apply(6, "b")}, 2)
	assert.False(t, page.More, "exactly a page leaves none")
	assert.Equal(t, "c.6", page.Cursor)

	page = cluster.LogPage(at, nil, 2)
	assert.Equal(t, "c.4", page.Cursor, "an empty page keeps the cursor it was read after")
	assert.NotNil(t, page.Changes, "changes is required, so an empty page carries []")
}

func TestFullPage_GoesOnFromItsBaseOnceTheSetIsSent(t *testing.T) {
	at := cluster.Cursor{Full: true, Base: 9, LastSent: -1}
	page := cluster.FullPage(at, []cluster.Entry{apply(2, "a"), apply(8, "b"), apply(9, "c")}, 2)
	assert.True(t, page.Full)
	assert.True(t, page.More)
	assert.Equal(t, "f.9.8", page.Cursor, "the next page reads after the last one sent")

	at = cluster.Cursor{Full: true, Base: 9, LastSent: 8}
	page = cluster.FullPage(at, []cluster.Entry{apply(9, "c")}, 2)
	assert.True(t, page.Full)
	assert.False(t, page.More)
	assert.Equal(t, "c.9", page.Cursor, "the last page answers the log cursor at the set's base")

	page = cluster.FullPage(cluster.Cursor{Full: true, Base: 0, LastSent: -1}, nil, 2)
	assert.True(t, page.Full, "a record with nothing declared answers an empty full set")
	assert.Equal(t, "c.0", page.Cursor)
}

func TestWakes_KeepsOneWakeForAPollBetweenItsReadAndItsWait(t *testing.T) {
	wakes := cluster.NewWakes()
	other := grn.New("acme", cluster.ServiceType, "0000000000000000")
	woken, stop := wakes.Subscribe(self)
	defer stop()
	wakes.Wake(other)
	select {
	case <-woken:
		t.Fatal("a write to another record woke the poll")
	default:
	}
	wakes.Wake(self)
	wakes.Wake(self)
	<-woken
	select {
	case <-woken:
		t.Fatal("two writes before a wait are one wake")
	default:
	}
}

// pagedStore answers DeclarationPage from a page a test sets, counting
// the reads; the rest of the store is left to the embedded interface.
type pagedStore struct {
	cluster.DeclarationStore
	mu    sync.Mutex
	page  cluster.Page
	err   error
	reads int
	read  chan struct{}
}

func newPagedStore() *pagedStore {
	return &pagedStore{page: cluster.Page{Cursor: "c.0", Changes: []cluster.Change{}}, read: make(chan struct{}, 16)}
}

func (s *pagedStore) DeclarationPage(_ context.Context, _ grn.GRN, _ *string, _ int) (cluster.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	s.read <- struct{}{}
	return s.page, s.err
}

func (s *pagedStore) set(p cluster.Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.page = p
}

func ptr(s string) *string { return &s }

func TestWatch_HoldsAnEmptyPageUntilAWriteWakesIt(t *testing.T) {
	store, wakes := newPagedStore(), cluster.NewWakes()
	d := cluster.NewService(nil, store, cluster.Config{Wakes: wakes, DeclarationRecheck: time.Hour})
	answered := make(chan cluster.Page)
	go func() {
		page, err := d.WatchDeclarations(context.Background(), self, ptr("c.0"), cluster.MaxWait)
		assert.NoError(t, err)
		answered <- page
	}()
	<-store.read
	store.set(cluster.Page{Cursor: "c.1", Changes: []cluster.Change{apply(1, "a").Change}})
	wakes.Wake(self)
	select {
	case page := <-answered:
		assert.Equal(t, "c.1", page.Cursor)
	case <-time.After(10 * time.Second):
		t.Fatal("the wake did not end the hold")
	}
}

func TestWatch_ReadsAgainAtItsRecheckWhenNoWakeCame(t *testing.T) {
	store := newPagedStore()
	d := cluster.NewService(nil, store, cluster.Config{DeclarationRecheck: 10 * time.Millisecond})
	answered := make(chan cluster.Page)
	go func() {
		page, _ := d.WatchDeclarations(context.Background(), self, ptr("c.0"), cluster.MaxWait)
		answered <- page
	}()
	<-store.read
	store.set(cluster.Page{Cursor: "c.1", Changes: []cluster.Change{apply(1, "a").Change}})
	select {
	case page := <-answered:
		assert.Equal(t, "c.1", page.Cursor)
	case <-time.After(10 * time.Second):
		t.Fatal("the recheck did not read the write a lost wake carried")
	}
}

func TestWatch_AnswersEmptyOnceWaitIsOverAndAFullSetAtOnce(t *testing.T) {
	store := newPagedStore()
	d := cluster.NewService(nil, store, cluster.Config{DeclarationRecheck: time.Hour})
	page, err := d.WatchDeclarations(context.Background(), self, ptr("c.0"), 0)
	require.NoError(t, err)
	assert.Empty(t, page.Changes)
	assert.Equal(t, 2, store.reads, "a wait of 0 reads, and reads once more as it answers")

	store.set(cluster.Page{Cursor: "c.0", Full: true, Changes: []cluster.Change{}})
	// Held, the poll would outlast this deadline and answer it canceled.
	soon, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err = d.WatchDeclarations(soon, self, nil, cluster.MaxWait)
	require.NoError(t, err, "an empty full set is answered without holding")
	assert.True(t, page.Full)
}

func TestWatch_RefusesWhatTheRouteMayNotAsk(t *testing.T) {
	store := newPagedStore()
	d := cluster.NewService(nil, store, cluster.Config{})
	_, err := d.WatchDeclarations(context.Background(), self, ptr(""), 0)
	assert.Equal(t, errorx.KindInvalidArgument, errorx.KindOf(err), "an empty after")
	_, err = d.WatchDeclarations(context.Background(), self, nil, cluster.MaxWait+time.Second)
	assert.Equal(t, errorx.KindInvalidArgument, errorx.KindOf(err), "a wait past 50 seconds")
	located, err := grn.Parse("grn:acme/iam/cluster/5f60710a1b2c3d4e#somewhere")
	require.NoError(t, err)
	_, err = d.WatchDeclarations(context.Background(), located, nil, 0)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "a GRN GRNOf does not derive")
	assert.Zero(t, store.reads, "a refused poll reads nothing")

	store.err = cluster.ErrClusterNotFound
	_, err = d.WatchDeclarations(context.Background(), self, nil, 0)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "a record deleted meanwhile leaves the token standing for nothing")
}

func TestWatch_IsBoundedPerCluster(t *testing.T) {
	store := newPagedStore()
	store.page.Full = true
	now := time.Now()
	d := cluster.NewService(nil, store, cluster.Config{Now: func() time.Time { return now }})
	for range cluster.WatchesPerCluster {
		_, err := d.WatchDeclarations(context.Background(), self, nil, 0)
		require.NoError(t, err)
		<-store.read
	}
	_, err := d.WatchDeclarations(context.Background(), self, nil, 0)
	var exceeded *operator.Exceeded
	assert.True(t, errors.As(err, &exceeded), "%v", err)
	other := grn.New("acme", cluster.ServiceType, "0000000000000000")
	_, err = d.WatchDeclarations(context.Background(), other, nil, 0)
	assert.NoError(t, err, "the control: another cluster's bound is its own")
}
