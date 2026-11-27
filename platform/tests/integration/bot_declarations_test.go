package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/app"
	apppg "github.com/garamsh/garam/platform/internal/app/postgres"
	"github.com/garamsh/garam/platform/internal/bot"
	botpg "github.com/garamsh/garam/platform/internal/bot/postgres"
)

// object is an interface-1 Bot named name, as ParseDeclaredObject
// admits it.
func object(t *testing.T, name string) bot.DeclaredObject {
	t.Helper()
	o, err := bot.ParseDeclaredObject([]byte(`{"apiVersion":"bot.garam.sh/v1alpha1","kind":"Bot",
		"metadata":{"namespace":"team-a","name":"` + name + `"},
		"spec":{"persona":{"role":"Reviewer"},"agent":{"harness":"claude-code"}}}`))
	require.NoError(t, err)
	return o
}

func page(t *testing.T, s *botpg.Store, g grn.GRN, after string, size int) bot.Page {
	t.Helper()
	var at *string
	if after != "" {
		at = &after
	}
	p, err := s.DeclarationPage(context.Background(), g, at, size)
	require.NoError(t, err, after)
	return p
}

func idsOf(p bot.Page) []string {
	out := make([]string, 0, len(p.Changes))
	for _, c := range p.Changes {
		out = append(out, c.Op+" "+c.ID+" "+fmt.Sprint(c.Generation))
	}
	return out
}

// TestBotDeclarations_AFullSetThenTheLogAfterIt holds DECL-1,
// DECL-2 and DECL-5 against the store: an absent cursor reads the live
// declarations as applies, paged, and goes on from the position the
// set began at; the log after it carries every change in order, a
// tombstone as a delete at the next generation.
func TestBotDeclarations_AFullSetThenTheLogAfterIt(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	g := declarationRecords(t, p, 1)[0]

	empty := page(t, s, g, "", 100)
	assert.True(t, empty.Full)
	assert.Empty(t, empty.Changes)
	assert.Equal(t, "c.0", empty.Cursor, "a record that wrote nothing starts at 0")

	for _, id := range []string{"a", "b", "c"} {
		_, created, err := s.Declare(ctx, g, id, object(t, "bot-"+id))
		require.NoError(t, err)
		assert.True(t, created)
	}
	_, err := s.Tombstone(ctx, g, "b")
	require.NoError(t, err)

	first := page(t, s, g, "", 1)
	assert.True(t, first.Full)
	assert.True(t, first.More)
	assert.Equal(t, []string{"apply a 1"}, idsOf(first))
	second := page(t, s, g, first.Cursor, 1)
	assert.Equal(t, []string{"apply c 1"}, idsOf(second), "a tombstoned declaration is not in the set")
	assert.True(t, second.Full)
	assert.False(t, second.More, "the set is exactly what was sent")
	assert.Equal(t, "c.4", second.Cursor, "the set goes on from the head it began at")

	d, created, err := s.Declare(ctx, g, "a", object(t, "bot-a"))
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, int64(2), d.Generation)
	after := page(t, s, g, second.Cursor, 100)
	assert.False(t, after.Full)
	assert.Equal(t, []string{"apply a 2"}, idsOf(after))
	assert.Equal(t, "c.5", after.Cursor)
	assert.Empty(t, page(t, s, g, after.Cursor, 100).Changes, "nothing after the head")

	log := page(t, s, g, "c.0", 100)
	assert.Equal(t, []string{"apply a 1", "apply b 1", "apply c 1", "delete b 2", "apply a 2"}, idsOf(log))
	assert.Nil(t, log.Changes[3].Object, "a delete carries no object")
	var applied struct {
		Metadata struct{ Name string } `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal(log.Changes[4].Object, &applied))
	assert.Equal(t, "bot-a", applied.Metadata.Name, "an apply carries its object")
	paged := page(t, s, g, "c.1", 2)
	assert.True(t, paged.More)
	assert.Equal(t, "c.3", paged.Cursor)

	_, err = s.DeclarationPage(ctx, g, ptrTo("c.6"), 100)
	assert.ErrorIs(t, err, bot.ErrCursorInvalid, "a cursor past the head is none of this record's")
}

func ptrTo(s string) *string { return &s }

// TestBotDeclarations_AnIdentifierNamesOneDeclarationOnce holds the
// store's refusals: a tombstoned identifier, one placed on another
// record, one whose record was deleted, an object another live
// declaration names, and a declaration read or tombstoned through
// another record. Each refused write leaves the record's position
// where it was.
func TestBotDeclarations_AnIdentifierNamesOneDeclarationOnce(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	records := declarationRecords(t, p, 3)
	g, other, deleted := records[0], records[1], records[2]

	_, _, err := s.Declare(ctx, g, "one", object(t, "reviewer"))
	require.NoError(t, err)
	_, err = s.Tombstone(ctx, g, "one")
	require.NoError(t, err)
	head := page(t, s, g, "", 100).Cursor

	_, _, err = s.Declare(ctx, g, "one", object(t, "reviewer"))
	assert.ErrorIs(t, err, bot.ErrDeclarationTombstoned)
	_, err = s.Tombstone(ctx, g, "one")
	assert.ErrorIs(t, err, bot.ErrDeclarationNotFound)
	_, _, err = s.Declare(ctx, other, "one", object(t, "reviewer"))
	assert.ErrorIs(t, err, bot.ErrDeclarationTaken)

	_, _, err = s.Declare(ctx, deleted, "kept", object(t, "reviewer"))
	require.NoError(t, err)
	require.NoError(t, apppg.New(p).DeleteInstance(ctx, deleted))
	_, _, err = s.Declare(ctx, g, "kept", object(t, "writer"))
	assert.ErrorIs(t, err, bot.ErrDeclarationTaken, "an identifier outlives its record")
	_, _, err = s.Declare(ctx, deleted, "fresh", object(t, "writer"))
	assert.ErrorIs(t, err, app.ErrInstanceNotFound)

	_, _, err = s.Declare(ctx, g, "two", object(t, "writer"))
	require.NoError(t, err)
	head = page(t, s, g, "", 100).Cursor
	_, _, err = s.Declare(ctx, g, "three", object(t, "writer"))
	assert.ErrorIs(t, err, bot.ErrDeclaredObjectTaken)
	assert.Equal(t, head, page(t, s, g, "", 100).Cursor, "a refused write draws no position")
	_, _, err = s.Declare(ctx, g, "three", object(t, "reviewer"))
	assert.NoError(t, err, "the control: the identifier the refusal left is placed under another object")

	_, err = s.GetDeclaration(ctx, other, "two")
	assert.ErrorIs(t, err, bot.ErrDeclarationNotFound, "another record's declaration is not read through this one")
	head = page(t, s, g, "", 100).Cursor
	_, err = s.Tombstone(ctx, other, "two")
	assert.ErrorIs(t, err, bot.ErrDeclarationNotFound, "another record's declaration is not tombstoned through this one")
	two, err := s.GetDeclaration(ctx, g, "two")
	require.NoError(t, err, "the declaration stands on its own record")
	assert.Equal(t, int64(1), two.Generation)
	assert.Equal(t, head, page(t, s, g, "", 100).Cursor, "its record's log is unchanged")
}

// TestBotDeclarations_ACursorBeforeThePurgedPositionIsExpired holds
// DECL-6: a write purges the changes past ChangeRetention and moves
// the purged position to their last, a cursor at or before it is
// refused, and a full set still reads the declarations they wrote.
func TestBotDeclarations_ACursorBeforeThePurgedPositionIsExpired(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	g := declarationRecords(t, p, 1)[0]
	for _, id := range []string{"old", "older"} {
		_, _, err := s.Declare(ctx, g, id, object(t, id))
		require.NoError(t, err)
	}
	_, err := p.Exec(ctx, `UPDATE bot_declaration_changes SET written_at = now() - interval '31 days'
		WHERE instance_grn = $1`, g.String())
	require.NoError(t, err)
	assert.Len(t, page(t, s, g, "c.0", 100).Changes, 2, "the control: nothing is purged until a write")

	_, _, err = s.Declare(ctx, g, "new", object(t, "new"))
	require.NoError(t, err)
	for _, cursor := range []string{"c.0", "c.2", "f.2.-1"} {
		_, err = s.DeclarationPage(ctx, g, &cursor, 100)
		assert.ErrorIs(t, err, bot.ErrCursorExpired, cursor)
	}
	assert.Equal(t, []string{}, idsOf(page(t, s, g, "c.3", 100)))
	full := page(t, s, g, "", 100)
	assert.Equal(t, []string{"apply old 1", "apply older 1", "apply new 1"}, idsOf(full))
	assert.Equal(t, "c.3", full.Cursor)
}

// TestBotDeclarations_ConcurrentWritesDrawConsecutivePositions
// holds that a record's positions are drawn one at a time: sixteen
// writes racing on one record leave a log of sixteen with no gap and
// no repeat.
func TestBotDeclarations_ConcurrentWritesDrawConsecutivePositions(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	g := declarationRecords(t, p, 1)[0]
	const racers = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, err := s.Declare(ctx, g, fmt.Sprintf("racer-%d", i), object(t, fmt.Sprintf("racer-%d", i)))
			assert.NoError(t, err)
		}()
	}
	close(start)
	wg.Wait()
	log := page(t, s, g, "c.0", 100)
	require.Len(t, log.Changes, racers)
	assert.Equal(t, fmt.Sprintf("c.%d", racers), log.Cursor)
	seen := map[string]bool{}
	for _, c := range log.Changes {
		seen[c.ID] = true
	}
	assert.Len(t, seen, racers, "each write is one change")
}

// TestBotDeclarations_AWriteWakesAPollOnAnyProcess holds the wake:
// a declaration written through one pool reaches ListenDeclarations on
// another, which wakes the polls held on that record alone.
func TestBotDeclarations_AWriteWakesAPollOnAnyProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := pool(t)
	s := botpg.New(p)
	records := declarationRecords(t, p, 2)
	g, other := records[0], records[1]
	wakes := bot.NewWakes()
	woken, stop := wakes.Subscribe(g)
	defer stop()
	otherWoken, stopOther := wakes.Subscribe(other)
	defer stopOther()
	go botpg.ListenDeclarations(ctx, botpg.New(p), wakes, func(error, time.Duration) {})

	// The listener's LISTEN may follow the first write, so the write is
	// repeated until one is heard.
	deadline := time.After(20 * time.Second)
	for i := 0; ; i++ {
		_, _, err := s.Declare(ctx, g, "watched", object(t, "watched"))
		require.NoError(t, err)
		select {
		case <-woken:
			select {
			case <-otherWoken:
				t.Fatal("a write to one record woke a poll on another")
			default:
			}
			return
		case <-time.After(200 * time.Millisecond):
		case <-deadline:
			t.Fatalf("no wake after %d writes", i+1)
		}
	}
}

// TestBotDeclarations_ListAnswersTheLiveOnesInWriteOrder covers the
// browser's read.
func TestBotDeclarations_ListAnswersTheLiveOnesInWriteOrder(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := botpg.New(p)
	g := declarationRecords(t, p, 1)[0]
	for _, id := range []string{"a", "b", "c"} {
		_, _, err := s.Declare(ctx, g, id, object(t, id))
		require.NoError(t, err)
	}
	_, _, err := s.Declare(ctx, g, "a", object(t, "a"))
	require.NoError(t, err)
	_, err = s.Tombstone(ctx, g, "b")
	require.NoError(t, err)
	listed, err := s.ListDeclarations(ctx, g)
	require.NoError(t, err)
	var ids []string
	for _, d := range listed {
		ids = append(ids, d.ID+"@"+fmt.Sprint(d.Generation))
	}
	assert.Equal(t, "c@1 a@2", strings.Join(ids, " "))
	got, err := s.GetDeclaration(ctx, g, "a")
	require.NoError(t, err)
	assert.Equal(t, "a", got.Object.Name())
	assert.False(t, got.UpdatedAt.Before(got.CreatedAt))
	_, err = s.ListDeclarations(ctx, grn.New("acme", app.InstanceType, "0000000000000000"))
	assert.ErrorIs(t, err, app.ErrInstanceNotFound)

	// jsonb keeps a number as numeric, so it reads back as written.
	big, err := bot.ParseDeclaredObject([]byte(`{"apiVersion":"bot.garam.sh/v1alpha1","kind":"BotPool",
		"metadata":{"namespace":"team-a","name":"big"},"spec":{"maxBotsPerPod":50}}`))
	require.NoError(t, err)
	_, _, err = s.Declare(ctx, g, "big", big)
	require.NoError(t, err)
	got, err = s.GetDeclaration(ctx, g, "big")
	require.NoError(t, err)
	assert.Contains(t, string(got.Object.JSON()), `"maxBotsPerPod":50`)
}
