package api

import (
	"context"
	"log/slog"
	"net/http"

	apigen "github.com/garamsh/garam/cowork/api"
	"github.com/garamsh/garam/platform/sdk/errorx"
)

// The probes every cowork serve answers: live while the process runs,
// ready while its database answers.
const (
	LivePath  = "GET /healthz"
	ReadyPath = "GET /readyz"
)

// Handler is cowork's whole HTTP surface. ready answers whether the
// server's dependencies answer, and its error is the readiness
// probe's.
func (s *Server) Handler(ready func(context.Context) error) http.Handler {
	mux := http.NewServeMux()
	apigen.HandlerWithOptions(s, apigen.StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: BindingError})
	mux.HandleFunc(SyncPath, s.SyncPage)
	mux.HandleFunc(ChannelLivePath, s.ChannelLive)
	mux.HandleFunc(LivePath, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc(ReadyPath, func(w http.ResponseWriter, r *http.Request) {
		if err := ready(r.Context()); err != nil {
			writeError(w, r, errorx.Wrap(err, "not ready", errorx.KindUnavailable))
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return recoverPanic(mux)
}

// recoverPanic answers a handler's panic with a 500 rather than the
// closed connection net/http leaves (stack-go.md §4).
func recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				slog.ErrorContext(r.Context(), "cowork: handler panicked", slog.Any("panic", v), slog.String("path", r.URL.Path))
				writeError(w, r, errorx.New(errorx.KindInternal, "panic"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
