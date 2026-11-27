package bot_test

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
	"github.com/garamsh/garam/platform/internal/app"
	"github.com/garamsh/garam/platform/internal/bot"
	"github.com/garamsh/garam/platform/internal/operator"
)

// aBot is an interface-1 Bot the connector admits; each case of the
// refusal table below changes one member of it.
const (
	spec = `"spec":{"persona":{"role":"Reviewer"},"agent":{"harness":"claude-code"},"permissions":{"secrets":["bot-token/value"]}}`
	aBot = `{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot",
	"metadata":{"namespace":"team-a","name":"reviewer","labels":{"tier":"one"}},` + spec + `}`
)

func TestParseDeclaredObject_KeepsDECL7sMembersAsWritten(t *testing.T) {
	o, err := bot.ParseDeclaredObject([]byte(strings.Replace(aBot, `"kind":"Bot",`,
		`"kind":"Bot","status":{},`, 1)))
	require.NoError(t, err)
	assert.Equal(t, "Bot", o.Kind())
	assert.Equal(t, "team-a", o.Namespace())
	assert.Equal(t, "reviewer", o.Name())
	var kept map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(o.JSON(), &kept))
	assert.NotContains(t, kept, "status", "a member DECL-7 does not name is dropped")
	assert.NotContains(t, string(kept["spec"]), "placement", "the parser's defaults are the connector's to apply")
	assert.Contains(t, string(kept["metadata"]), `"labels":{"tier":"one"}`)
}

func TestParseDeclaredObject_RefusesWhatInterface1Refuses(t *testing.T) {
	for _, tc := range []struct {
		name, from, to, field string
	}{
		{"no apiVersion", `"apiVersion":"bot.garam.sh/v1alpha1",`, ``, "Bot"},
		{"another apiVersion", `"bot.garam.sh/v1alpha1"`, `"garam.sh/v1alpha1"`, "garam.sh/v1alpha1"},
		{"a kind not declared", `"kind":"Bot"`, `"kind":"Deployment"`, "Deployment"},
		{"a namespace that is no label", `"namespace":"team-a"`, `"namespace":"Team.A"`, "metadata.namespace"},
		{"no namespace", `"namespace":"team-a",`, ``, "metadata.namespace"},
		{"an empty name", `"name":"reviewer"`, `"name":""`, "metadata.name"},
		{"a name that is no subdomain", `"name":"reviewer"`, `"name":"Reviewer_1"`, "metadata.name"},
		{"an unknown metadata field", `"name":"reviewer"`, `"name":"reviewer","owner":"x"`, "metadata.owner"},
		{"a spec that is null", spec, `"spec":null`, "spec"},
		{"a required field missing", `"agent":{"harness":"claude-code"},`, ``, "spec.agent"},
		{"an unknown spec field", `"persona"`, `"unknownField":true,"persona"`, "spec.unknownField"},
		{"an image (DECL-9)", `"persona"`, `"image":"x","persona"`, "spec.image"},
		{"a secret's value (DECL-8)", `"bot-token/value"`, `"hunter2"`, "spec.permissions.secrets[0]"},
		{"a secret with no key (DECL-8)", `"bot-token/value"`, `"bot-token/"`, "spec.permissions.secrets[0]"},
		{"a secret that is no string (DECL-8)", `"bot-token/value"`, `{"name":"bot-token","key":"value"}`, "spec.permissions.secrets[0]"},
		{"not an object", aBot, `[]`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := strings.Replace(aBot, tc.from, tc.to, 1)
			require.NotEqual(t, aBot, raw, "the case changes the object")
			_, err := bot.ParseDeclaredObject([]byte(raw))
			require.ErrorIs(t, err, bot.ErrDeclaredObjectInvalid)
			assert.Contains(t, err.Error(), tc.field, "the refusal names what it refused")
			assert.NotContains(t, err.Error(), "hunter2", "a refusal never repeats a secret's value")
		})
	}
	_, err := bot.ParseDeclaredObject([]byte(aBot))
	assert.NoError(t, err, "the control: the unchanged object is admitted")
}

func TestParseDeclaredObject_RefusesAnObjectOverItsBound(t *testing.T) {
	big := strings.Replace(aBot, `"labels"`, `"annotations":{"note":"`+strings.Repeat("x", bot.DeclaredObjectMaxBytes)+`"},"labels"`, 1)
	_, err := bot.ParseDeclaredObject([]byte(big))
	assert.Equal(t, errorx.KindInvalidArgument, errorx.KindOf(err))
	assert.NotErrorIs(t, err, bot.ErrDeclaredObjectInvalid, "the bound is garam's, not interface 1's")
}

func TestCheckDeclarationID_TakesWhatAChangeCarries(t *testing.T) {
	drawn, err := bot.NewDeclarationID()
	require.NoError(t, err)
	for _, id := range []string{drawn, "a", "Bot.reviewer_1-x", strings.Repeat("a", 128)} {
		assert.NoError(t, bot.CheckDeclarationID(id), id)
	}
	for _, id := range []string{"", strings.Repeat("a", 129), "a/b", "a b", "ü"} {
		assert.Error(t, bot.CheckDeclarationID(id), id)
	}
}

func TestParseCursor_ReadsOnlyThisRecordsLiveCursors(t *testing.T) {
	at := bot.Position{Head: 7, PurgedThrough: 2}
	full, err := bot.ParseCursor(nil, at)
	require.NoError(t, err)
	assert.Equal(t, bot.Cursor{Full: true, Base: 7, LastSent: -1}, full, "an absent cursor starts a full set at the head")
	for raw, want := range map[string]bot.Cursor{
		"c.3":    {Base: 3, LastSent: 3},
		"c.7":    {Base: 7, LastSent: 7},
		"f.5.6":  {Full: true, Base: 5, LastSent: 6},
		"f.5.-1": {Full: true, Base: 5, LastSent: -1},
	} {
		got, err := bot.ParseCursor(&raw, at)
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
		assert.Equal(t, raw, got.String(), "a cursor reads back as written")
	}
	for _, raw := range []string{"c", "c.", "c.x", "c.8", "c.-2", "f.5", "f.5.6.7", "x.3", "c.3.4"} {
		_, err := bot.ParseCursor(&raw, at)
		assert.ErrorIs(t, err, bot.ErrCursorInvalid, raw)
	}
	for _, raw := range []string{"c.2", "c.0", "f.2.5"} {
		_, err := bot.ParseCursor(&raw, at)
		assert.ErrorIs(t, err, bot.ErrCursorExpired, raw)
	}
}

func apply(seq int64, id string) bot.Entry {
	return bot.Entry{Seq: seq, Change: bot.Change{Op: bot.OpApply, ID: id, Generation: 1, Object: json.RawMessage(`{}`)}}
}

func TestLogPage_AnswersAtMostAPageAndTheCursorAfterItsLast(t *testing.T) {
	at := bot.Cursor{Base: 4, LastSent: 4}
	page := bot.LogPage(at, []bot.Entry{apply(5, "a"), apply(6, "b"), apply(7, "c")}, 2)
	assert.True(t, page.More)
	assert.Equal(t, "c.6", page.Cursor)
	assert.Len(t, page.Changes, 2)

	page = bot.LogPage(at, []bot.Entry{apply(5, "a"), apply(6, "b")}, 2)
	assert.False(t, page.More, "exactly a page leaves none")
	assert.Equal(t, "c.6", page.Cursor)

	page = bot.LogPage(at, nil, 2)
	assert.Equal(t, "c.4", page.Cursor, "an empty page keeps the cursor it was read after")
	assert.NotNil(t, page.Changes, "changes is required, so an empty page carries []")
}

func TestFullPage_GoesOnFromItsBaseOnceTheSetIsSent(t *testing.T) {
	at := bot.Cursor{Full: true, Base: 9, LastSent: -1}
	page := bot.FullPage(at, []bot.Entry{apply(2, "a"), apply(8, "b"), apply(9, "c")}, 2)
	assert.True(t, page.Full)
	assert.True(t, page.More)
	assert.Equal(t, "f.9.8", page.Cursor, "the next page reads after the last one sent")

	at = bot.Cursor{Full: true, Base: 9, LastSent: 8}
	page = bot.FullPage(at, []bot.Entry{apply(9, "c")}, 2)
	assert.True(t, page.Full)
	assert.False(t, page.More)
	assert.Equal(t, "c.9", page.Cursor, "the last page answers the log cursor at the set's base")

	page = bot.FullPage(bot.Cursor{Full: true, Base: 0, LastSent: -1}, nil, 2)
	assert.True(t, page.Full, "a record with nothing declared answers an empty full set")
	assert.Equal(t, "c.0", page.Cursor)
}

func TestWakes_KeepsOneWakeForAPollBetweenItsReadAndItsWait(t *testing.T) {
	wakes := bot.NewWakes()
	other := grn.New("acme", app.InstanceType, "0000000000000000")
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
	bot.DeclarationStore
	mu    sync.Mutex
	page  bot.Page
	err   error
	reads int
	read  chan struct{}
}

func newPagedStore() *pagedStore {
	return &pagedStore{page: bot.Page{Cursor: "c.0", Changes: []bot.Change{}}, read: make(chan struct{}, 16)}
}

func (s *pagedStore) DeclarationPage(_ context.Context, _ grn.GRN, _ *string, _ int) (bot.Page, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	s.read <- struct{}{}
	return s.page, s.err
}

func (s *pagedStore) set(p bot.Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.page = p
}

func ptr(s string) *string { return &s }

func TestWatch_HoldsAnEmptyPageUntilAWriteWakesIt(t *testing.T) {
	store, wakes := newPagedStore(), bot.NewWakes()
	d := bot.NewService(nil, store, nil, nil, bot.Config{Wakes: wakes, DeclarationRecheck: time.Hour})
	answered := make(chan bot.Page)
	go func() {
		page, err := d.WatchDeclarations(context.Background(), self, ptr("c.0"), bot.MaxWait)
		assert.NoError(t, err)
		answered <- page
	}()
	<-store.read
	store.set(bot.Page{Cursor: "c.1", Changes: []bot.Change{apply(1, "a").Change}})
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
	d := bot.NewService(nil, store, nil, nil, bot.Config{DeclarationRecheck: 10 * time.Millisecond})
	answered := make(chan bot.Page)
	go func() {
		page, _ := d.WatchDeclarations(context.Background(), self, ptr("c.0"), bot.MaxWait)
		answered <- page
	}()
	<-store.read
	store.set(bot.Page{Cursor: "c.1", Changes: []bot.Change{apply(1, "a").Change}})
	select {
	case page := <-answered:
		assert.Equal(t, "c.1", page.Cursor)
	case <-time.After(10 * time.Second):
		t.Fatal("the recheck did not read the write a lost wake carried")
	}
}

func TestWatch_AnswersEmptyOnceWaitIsOverAndAFullSetAtOnce(t *testing.T) {
	store := newPagedStore()
	d := bot.NewService(nil, store, nil, nil, bot.Config{DeclarationRecheck: time.Hour})
	page, err := d.WatchDeclarations(context.Background(), self, ptr("c.0"), 0)
	require.NoError(t, err)
	assert.Empty(t, page.Changes)
	assert.Equal(t, 2, store.reads, "a wait of 0 reads, and reads once more as it answers")

	store.set(bot.Page{Cursor: "c.0", Full: true, Changes: []bot.Change{}})
	// Held, the poll would outlast this deadline and answer it canceled.
	soon, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	page, err = d.WatchDeclarations(soon, self, nil, bot.MaxWait)
	require.NoError(t, err, "an empty full set is answered without holding")
	assert.True(t, page.Full)
}

func TestWatch_RefusesWhatTheRouteMayNotAsk(t *testing.T) {
	store := newPagedStore()
	d := bot.NewService(nil, store, nil, nil, bot.Config{})
	_, err := d.WatchDeclarations(context.Background(), self, ptr(""), 0)
	assert.Equal(t, errorx.KindInvalidArgument, errorx.KindOf(err), "an empty after")
	_, err = d.WatchDeclarations(context.Background(), self, nil, bot.MaxWait+time.Second)
	assert.Equal(t, errorx.KindInvalidArgument, errorx.KindOf(err), "a wait past 50 seconds")
	located, err := grn.Parse("grn:acme/iam/instance/5f60710a1b2c3d4e#somewhere")
	require.NoError(t, err)
	_, err = d.WatchDeclarations(context.Background(), located, nil, 0)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "a GRN GRNOf does not derive")
	assert.Zero(t, store.reads, "a refused poll reads nothing")

	store.err = app.ErrInstanceNotFound
	_, err = d.WatchDeclarations(context.Background(), self, nil, 0)
	assert.ErrorIs(t, err, operator.ErrClientInvalid, "a record deleted meanwhile leaves the token standing for nothing")
}

func TestWatch_IsBoundedPerInstance(t *testing.T) {
	store := newPagedStore()
	store.page.Full = true
	now := time.Now()
	d := bot.NewService(nil, store, nil, nil, bot.Config{Now: func() time.Time { return now }})
	for range bot.WatchesPerInstance {
		_, err := d.WatchDeclarations(context.Background(), self, nil, 0)
		require.NoError(t, err)
		<-store.read
	}
	_, err := d.WatchDeclarations(context.Background(), self, nil, 0)
	var exceeded *operator.Exceeded
	assert.True(t, errors.As(err, &exceeded), "%v", err)
	other := grn.New("acme", app.InstanceType, "0000000000000000")
	_, err = d.WatchDeclarations(context.Background(), other, nil, 0)
	assert.NoError(t, err, "the control: another instance's bound is its own")
}
