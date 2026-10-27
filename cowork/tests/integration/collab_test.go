package integration_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/collab"
)

// containsAll reports whether text holds every token.
func containsAll(text string, tokens []string) bool {
	for _, tok := range tokens {
		if !strings.Contains(text, tok) {
			return false
		}
	}
	return true
}

// TestSync_InterleavedEditsConverge has two people type into the same
// paragraph at once, each edit sent before the other's arrives, and
// waits until both documents read the same. Ada types at the start and
// bob at the end of the text bob holds, so no insertion lands inside
// another's token and each token can be found whole. Every token either
// typed is in the result, so no edit was lost, and a third editor
// opening the page afterwards reads the same from what the server
// kept.
func TestSync_InterleavedEditsConverge(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "notes")
	ada, bob := newEditor(t, s, "ada", pageID), newEditor(t, s, "bob", pageID)
	ada.connect()
	bob.connect()
	ada.appendBlock(collab.Block{Type: "paragraph", Text: "start"})
	eventually(t, "bob receives ada's paragraph", func() bool { return bob.text() == "start" })

	var tokens []string
	for i := range 20 {
		a, b := fmt.Sprintf("<a%d>", i), fmt.Sprintf("<b%d>", i)
		tokens = append(tokens, a, b)
		ada.insertText(0, 0, a)
		bob.typeAtEnd(0, b)
	}

	eventually(t, "both documents converge on every edit", func() bool {
		at, bt := ada.text(), bob.text()
		return at == bt && containsAll(at, tokens)
	})
	late := newEditor(t, s, "ada", pageID)
	late.connect()
	eventually(t, "a later editor reads what the server kept", func() bool { return late.text() == ada.text() })
}

// TestSync_OfflineEditsMergeOnReconnect takes bob offline while both
// keep typing, then reconnects bob. Both documents converge on both
// sides' edits, the server kept bob's offline edits, and ada's, made
// while bob was away, reached bob on reconnect.
func TestSync_OfflineEditsMergeOnReconnect(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "notes")
	ada, bob := newEditor(t, s, "ada", pageID), newEditor(t, s, "bob", pageID)
	ada.connect()
	bob.connect()
	ada.appendBlock(collab.Block{Type: "paragraph", Text: "shared"})
	eventually(t, "bob holds the paragraph", func() bool { return bob.text() == "shared" })

	bob.disconnect()
	ada.insertText(0, 0, "[ada online]")
	bob.insertText(0, len("shared"), "[bob offline]")
	bob.appendBlock(collab.Block{Type: "paragraph", Text: "[bob's new block]"})
	require.NotContains(t, ada.text(), "[bob offline]", "an offline edit is not sent")

	bob.connect()
	tokens := []string{"[ada online]", "[bob offline]", "[bob's new block]"}
	eventually(t, "both converge on both sides' edits", func() bool {
		return ada.text() == bob.text() && containsAll(ada.text(), tokens)
	})
	fresh := newEditor(t, s, "ada", pageID)
	fresh.connect()
	eventually(t, "the server kept the offline edits", func() bool { return containsAll(fresh.text(), tokens) })
}

// TestSync_KeepsEveryBlockTypeInOrder writes one block of each type
// the web app's editor offers and reads them back, in order and with
// their attributes, from what the server kept.
func TestSync_KeepsEveryBlockTypeInOrder(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "blocks")
	writer := newEditor(t, s, "ada", pageID)
	writer.connect()
	want := []collab.Block{
		{Type: "paragraph", Text: "body"},
		{Type: "heading", Attrs: map[string]any{"level": float64(1)}, Text: "title"},
		{Type: "bulletList", Text: "bullet", Items: []collab.Item{{Text: "bullet"}}},
		{Type: "orderedList", Text: "first", Items: []collab.Item{{Text: "first"}}},
		{Type: "taskList", Text: "to do", Items: []collab.Item{{Text: "to do", Checked: true}}},
		{Type: "codeBlock", Attrs: map[string]any{"language": "go"}, Text: "x := 1"},
		{Type: "blockquote", Text: "quoted"},
		{Type: "horizontalRule"},
		{Type: "image", Attrs: map[string]any{"src": "https://example.com/a.png", "alt": "a"}},
	}
	for _, b := range want {
		writer.appendBlock(b)
	}

	reader := newEditor(t, s, "bob", pageID)
	reader.connect()
	eventually(t, "the reader holds every block", func() bool { return len(reader.blocks()) == len(want) })
	got := reader.blocks()
	for i, b := range want {
		assert.Equal(t, b.Type, got[i].Type, "block %d", i)
		assert.Equal(t, b.Text, got[i].Text, "block %d", i)
		for k, v := range b.Attrs {
			assert.Equal(t, v, got[i].Attrs[k], "block %d attribute %s", i, k)
		}
		assert.Equal(t, b.Items, got[i].Items, "block %d items", i)
	}
}

// TestSync_CompactsTheLogAndLosesNothing sends more updates than one
// snapshot may trail by, and holds that the log was compacted, fewer
// rows than updates, while a fresh editor still reads every edit.
func TestSync_CompactsTheLogAndLosesNothing(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "long")
	writer := newEditor(t, s, "ada", pageID)
	writer.connect()
	writer.appendBlock(collab.Block{Type: "paragraph", Text: "."})
	const edits = 260
	for i := range edits {
		writer.insertText(0, 0, fmt.Sprintf("<%d>", i))
	}

	fresh := newEditor(t, s, "bob", pageID)
	fresh.connect()
	eventually(t, "every edit is read back", func() bool { return fresh.text() == writer.text() })
	var rows, snapshots int
	require.NoError(t, s.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM page_updates WHERE page_id = $1", pageID).Scan(&rows))
	require.NoError(t, s.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM page_snapshots WHERE page_id = $1", pageID).Scan(&snapshots))
	assert.Equal(t, 1, snapshots)
	assert.Less(t, rows, edits, "the log was compacted")
}

// TestSync_RefusesEditsFromACallerWhoMayOnlyRead denies bob writing
// the page. Bob still connects and reads, bob's edit is answered with
// y-websocket's permission-denied message, and nobody else receives
// or keeps it. Ada's edit in the same page, allowed, is the control.
func TestSync_RefusesEditsFromACallerWhoMayOnlyRead(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "locked")
	s.platform.denyCheck("bob cowork:page-write@" + pageID)
	ada, bob := newEditor(t, s, "ada", pageID), newEditor(t, s, "bob", pageID)
	ada.connect()
	bob.connect()

	ada.appendBlock(collab.Block{Type: "paragraph", Text: "ada's"})
	eventually(t, "bob reads ada's edit", func() bool { return bob.text() == "ada's" })
	bob.appendBlock(collab.Block{Type: "paragraph", Text: "bob's"})
	eventually(t, "bob is told the edit was refused", bob.wasDenied)

	fresh := newEditor(t, s, "ada", pageID)
	fresh.connect()
	eventually(t, "a fresh editor reads ada's edit", func() bool { return strings.Contains(fresh.text(), "ada's") })
	assert.NotContains(t, fresh.text(), "bob's", "the refused edit was not kept")
	assert.NotContains(t, ada.text(), "bob's", "the refused edit was not relayed")
}

// TestSync_RefusesTheUpgradeAsGetPageWould answers the upgrade as the
// page's read would be answered: eve, no member, 403; a page that does
// not exist, 404; no credential, 401. Ada's upgrade to the page is the
// control.
func TestSync_RefusesTheUpgradeAsGetPageWould(t *testing.T) {
	s := newStack(t)
	pageID := s.createPage(t, "p")
	_, status, err := newEditor(t, s, "ada", pageID).dial()
	require.NoError(t, err)
	assert.Equal(t, http.StatusSwitchingProtocols, status)

	for _, c := range []struct {
		who, page string
		want      int
	}{
		{"eve", pageID, http.StatusForbidden},
		{"ada", "nosuchpage00", http.StatusNotFound},
		{"nobody", pageID, http.StatusUnauthorized},
	} {
		_, status, err := newEditor(t, s, c.who, c.page).dial()
		assert.Error(t, err)
		assert.Equal(t, c.want, status, "%s on %s", c.who, c.page)
	}
}
