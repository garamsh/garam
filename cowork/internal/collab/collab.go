// Package collab is real-time co-editing of a cowork page: the Yjs
// sync protocol a client speaks over a WebSocket, relayed between the
// clients on one page and kept as the page's update log (ADR-0127).
package collab

// Block is one top-level node of a page: its type as the editor names
// it (`paragraph`, `heading`, `taskItem` ...), its attributes, and its
// text with formatting dropped.
type Block struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs"`
	Text  string         `json:"text"`
}
