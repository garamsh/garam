// Package blobtest is the contract every blob.Store adapter keeps,
// run against each, so that swapping one for another changes nothing
// cowork can see.
package blobtest

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/blob"
	"github.com/garamsh/garam/lib/errorx"
)

// Run holds s to the contract. Each run writes under keys of its own,
// so it runs against a store that holds other blobs.
func Run(t *testing.T, s blob.Store) {
	ctx := context.Background()
	prefix := rand.Text()

	t.Run("reads back what it kept, bytes and media type", func(t *testing.T) {
		body := []byte("<!doctype html><p>hello</p>\x00\xff")
		require.NoError(t, s.Put(ctx, prefix+"/a", blob.Blob{ContentType: "text/html; charset=utf-8", Body: body}))
		got, err := s.Get(ctx, prefix+"/a")
		require.NoError(t, err)
		assert.Equal(t, body, got.Body)
		assert.Equal(t, "text/html; charset=utf-8", got.ContentType)
	})

	t.Run("never replaces a kept blob", func(t *testing.T) {
		require.NoError(t, s.Put(ctx, prefix+"/b", blob.Blob{ContentType: "text/plain", Body: []byte("first")}))
		err := s.Put(ctx, prefix+"/b", blob.Blob{ContentType: "text/plain", Body: []byte("second")})
		assert.True(t, errorx.IsKind(err, errorx.KindAlreadyExists), "%v", err)
		got, err := s.Get(ctx, prefix+"/b")
		require.NoError(t, err)
		assert.Equal(t, []byte("first"), got.Body)
	})

	t.Run("answers not found for a key it never kept", func(t *testing.T) {
		_, err := s.Get(ctx, prefix+"/missing")
		assert.True(t, errorx.IsKind(err, errorx.KindNotFound), "%v", err)
	})

	t.Run("keeps an empty blob and a large one", func(t *testing.T) {
		require.NoError(t, s.Put(ctx, prefix+"/empty", blob.Blob{ContentType: "text/plain"}))
		got, err := s.Get(ctx, prefix+"/empty")
		require.NoError(t, err)
		assert.Empty(t, got.Body)

		large := bytes.Repeat([]byte("0123456789abcdef"), 1<<16) // 1 MiB
		require.NoError(t, s.Put(ctx, prefix+"/large", blob.Blob{ContentType: "application/octet-stream", Body: large}))
		got, err = s.Get(ctx, prefix+"/large")
		require.NoError(t, err)
		assert.Equal(t, len(large), len(got.Body))
		assert.True(t, bytes.Equal(large, got.Body))
	})

	t.Run("keeps keys apart", func(t *testing.T) {
		for i := range 3 {
			require.NoError(t, s.Put(ctx, fmt.Sprintf("%s/many/%d", prefix, i), blob.Blob{ContentType: "text/plain", Body: []byte{byte('a' + i)}}))
		}
		for i := range 3 {
			got, err := s.Get(ctx, fmt.Sprintf("%s/many/%d", prefix, i))
			require.NoError(t, err)
			assert.Equal(t, []byte{byte('a' + i)}, got.Body)
		}
	})
}
