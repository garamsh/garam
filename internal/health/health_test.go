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

func TestReady_NamesTheDependencyThatIsUnavailable(t *testing.T) {
	// The same two dependencies, differing only in whether the second
	// one answers: what the readiness answer turns on.
	down := probe(t, serving(available("database"), unavailable("authz")), health.ReadyPath)
	assert.Equal(t, http.StatusServiceUnavailable, down.Code)
	assert.Contains(t, down.Body.String(), "authz")

	up := probe(t, serving(available("database"), available("authz")), health.ReadyPath)
	assert.Equal(t, http.StatusOK, up.Code)
	assert.Contains(t, up.Body.String(), "ok")
}

func TestReady_ReportsNoDependencyItDoesNotHave(t *testing.T) {
	// A process readiness is given no dependency for is ready, and the
	// body says nothing about one.
	rec := probe(t, serving(), health.ReadyPath)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "unavailable")
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
