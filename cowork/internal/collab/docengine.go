package collab

// DocEngine reads and writes a page's Yjs document on the server
// (ADR-0127). A document travels as a Yjs update, the encoding the
// clients send; the operations on updates decode no document, so the
// relay's own work stays cheap.
type DocEngine interface {
	// Merge folds updates into one update holding all of them.
	Merge(updates [][]byte) ([]byte, error)
	// StateVector answers what the document holds, as a client sends
	// it to ask for what it lacks.
	StateVector(state []byte) ([]byte, error)
	// Diff answers the part of state a client holding stateVector
	// lacks.
	Diff(state, stateVector []byte) ([]byte, error)
	// Blocks answers the page's top-level blocks, in order.
	Blocks(state []byte) ([]Block, error)
	// Open answers an editor of a document holding state, which edits
	// it as a client would.
	Open(state []byte) (Editor, error)
}

// Editor edits one document. Each edit answers the update it made, to
// be kept and sent on as a client's would be.
type Editor interface {
	AppendBlock(b Block) ([]byte, error)
	InsertBlock(index int, b Block) ([]byte, error)
	ReplaceBlock(index int, b Block) ([]byte, error)
	DeleteBlock(index int) ([]byte, error)
	Close()
}
