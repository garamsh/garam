package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
)

func TestProbe_AnswersOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cmd := newProbeCommand()
	cmd.SetArgs([]string{srv.URL})
	require.NoError(t, cmd.Execute())
}

func TestProbe_RefusesUnready(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	cmd := newProbeCommand()
	cmd.SetArgs([]string{srv.URL})
	err := cmd.Execute()
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "want an unavailable kind, got %v", err)
}

func TestProbe_RefusesSilence(t *testing.T) {
	// A listener that is never there: the probe cannot pass on nothing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	addr := srv.URL
	srv.Close()

	cmd := newProbeCommand()
	cmd.SetArgs([]string{addr})
	require.Error(t, cmd.Execute())
}
