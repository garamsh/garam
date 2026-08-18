package cli

import (
	"context"
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
// shuts the server down — the listener closes at once and in-flight
// requests finish within shutdownGrace — and returns nil, because a
// requested stop is not a failure.
func serveHTTP(ctx context.Context, srv *http.Server, serve func() error) error {
	errCh := make(chan error, 1)
	go func() { errCh <- serve() }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		// Past the bound the connections still open are dropped, not
		// held.
		_ = srv.Close()
		return err
	}
	return nil
}

// serveGRPC is serveHTTP for the engine: GracefulStop lets in-flight
// RPCs finish, and past shutdownGrace Stop abandons them.
func serveGRPC(ctx context.Context, srv *grpc.Server, serve func() error) error {
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
	case <-time.After(shutdownGrace):
		srv.Stop()
		<-stopped
	}
	return nil
}
