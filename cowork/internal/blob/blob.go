// Package blob is where cowork keeps bytes it never edits in place:
// each artifact version, written once under a key and read back
// whole (ADR-0127 §2, §8). The port is all the rest of cowork sees,
// so the store behind it is chosen by configuration alone: `s3/`
// serves RustFS, AWS S3 and MinIO, and `memory/` serves tests.
package blob

import (
	"context"

	"github.com/garamsh/garam/lib/errorx"
)

// Blob is what a key holds: its bytes and the media type they were
// written with.
type Blob struct {
	ContentType string
	Body        []byte
}

// Store keeps blobs by key. A key is written once: Put on a key that
// holds a blob is ErrExists, so nothing kept is ever replaced.
type Store interface {
	Put(ctx context.Context, key string, b Blob) error
	// Get answers the blob under key, ErrNotFound where there is none.
	Get(ctx context.Context, key string) (Blob, error)
}

var (
	ErrNotFound = errorx.New(errorx.KindNotFound, "blob not found")
	ErrExists   = errorx.New(errorx.KindAlreadyExists, "a blob is already kept under that key")
)
