package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/health"
)

// fixedDependency answers the same way every time: a dependency that
// is there, or one that is not.
type fixedDependency struct {
	name string
	err  error
}

func (d fixedDependency) Name() string                { return d.name }
func (d fixedDependency) Check(context.Context) error { return d.err }
func available(name string) fixedDependency           { return fixedDependency{name: name} }
func unavailable(name string) fixedDependency {
	return fixedDependency{name: name, err: errorx.New(errorx.KindUnavailable, name+" is down")}
}

// probe asks mux the question a scheduler asks.
func probe(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func serving(deps ...health.Dependency) *http.ServeMux {
	mux := http.NewServeMux()
	health.Register(mux, deps...)
	return mux
}

// TestReady_NamesTheDependencyThatIsUnavailable covers what the
// readiness answer turns on: the dependencies the process was given.
// The rows are read together and each body in full — a handler that
// answered with an empty body would satisfy every row's status code,
// and naming no dependency would satisfy the first two.
func TestReady_NamesTheDependencyThatIsUnavailable(t *testing.T) {
	cases := map[string]struct {
		deps []health.Dependency
		code int
		body string
	}{
		"no dependency to report": {nil, http.StatusOK, "ok\n"},
		"every one answers": {
			[]health.Dependency{available("database"), available("authz")},
			http.StatusOK, "ok\n",
		},
		"one does not answer": {
			[]health.Dependency{available("database"), unavailable("authz")},
			http.StatusServiceUnavailable, "authz unavailable\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			rec := probe(t, serving(c.deps...), health.ReadyPath)
			assert.Equal(t, c.code, rec.Code)
			assert.Equal(t, c.body, rec.Body.String())
		})
	}
}

func TestLive_AnswersWhileADependencyIsUnavailable(t *testing.T) {
	// One process, one broken dependency, both probes: liveness answers
	// for the process and readiness for the dependency, so a restart is
	// not what a database blip asks for.
	mux := serving(unavailable("database"))

	live := probe(t, mux, health.LivePath)
	assert.Equal(t, http.StatusOK, live.Code)

	ready := probe(t, mux, health.ReadyPath)
	assert.Equal(t, http.StatusServiceUnavailable, ready.Code)
}

// pinger is a database that answers or does not.
type pinger struct{ err error }

func (p pinger) Ping(context.Context) error { return p.err }

func TestDatabase_ReportsAPingThatFails(t *testing.T) {
	unreachable := errorx.New(errorx.KindUnavailable, "dial tcp: connection refused")
	err := health.Database(pinger{err: unreachable}).Check(context.Background())
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
	assert.Equal(t, "database", health.Database(pinger{err: unreachable}).Name())

	require.NoError(t, health.Database(pinger{}).Check(context.Background()))
}
