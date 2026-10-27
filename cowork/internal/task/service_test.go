package task_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/channel"
	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/cowork/internal/task"
	"github.com/garamsh/garam/cowork/internal/task/memory"
	"github.com/garamsh/garam/platform/sdk/errorx"
	"github.com/garamsh/garam/platform/sdk/grn"
)

// policy allows every check but those deny names, as `action` or
// `action@id`, or either after a person's name and a space.
type policy struct{ deny map[string]bool }

func (p *policy) Identify(context.Context, platform.Credential) (platform.Caller, error) {
	return platform.Caller{}, nil
}

func (p *policy) Organizations(context.Context, platform.Caller) ([]platform.Organization, error) {
	return nil, nil
}

func (p *policy) Allowed(_ context.Context, c platform.Caller, org string, checks []platform.Check) ([]bool, error) {
	if org != "acme" {
		return nil, errorx.New(errorx.KindPermissionDenied, "not a member")
	}
	out := make([]bool, len(checks))
	for i, ch := range checks {
		k := ch.Action
		if ch.ID != "" {
			k += "@" + ch.ID
		}
		out[i] = !p.deny[k] && !p.deny[c.GRN.Identifier+" "+k]
	}
	return out, nil
}

// readers answers the pages and messages a person may read: every one
// but those hidden names, as an ID or after a person's name and a
// space.
type readers struct{ hidden map[string]bool }

func (r readers) refuses(c platform.Caller, id string) bool {
	return r.hidden[id] || r.hidden[c.GRN.Identifier+" "+id]
}

func (r readers) Get(_ context.Context, c platform.Caller, _ string, id string) (page.Page, error) {
	if r.refuses(c, id) {
		return page.Page{}, platform.ErrNotAllowed
	}
	return page.Page{ID: id}, nil
}

func (r readers) Thread(_ context.Context, c platform.Caller, _ string, id string) ([]channel.Message, error) {
	if r.refuses(c, id) {
		return nil, channel.ErrNotParticipant
	}
	return []channel.Message{{ID: id}}, nil
}

func person(name string) platform.Caller {
	return platform.Caller{GRN: grn.New(grn.RootOwner, grn.DefaultProject, "user", name)}
}

var (
	ada = person("ada")
	bob = person("bob")
	ctx = context.Background()
)

func newService(deny map[string]bool, hidden map[string]bool) task.Service {
	r := readers{hidden: hidden}
	return task.NewService(memory.New(), &policy{deny: deny}, r, r)
}

// TestCreate_TakesItsFieldsAndStartsTodo keeps what it is given, and
// a task with no status starts todo.
func TestCreate_TakesItsFieldsAndStartsTodo(t *testing.T) {
	s := newService(nil, nil)
	agent := grn.New("acme", grn.DefaultProject, "agent", "triage")
	tk, err := s.Create(ctx, ada, "acme", task.CreateInput{
		Title: " Ship it ", Assignee: agent, Due: "2026-11-02", Pages: []string{"p1", "p1"}, Messages: []string{"m1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "Ship it", tk.Title)
	assert.Equal(t, task.StatusTodo, tk.Status)
	assert.Equal(t, agent, tk.Assignee)
	assert.Equal(t, []string{"p1"}, tk.Pages, "a link is kept once")

	got, err := s.Get(ctx, bob, "acme", tk.ID)
	require.NoError(t, err)
	assert.Equal(t, tk.Title, got.Title)
	assert.Equal(t, []string{"m1"}, got.Messages)
}

// TestCreate_RefusesWhatItCannotKeep: no title, a status it does not
// know, a date that is not one, and an assignee that is neither a
// person nor an agent.
func TestCreate_RefusesWhatItCannotKeep(t *testing.T) {
	s := newService(nil, nil)
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("p%d", i)
	}
	for name, in := range map[string]task.CreateInput{
		"no title":   {Title: " "},
		"status":     {Title: "x", Status: "blocked"},
		"due":        {Title: "x", Due: "next friday"},
		"assignee":   {Title: "x", Assignee: grn.New("acme", grn.DefaultProject, "group", "eng")},
		"many links": {Title: "x", Pages: many},
	} {
		_, err := s.Create(ctx, ada, "acme", in)
		assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), name)
	}
	_, err := s.Create(ctx, ada, "acme", task.CreateInput{Title: "x", Due: "2026-11-02"})
	assert.NoError(t, err, "the control: the same task, valid")
}

// TestCreate_AsksTheOrganizationAndLinksOnlyWhatTheWriterReads
// refuses a writer the platform denies, and a link to a page or a
// message the writer may not read.
func TestCreate_AsksTheOrganizationAndLinksOnlyWhatTheWriterReads(t *testing.T) {
	s := newService(map[string]bool{"bob cowork:task-write": true}, map[string]bool{"ada secret": true, "ada dm": true})
	_, err := s.Create(ctx, bob, "acme", task.CreateInput{Title: "x"})
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))

	_, err = s.Create(ctx, ada, "acme", task.CreateInput{Title: "x", Pages: []string{"secret"}})
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "a page ada may not read")
	_, err = s.Create(ctx, ada, "acme", task.CreateInput{Title: "x", Messages: []string{"dm"}})
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "a message ada may not read")
	_, err = s.Create(ctx, ada, "acme", task.CreateInput{Title: "x", Pages: []string{"open"}, Messages: []string{"general"}})
	assert.NoError(t, err, "the control: what ada may read")
}

// TestList_LeavesOutWhatThePlatformDeniesAndFilters lists each task
// the caller may read, narrowed by status, assignee and link.
func TestList_LeavesOutWhatThePlatformDeniesAndFilters(t *testing.T) {
	deny := map[string]bool{}
	s := newService(deny, nil)
	doing := task.StatusDoing
	one, err := s.Create(ctx, ada, "acme", task.CreateInput{Title: "one", Assignee: bob.GRN, Pages: []string{"p1"}})
	require.NoError(t, err)
	two, err := s.Create(ctx, ada, "acme", task.CreateInput{Title: "two", Status: doing, Messages: []string{"m1"}})
	require.NoError(t, err)
	deny["bob cowork:task-read@"+two.ID] = true

	titles := func(c platform.Caller, f task.Filter) []string {
		ts, err := s.List(ctx, c, "acme", f)
		require.NoError(t, err)
		var out []string
		for _, tk := range ts {
			out = append(out, tk.Title)
		}
		return out
	}
	assert.ElementsMatch(t, []string{"one", "two"}, titles(ada, task.Filter{}))
	assert.Equal(t, []string{"one"}, titles(bob, task.Filter{}), "two is denied to bob")
	assert.Equal(t, []string{"two"}, titles(ada, task.Filter{Status: task.StatusDoing}))
	assert.Equal(t, []string{"one"}, titles(ada, task.Filter{Assignee: bob.GRN}))
	assert.Equal(t, []string{"one"}, titles(ada, task.Filter{Page: "p1"}))
	assert.Equal(t, []string{"two"}, titles(ada, task.Filter{Message: "m1"}))
	_ = one
}

// TestUpdate_ChangesWhatItSetsAndAsksTheTask moves a task, reassigns
// it, clears its due date and replaces its links; a writer the
// platform denies on the task changes nothing.
func TestUpdate_ChangesWhatItSetsAndAsksTheTask(t *testing.T) {
	deny := map[string]bool{}
	s := newService(deny, map[string]bool{"secret": true})
	tk, err := s.Create(ctx, ada, "acme", task.CreateInput{Title: "x", Due: "2026-11-02", Pages: []string{"p1"}})
	require.NoError(t, err)

	done := task.StatusDone
	pages := []string{"p1", "p2"}
	none := ""
	got, err := s.Update(ctx, bob, "acme", tk.ID, task.UpdateInput{Status: &done, Assignee: &bob.GRN, Due: &none, Pages: &pages})
	require.NoError(t, err)
	assert.Equal(t, task.StatusDone, got.Status)
	assert.Equal(t, bob.GRN, got.Assignee)
	assert.Empty(t, got.Due)
	assert.Equal(t, pages, got.Pages)
	assert.Equal(t, "x", got.Title, "what it does not set stays")

	more := []string{"p1", "p2", "secret"}
	_, err = s.Update(ctx, bob, "acme", tk.ID, task.UpdateInput{Pages: &more})
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied), "a link bob may not read")

	deny["bob cowork:task-write@"+tk.ID] = true
	todo := task.StatusTodo
	_, err = s.Update(ctx, bob, "acme", tk.ID, task.UpdateInput{Status: &todo})
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
	kept, err := s.Get(ctx, ada, "acme", tk.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatusDone, kept.Status, "the refused change changed nothing")
}

// TestComments_AreWrittenByWritersAndReadByReaders.
func TestComments_AreWrittenByWritersAndReadByReaders(t *testing.T) {
	deny := map[string]bool{}
	s := newService(deny, nil)
	tk, err := s.Create(ctx, ada, "acme", task.CreateInput{Title: "x"})
	require.NoError(t, err)
	_, err = s.Comment(ctx, bob, "acme", tk.ID, "on it")
	require.NoError(t, err)
	_, err = s.Comment(ctx, bob, "acme", tk.ID, " ")
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))

	cs, err := s.Comments(ctx, ada, "acme", tk.ID)
	require.NoError(t, err)
	require.Len(t, cs, 1)
	assert.Equal(t, bob.GRN, cs[0].Author)

	deny["ada cowork:task-read@"+tk.ID] = true
	_, err = s.Comments(ctx, ada, "acme", tk.ID)
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
	deny["bob cowork:task-write@"+tk.ID] = true
	_, err = s.Comment(ctx, bob, "acme", tk.ID, "again")
	assert.True(t, errorx.IsKind(err, errorx.KindPermissionDenied))
}
