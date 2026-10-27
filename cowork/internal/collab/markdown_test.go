package collab_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/garamsh/garam/cowork/internal/collab"
)

// TestMarkdown_RendersEveryBlockType renders one block of each type the
// editor offers and reads the Markdown a reader would write for it.
func TestMarkdown_RendersEveryBlockType(t *testing.T) {
	got := collab.Markdown([]collab.Block{
		{Type: "heading", Attrs: map[string]any{"level": float64(2)}, Text: "Plan"},
		{Type: "paragraph", Text: "Body"},
		{Type: "bulletList", Items: []collab.Item{{Text: "a"}, {Text: "b"}}},
		{Type: "orderedList", Items: []collab.Item{{Text: "first"}, {Text: "second"}}},
		{Type: "taskList", Items: []collab.Item{{Text: "done", Checked: true}, {Text: "todo"}}},
		{Type: "codeBlock", Attrs: map[string]any{"language": "go"}, Text: "x := 1"},
		{Type: "blockquote", Text: "one\ntwo"},
		{Type: "horizontalRule"},
		{Type: "image", Attrs: map[string]any{"src": "https://example.com/a.png", "alt": "a"}},
		{Type: "artifact", Attrs: map[string]any{"artifact": "k3v7q2m9x4ab"}},
	})
	assert.Equal(t, "## Plan\n\nBody\n\n- a\n- b\n\n1. first\n2. second\n\n- [x] done\n- [ ] todo\n\n"+
		"```go\nx := 1\n```\n\n> one\n> two\n\n---\n\n![a](https://example.com/a.png)\n\n[artifact k3v7q2m9x4ab]\n", got)
}
