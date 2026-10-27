package integration_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/blob/blobtest"
	"github.com/garamsh/garam/cowork/internal/blob/memory"
	"github.com/garamsh/garam/cowork/internal/blob/s3"
)

// rustfs is the s3 store over the RustFS `make db-up` starts.
func rustfs(t *testing.T) *s3.Store {
	t.Helper()
	endpoint := os.Getenv("COWORK_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Fatal("COWORK_TEST_S3_ENDPOINT is unset — run the suite through `make integration`")
	}
	store, err := s3.New(s3.Config{
		Endpoint: endpoint, Bucket: "cowork-test", PathStyle: true,
		AccessKey: os.Getenv("COWORK_TEST_S3_ACCESS_KEY"), SecretKey: os.Getenv("COWORK_TEST_S3_SECRET_KEY"),
	})
	require.NoError(t, err)
	require.NoError(t, store.EnsureBucket(context.Background()))
	return store
}

// TestBlobStore_EveryAdapterKeepsOneContract runs the same suite
// against the memory store and the s3 store over RustFS: what cowork
// sees of either is the same, so swapping them is configuration.
func TestBlobStore_EveryAdapterKeepsOneContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) { blobtest.Run(t, memory.New()) })
	t.Run("s3 over RustFS", func(t *testing.T) { blobtest.Run(t, rustfs(t)) })
}
