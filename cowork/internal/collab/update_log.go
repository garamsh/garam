package collab

import "context"

// Document is a page's content as stored: a snapshot, then every
// update appended since, through the update numbered Through.
type Document struct {
	Snapshot []byte
	Updates  [][]byte
	Through  int64
}

// UpdateLog keeps each page's updates, a page named by its
// organization and its ID, in the order they arrived, and
// the snapshot they are compacted into (ADR-0127). A page's log is
// deleted with the page.
type UpdateLog interface {
	Load(ctx context.Context, org, page string) (Document, error)
	// Append stores an update and answers how many updates now follow
	// the snapshot. A page that no longer exists is not found.
	Append(ctx context.Context, org, page string, update []byte) (int, error)
	// Compact replaces the snapshot with merged, which folds in every
	// update through through, and drops those updates. Updates
	// appended after through stay, and a compaction through an earlier
	// update than the stored snapshot's changes nothing.
	Compact(ctx context.Context, org, page string, merged []byte, through int64) error
}
