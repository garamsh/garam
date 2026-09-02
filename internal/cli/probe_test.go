package cli_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/errorx"
)

func runProbe(url string) error {
	root := cli.NewRootCommand()
	root.SetArgs([]string{"probe", url})
	return root.Execute()
}

func TestProbe_AnswersOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	require.NoError(t, runProbe(srv.URL))
}

func TestProbe_RefusesUnready(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	err := runProbe(srv.URL)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "want an unavailable kind, got %v", err)
}

// TestProbe_RefusesTheFirstStatusThatIsNotSuccess covers the upper
// boundary of what the probe takes for ok. The compose healthchecks ask
// this binary whether a process can serve, and a 3xx is an answer about
// where to look rather than one that it can — so the first status past
// 2xx is refused, not the first status past 3xx.
func TestProbe_RefusesTheFirstStatusThatIsNotSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer srv.Close()

	err := runProbe(srv.URL)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "want an unavailable kind, got %v", err)
}

func TestProbe_RefusesSilence(t *testing.T) {
	// A listener that is never there: the probe cannot pass on nothing.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	addr := srv.URL
	srv.Close()

	require.Error(t, runProbe(addr))
}
