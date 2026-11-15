package cli

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/artifact"
	"github.com/garamsh/garam/cowork/internal/channel"
	"github.com/garamsh/garam/cowork/internal/page"
	"github.com/garamsh/garam/cowork/internal/platform"
	"github.com/garamsh/garam/cowork/internal/policyfeed"
	"github.com/garamsh/garam/cowork/internal/task"
	"github.com/garamsh/garam/lib/errorx"
)

// journal is what a create and its wait did, in the order they did it.
type journal struct {
	mu     sync.Mutex
	events []string
	err    error
}

func (j *journal) add(e string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.events = append(j.events, e)
}

func (j *journal) all() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.events...)
}

// Await records the entity it was asked to wait for, and answers with
// the journal's error.
func (j *journal) Await(_ context.Context, e policyfeed.Entity) error {
	j.add("await " + e.Type + " " + e.Org + " " + e.ID)
	return j.err
}

var _ policyfeed.Gate = (*journal)(nil)

// Each fake service stores on Create, which the journal records before
// any wait, and answers a read without one.
type pages struct {
	page.Service
	j   *journal
	err error
}

func (s pages) Create(context.Context, platform.Caller, string, page.CreateInput) (page.Page, error) {
	s.j.add("store page")
	return page.Page{ID: "pg1", Org: "acme"}, s.err
}

func (s pages) Get(context.Context, platform.Caller, string, string) (page.Page, error) {
	s.j.add("get page")
	return page.Page{ID: "pg1", Org: "acme"}, nil
}

// Delete removes pg1 and pg2 beneath it.
func (s pages) Delete(context.Context, platform.Caller, string, string) ([]string, error) {
	s.j.add("delete page")
	if s.err != nil {
		return nil, s.err
	}
	return []string{"pg1", "pg2"}, nil
}

func (s pages) Update(context.Context, platform.Caller, string, string, page.UpdateInput) (page.Page, error) {
	s.j.add("update page")
	return page.Page{ID: "pg1", Org: "acme"}, s.err
}

type channels struct {
	channel.Service
	j   *journal
	err error
}

func (s channels) Create(context.Context, platform.Caller, string, channel.CreateInput) (channel.Channel, error) {
	s.j.add("store channel")
	return channel.Channel{ID: "ch1", Org: "acme"}, s.err
}

func (s channels) Get(context.Context, platform.Caller, string, string) (channel.Channel, error) {
	s.j.add("get channel")
	return channel.Channel{ID: "ch1", Org: "acme"}, nil
}

type tasks struct {
	task.Service
	j   *journal
	err error
}

func (s tasks) Create(context.Context, platform.Caller, string, task.CreateInput) (task.Task, error) {
	s.j.add("store task")
	return task.Task{ID: "tk1", Org: "acme"}, s.err
}

func (s tasks) Get(context.Context, platform.Caller, string, string) (task.Task, error) {
	s.j.add("get task")
	return task.Task{ID: "tk1", Org: "acme"}, nil
}

type artifacts struct {
	artifact.Service
	j   *journal
	err error
}

func (s artifacts) Create(context.Context, platform.Caller, string, artifact.CreateInput) (artifact.Artifact, error) {
	s.j.add("store artifact")
	return artifact.Artifact{ID: "ar1", Org: "acme"}, s.err
}

func (s artifacts) Get(context.Context, platform.Caller, string, string) (artifact.Artifact, error) {
	s.j.add("get artifact")
	return artifact.Artifact{ID: "ar1", Org: "acme"}, nil
}

var kindNames = []string{"page", "channel", "task", "artifact"}

// kinds drives one create and one read per kind of entity a route
// creates, each as a closure over a fresh journal.
func kinds(j *journal) map[string]struct {
	create, read func() error
	wait, store  string
} {
	ctx, who := context.Background(), platform.Caller{}
	type pair = struct {
		create, read func() error
		wait, store  string
	}
	p := awaitingPages{pages{j: j}, j}
	c := awaitingChannels{channels{j: j}, j}
	tk := awaitingTasks{tasks{j: j}, j}
	a := awaitingArtifacts{artifacts{j: j}, j}
	return map[string]pair{
		"page": {
			func() error { _, err := p.Create(ctx, who, "acme", page.CreateInput{}); return err },
			func() error { _, err := p.Get(ctx, who, "acme", "pg1"); return err },
			"await page acme pg1", "store page"},
		"channel": {
			func() error { _, err := c.Create(ctx, who, "acme", channel.CreateInput{}); return err },
			func() error { _, err := c.Get(ctx, who, "acme", "ch1"); return err },
			"await channel acme ch1", "store channel"},
		"task": {
			func() error { _, err := tk.Create(ctx, who, "acme", task.CreateInput{}); return err },
			func() error { _, err := tk.Get(ctx, who, "acme", "tk1"); return err },
			"await task acme tk1", "store task"},
		"artifact": {
			func() error { _, err := a.Create(ctx, who, "acme", artifact.CreateInput{}); return err },
			func() error { _, err := a.Get(ctx, who, "acme", "ar1"); return err },
			"await artifact acme ar1", "store artifact"},
	}
}

func TestAwaiting_ACreateWaitsOnTheEntityItStoredAfterStoringIt(t *testing.T) {
	for _, name := range kindNames {
		t.Run(name, func(t *testing.T) {
			j := &journal{}
			k := kinds(j)[name]

			require.NoError(t, k.create())

			assert.Equal(t, []string{k.store, k.wait}, j.all())
		})
	}
}

func TestAwaiting_ACreateThatIsNotHeldByTheServiceAnswersThatError(t *testing.T) {
	for _, name := range kindNames {
		t.Run(name, func(t *testing.T) {
			j := &journal{err: errorx.New(errorx.KindUnavailable, "not applied")}

			err := kinds(j)[name].create()

			assert.Same(t, j.err, err)
		})
	}
}

func TestAwaiting_ARead_WaitsOnNothing(t *testing.T) {
	for _, name := range kindNames {
		t.Run(name, func(t *testing.T) {
			j := &journal{}

			require.NoError(t, kinds(j)[name].read())

			assert.Equal(t, []string{"get " + name}, j.all())
		})
	}
}

func TestAwaiting_ACreateThatStoredNothingWaitsOnNothing(t *testing.T) {
	failed := errorx.New(errorx.KindPermissionDenied, "refused")
	ctx, who := context.Background(), platform.Caller{}
	cases := map[string]func(j *journal) error{
		"page": func(j *journal) error {
			_, err := awaitingPages{pages{j: j, err: failed}, j}.Create(ctx, who, "acme", page.CreateInput{})
			return err
		},
		"channel": func(j *journal) error {
			_, err := awaitingChannels{channels{j: j, err: failed}, j}.Create(ctx, who, "acme", channel.CreateInput{})
			return err
		},
		"task": func(j *journal) error {
			_, err := awaitingTasks{tasks{j: j, err: failed}, j}.Create(ctx, who, "acme", task.CreateInput{})
			return err
		},
		"artifact": func(j *journal) error {
			_, err := awaitingArtifacts{artifacts{j: j, err: failed}, j}.Create(ctx, who, "acme", artifact.CreateInput{})
			return err
		},
	}
	for name, create := range cases {
		t.Run(name, func(t *testing.T) {
			j := &journal{}

			err := create(j)

			assert.Same(t, failed, err)
			assert.Equal(t, []string{"store " + name}, j.all())
		})
	}
}

// A page is the one entity a route deletes: a channel, a task and an
// artifact have none. The pages beneath it go with it, and each has a
// mark of its own.
func TestAwaiting_APageDeleteWaitsOnEveryPageItRemovedAfterDeleting(t *testing.T) {
	j := &journal{}

	removed, err := awaitingPages{pages{j: j}, j}.Delete(context.Background(), platform.Caller{}, "acme", "pg1")

	require.NoError(t, err)
	assert.Equal(t, []string{"pg1", "pg2"}, removed)
	assert.Equal(t, []string{"delete page", "await page acme pg1", "await page acme pg2"}, j.all())
}

func TestAwaiting_APageDeleteThatDeletedNothingWaitsOnNothing(t *testing.T) {
	failed := errorx.New(errorx.KindNotFound, "no such page")
	j := &journal{}

	_, err := awaitingPages{pages{j: j, err: failed}, j}.Delete(context.Background(), platform.Caller{}, "acme", "pg1")

	assert.Same(t, failed, err)
	assert.Equal(t, []string{"delete page"}, j.all())
}

func TestAwaiting_APageDeleteThatIsNotAppliedAnswersThatError(t *testing.T) {
	j := &journal{err: errorx.New(errorx.KindUnavailable, "not applied")}

	_, err := awaitingPages{pages{j: j}, j}.Delete(context.Background(), platform.Caller{}, "acme", "pg1")

	assert.Same(t, j.err, err)
}

func TestAwaiting_APageUpdateWaitsOnNothing(t *testing.T) {
	j := &journal{}

	_, err := awaitingPages{pages{j: j}, j}.Update(context.Background(), platform.Caller{}, "acme", "pg1", page.UpdateInput{})

	require.NoError(t, err)
	assert.Equal(t, []string{"update page"}, j.all())
}

func TestWire_WithAFeedEveryCreateWaitsAndWithoutOneNoneDoes(t *testing.T) {
	without, err := wire(Config{PlatformURL: "http://platform.invalid", PolicyClient: testPolicyClient(t)}, nil)
	require.NoError(t, err)
	assert.IsType(t, page.NewService(nil, nil), without.pages)
	assert.IsType(t, channel.NewService(nil, nil, nil), without.channels)
	assert.IsType(t, task.NewService(nil, nil, nil, nil), without.tasks)
	assert.IsType(t, artifact.NewService(nil, nil, nil, nil, nil, nil), without.artifacts)

	with, err := wire(Config{PlatformURL: "http://platform.invalid", PolicyClient: testPolicyClient(t), Feed: &journal{}}, nil)
	require.NoError(t, err)
	assert.IsType(t, awaitingPages{}, with.pages)
	assert.IsType(t, awaitingChannels{}, with.channels)
	assert.IsType(t, awaitingTasks{}, with.tasks)
	assert.IsType(t, awaitingArtifacts{}, with.artifacts)
}
