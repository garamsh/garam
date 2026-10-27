package goja_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/collab"
	"github.com/garamsh/garam/cowork/internal/collab/goja"
	"github.com/garamsh/garam/platform/sdk/errorx"
)

func newEngine(t *testing.T) *goja.Engine {
	t.Helper()
	e, err := goja.New()
	require.NoError(t, err)
	return e
}

func newClient(t *testing.T, e *goja.Engine, state []byte) *goja.Client {
	t.Helper()
	c, err := e.NewClient(state)
	require.NoError(t, err)
	t.Cleanup(c.Close)
	return c
}

// TestMerge_KeepsConcurrentEdits merges two edits made apart from the
// same starting document and reads both back. Each edit alone is the
// control: neither holds the other's text.
func TestMerge_KeepsConcurrentEdits(t *testing.T) {
	e := newEngine(t)
	base := newClient(t, e, nil)
	first, err := base.AppendBlock(collab.Block{Type: "paragraph", Text: "hello"})
	require.NoError(t, err)

	a, b := newClient(t, e, first), newClient(t, e, first)
	ua, err := a.InsertText(0, 5, " ada")
	require.NoError(t, err)
	ub, err := b.InsertText(0, 0, "bob: ")
	require.NoError(t, err)

	merged, err := e.Merge([][]byte{first, ua, ub})
	require.NoError(t, err)
	blocks, err := e.Blocks(merged)
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	assert.Equal(t, "bob: hello ada", blocks[0].Text)

	onlyA, err := e.Merge([][]byte{first, ua})
	require.NoError(t, err)
	aBlocks, err := e.Blocks(onlyA)
	require.NoError(t, err)
	assert.Equal(t, "hello ada", aBlocks[0].Text)
}

// TestDiff_AnswersWhatAClientLacks holds that a client holding the
// first edit is sent the second alone: applying the diff to it yields
// the whole document, and the diff for a client holding everything
// adds nothing. The empty client, sent the diff against no state, is
// the control: it receives both edits.
func TestDiff_AnswersWhatAClientLacks(t *testing.T) {
	e := newEngine(t)
	c := newClient(t, e, nil)
	u1, err := c.AppendBlock(collab.Block{Type: "paragraph", Text: "one"})
	require.NoError(t, err)
	u2, err := c.AppendBlock(collab.Block{Type: "heading", Attrs: map[string]any{"level": float64(2)}, Text: "two"})
	require.NoError(t, err)
	state, err := e.Merge([][]byte{u1, u2})
	require.NoError(t, err)

	behind := newClient(t, e, u1)
	sv, err := behind.StateVector()
	require.NoError(t, err)
	missing, err := e.Diff(state, sv)
	require.NoError(t, err)
	require.NoError(t, behind.Apply(missing))
	caught, err := behind.State()
	require.NoError(t, err)
	blocks, err := e.Blocks(caught)
	require.NoError(t, err)
	assert.Equal(t, []string{"one", "two"}, []string{blocks[0].Text, blocks[1].Text})
	assert.Equal(t, "heading", blocks[1].Type)
	assert.Equal(t, float64(2), blocks[1].Attrs["level"])

	fresh := newClient(t, e, nil)
	empty, err := fresh.StateVector()
	require.NoError(t, err)
	all, err := e.Diff(state, empty)
	require.NoError(t, err)
	allBlocks, err := e.Blocks(all)
	require.NoError(t, err)
	assert.Len(t, allBlocks, 2)
}

// TestStateVector_MatchesTheClients holds that the vector read off an
// update, without a document, is the one a client holding it sends.
func TestStateVector_MatchesTheClients(t *testing.T) {
	e := newEngine(t)
	c := newClient(t, e, nil)
	u, err := c.AppendBlock(collab.Block{Type: "paragraph", Text: "x"})
	require.NoError(t, err)
	fromUpdate, err := e.StateVector(u)
	require.NoError(t, err)
	fromClient, err := c.StateVector()
	require.NoError(t, err)
	assert.Equal(t, fromClient, fromUpdate)
}

// TestMerge_RefusesAMalformedUpdate holds that bytes no Yjs encoder
// wrote are refused as an invalid argument, the kind a client sending
// them is answered with. A real update in the same call is the
// control.
func TestMerge_RefusesAMalformedUpdate(t *testing.T) {
	e := newEngine(t)
	u, err := newClient(t, e, nil).AppendBlock(collab.Block{Type: "paragraph", Text: "x"})
	require.NoError(t, err)
	_, err = e.Merge([][]byte{u})
	require.NoError(t, err)

	_, err = e.Merge([][]byte{u, {0xff, 0xff, 0xff}})
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), err.Error())
}

// TestBlocks_ReadsTextThatLooksLikeMarkup holds that text holding
// angle brackets reads back as typed: XmlText renders formatting as
// markup, so text read off that rendering loses "<b>" typed by hand.
// Plain text is the control.
func TestBlocks_ReadsTextThatLooksLikeMarkup(t *testing.T) {
	e := newEngine(t)
	for _, text := range []string{"plain", "a <b>bold</b> claim, 1 < 2"} {
		u, err := newClient(t, e, nil).AppendBlock(collab.Block{Type: "paragraph", Text: text})
		require.NoError(t, err)
		blocks, err := e.Blocks(u)
		require.NoError(t, err)
		assert.Equal(t, text, blocks[0].Text)
	}
}

// TestBlocks_ListsHoldTheirItems writes each list type with two items
// and reads the items back, a to-do's checked state included, and the
// list's text as its items' joined.
func TestBlocks_ListsHoldTheirItems(t *testing.T) {
	e := newEngine(t)
	c := newClient(t, e, nil)
	for _, typ := range []string{"bulletList", "orderedList", "taskList"} {
		_, err := c.AppendBlock(collab.Block{Type: typ, Items: []collab.Item{{Text: "one", Checked: true}, {Text: "two"}}})
		require.NoError(t, err)
	}
	state, err := c.State()
	require.NoError(t, err)
	blocks, err := e.Blocks(state)
	require.NoError(t, err)
	require.Len(t, blocks, 3)
	for _, b := range blocks {
		assert.Equal(t, "one\ntwo", b.Text, b.Type)
		require.Len(t, b.Items, 2)
	}
	assert.Equal(t, []collab.Item{{Text: "one"}, {Text: "two"}}, blocks[0].Items, "a bullet item has no checked state")
	assert.Equal(t, []collab.Item{{Text: "one", Checked: true}, {Text: "two"}}, blocks[2].Items)
}

// TestEdits_InsertReplaceAndDeleteByIndex places a block before the
// first, replaces the second and deletes the third, and reads the
// order back. An index past the last block is refused, and the page is
// unchanged by it.
func TestEdits_InsertReplaceAndDeleteByIndex(t *testing.T) {
	e := newEngine(t)
	c := newClient(t, e, nil)
	for _, text := range []string{"a", "b", "c"} {
		_, err := c.AppendBlock(collab.Block{Type: "paragraph", Text: text})
		require.NoError(t, err)
	}
	_, err := c.InsertBlock(0, collab.Block{Type: "heading", Attrs: map[string]any{"level": float64(1)}, Text: "title"})
	require.NoError(t, err)
	_, err = c.ReplaceBlock(1, collab.Block{Type: "blockquote", Text: "A"})
	require.NoError(t, err)
	_, err = c.DeleteBlock(2)
	require.NoError(t, err)

	_, err = c.DeleteBlock(9)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%v", err)
	_, err = c.ReplaceBlock(9, collab.Block{Type: "paragraph", Text: "x"})
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "%v", err)

	state, err := c.State()
	require.NoError(t, err)
	blocks, err := e.Blocks(state)
	require.NoError(t, err)
	var got []string
	for _, b := range blocks {
		got = append(got, b.Type+":"+b.Text)
	}
	assert.Equal(t, []string{"heading:title", "blockquote:A", "paragraph:c"}, got)
}
