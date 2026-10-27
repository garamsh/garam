package collab

import (
	"context"

	"github.com/garamsh/garam/platform/sdk/errorx"
)

// EditKind is what one edit does to a page's blocks.
type EditKind string

const (
	EditAppend  EditKind = "append"
	EditInsert  EditKind = "insert"
	EditReplace EditKind = "replace"
	EditDelete  EditKind = "delete"
)

// Edit is one change to a page's blocks: Block at Index, for an insert
// or a replace, or after the last for an append; the block at Index
// gone, for a delete.
type Edit struct {
	Kind  EditKind
	Index int
	Block Block
}

// ErrNotWritable is an edit to a room whose caller may only read.
var ErrNotWritable = errorx.New(errorx.KindPermissionDenied, "this page is read-only for you")

// ErrInvalidEdit is an edit that names no kind this package defines.
var ErrInvalidEdit = errorx.New(errorx.KindInvalidArgument, "an edit is append, insert, replace or delete")

// Read answers the page's blocks as they now stand.
func (s *service) Read(ctx context.Context, room Room) ([]Block, error) {
	state, err := s.state(ctx, room)
	if err != nil {
		return nil, err
	}
	return s.engine.Blocks(state)
}

// Apply makes edits to the page, in order, as one client would, and
// answers the blocks after them. Every update is kept and sent to the
// page's connections like a client's, so open editors merge it live.
// An edit that fails leaves the ones before it made.
func (s *service) Apply(ctx context.Context, room Room, edits []Edit) ([]Block, error) {
	if !room.Writable {
		return nil, ErrNotWritable
	}
	state, err := s.state(ctx, room)
	if err != nil {
		return nil, err
	}
	editor, err := s.engine.Open(state)
	if err != nil {
		return nil, err
	}
	defer editor.Close()
	for _, e := range edits {
		var update []byte
		switch e.Kind {
		case EditAppend:
			update, err = editor.AppendBlock(e.Block)
		case EditInsert:
			update, err = editor.InsertBlock(e.Index, e.Block)
		case EditReplace:
			update, err = editor.ReplaceBlock(e.Index, e.Block)
		case EditDelete:
			update, err = editor.DeleteBlock(e.Index)
		default:
			err = ErrInvalidEdit
		}
		if err != nil {
			return nil, err
		}
		if err := s.apply(ctx, room, nil, update); err != nil {
			return nil, err
		}
	}
	return s.Read(ctx, room)
}
