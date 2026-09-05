package cli

import (
	"context"
	"log/slog"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
)

// shutdownGrace bounds how long a serve command lets in-flight work
// finish after it is asked to stop, before abandoning it deliberately:
// shorter than the 30s terminationGracePeriodSeconds a pod defaults to,
// so the bound is the binary's rather than the kubelet's SIGKILL.
const shutdownGrace = 10 * time.Second

// serveSignalContext cancels when the process is asked to stop. Without
// it SIGTERM is the runtime's default action — death by the signal,
// which as container PID 1 the kernel turns into exit code 2 — and a
// requested shutdown reads as a failed one (#338).
func serveSignalContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
}

// serveHTTP runs serve until it fails or ctx is cancelled. Cancellation
// shuts the server down — the listener closes at once, in-flight
// requests get grace to finish, and whatever has not finished by then
// is dropped — and returns nil either way, because a requested stop is
// not a failure. grace is shutdownGrace at every call site, and a
// parameter so that a test reaches the branch past it in milliseconds.
func (o *observability) serveHTTP(ctx context.Context, grace time.Duration, srv *http.Server, serve func() error) error {
	errCh := make(chan error, 1)
	go func() { errCh <- serve() }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Past the bound the connections still open are dropped, not
		// held. Neither this error nor Close's is returned: the stop
		// was requested, and abandoning what did not finish is this
		// function's decision rather than a failure of it, so the
		// process exits 0 as serveGRPC does below
		// (deployment-contract.md §Termination).
		_ = srv.Close()
		o.logAbandoned(ctx, grace, err)
	}
	return nil
}

// serveGRPC is serveHTTP for the engine: GracefulStop lets in-flight
// RPCs finish, and past grace Stop abandons them.
func (o *observability) serveGRPC(ctx context.Context, grace time.Duration, srv *grpc.Server, serve func() error) error {
	errCh := make(chan error, 1)
	go func() { errCh <- serve() }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	stopped := make(chan struct{})
	go func() { srv.GracefulStop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(grace):
		srv.Stop()
		<-stopped
		o.logAbandoned(ctx, grace, nil)
	}
	return nil
}

// logAbandoned records a termination that dropped work still in flight;
// one that finished inside the bound writes nothing, being the ordinary
// case. Both exit 0 and neither leaves a request line, so this is what
// tells them apart, and dropping the work is this process's own
// decision rather than a failure of it, so the level stops at warn
// (deployment-contract.md §Termination). cause is what the server
// reported where it reports anything — GracefulStop reports nothing.
func (o *observability) logAbandoned(ctx context.Context, grace time.Duration, cause error) {
	attrs := []slog.Attr{slog.Duration("grace", grace)}
	if cause != nil {
		attrs = append(attrs, slog.String("error", cause.Error()))
	}
	o.logger.LogAttrs(ctx, slog.LevelWarn, "shutdown abandoned work in flight", attrs...)
}
