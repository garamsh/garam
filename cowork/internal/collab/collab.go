// Package collab is real-time co-editing of a cowork page: the Yjs
// sync protocol a client speaks over a WebSocket, relayed between the
// clients on one page and kept as the page's update log (ADR-0127).
package collab

// Block is one top-level node of a page: its type as the editor names
// it (`paragraph`, `heading`, `bulletList` ...), its attributes, and
// its text with formatting dropped. A list's text is its items' joined
// by newlines, and Items holds them one by one.
type Block struct {
	Type  string         `json:"type" jsonschema:"paragraph, heading, bulletList, orderedList, taskList, blockquote, codeBlock or horizontalRule"`
	Attrs map[string]any `json:"attrs,omitempty" jsonschema:"the block's attributes: a heading's level, a code block's language"`
	Text  string         `json:"text,omitempty" jsonschema:"the block's text; for a list, its items' text, one per line, as read"`
	Items []Item         `json:"items,omitempty" jsonschema:"a list's items, in order"`
}

// Item is one entry of a list block. Checked is a to-do item's.
type Item struct {
	Text    string `json:"text" jsonschema:"the item's text"`
	Checked bool   `json:"checked,omitempty" jsonschema:"whether a to-do item is done"`
}
