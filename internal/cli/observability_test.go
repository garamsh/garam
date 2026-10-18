package cli_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewObservability_CollectsIntoOneRegistry reads the /metrics
// `serve api` serves.
func TestNewObservability_CollectsIntoOneRegistry(t *testing.T) {
	resp, err := http.Get(servedAPI(t) + "/metrics")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	scraped, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(t, string(scraped), "target_info", "the exporter collects into the registry")
	assert.Contains(t, string(scraped), "go_goroutines", "and so does the Go runtime")
}
