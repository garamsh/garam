// Package health answers the two questions a scheduler asks about a
// garam process: whether to restart it, and whether to send it
// traffic. Every serve command mounts both answers, on a port a probe
// reaches without credentials.
package health

import (
	"context"
	"net/http"
	"time"

	"github.com/garamsh/garam/internal/errorx"
)

// The paths the two probes answer on. One spelling, so three processes
// are configured the same way.
const (
	LivePath  = "/healthz"
	ReadyPath = "/readyz"
)

// readyTimeout bounds one readiness answer: a dependency that has
// stopped answering is not owed the probe's patience, and a check
// assumes nothing about how long one takes.
const readyTimeout = 2 * time.Second

// Dependency is one thing a process needs before traffic reaches it.
type Dependency interface {
	// Name is what a failing readiness answer reports.
	Name() string
	// Check reports whether the dependency answers.
	Check(ctx context.Context) error
}

// Register mounts both probes on mux: liveness, which answers for the
// process alone, and readiness, which answers for deps as well. A
// dependency belongs in deps and never in liveness (ADR-0021).
func Register(mux *http.ServeMux, deps ...Dependency) {
	mux.Handle("GET "+LivePath, live())
	mux.Handle("GET "+ReadyPath, ready(deps))
}

// live answers for the process itself, which reaching the handler is
// the whole of.
func live() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeOK(w)
	})
}

// ready answers whether every dependency is there, naming the first
// that is not. The name and nothing else: this port carries no
// credential, so an error's own text — a connection string among it —
// stays out of the body.
func ready(deps []Dependency) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		for _, d := range deps {
			if err := d.Check(ctx); err != nil {
				http.Error(w, d.Name()+" unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		writeOK(w)
	})
}

func writeOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// Pinger is the database as a readiness check sees it, satisfied by
// *pgxpool.Pool.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Database is the dependency a process's own database is. Every serve
// command has one, and reports it under the one name.
func Database(p Pinger) Dependency {
	return database{pinger: p}
}

type database struct{ pinger Pinger }

func (database) Name() string { return "database" }

func (d database) Check(ctx context.Context) error {
	if err := d.pinger.Ping(ctx); err != nil {
		return errorx.Wrap(err, "database", errorx.KindUnavailable)
	}
	return nil
}
