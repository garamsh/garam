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

// TestProbe_AnswersOnlyAReadyListener reads the three refusals against
// the listener that answers: one command, four listeners differing
// only in what they do with the request, so a probe that fails whatever
// it is pointed at stops satisfying the first assertion.
func TestProbe_AnswersOnlyAReadyListener(t *testing.T) {
	ready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ready.Close()

	unready := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
	}))
	defer unready.Close()

	// The upper boundary of what the probe takes for ok. The compose
	// healthchecks ask this binary whether a process can serve, and a
	// 3xx is an answer about where to look rather than one that it can
	// — so the first status past 2xx is refused, not the first past 3xx.
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusMultipleChoices)
	}))
	defer redirect.Close()

	// A listener that is never there: the probe cannot pass on nothing.
	silent := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	silentURL := silent.URL
	silent.Close()

	require.NoError(t, runProbe(ready.URL))

	err := runProbe(unready.URL)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "want an unavailable kind, got %v", err)

	err = runProbe(redirect.URL)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "want an unavailable kind, got %v", err)

	require.Error(t, runProbe(silentURL))
}
