// Internal test package: serveHTTP and serveGRPC are unexported wiring
// with no exported surface that reaches them.
package cli

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	authzv1 "github.com/garamsh/garam/proto/gen/garam/authz/v1"
)

// testShutdownGrace stands in for shutdownGrace, so the branch past the
// bound is reached in milliseconds. Long enough that work which did
// finish still finishes on a loaded machine.
const testShutdownGrace = 250 * time.Millisecond

// TestServeHTTP_RecordsATerminationThatDroppedWorkInFlight covers what
// a termination's logs could not answer: whether it dropped a request
// or every request finished inside the bound (#1064). Both exit 0,
// which the first assertion of each row holds.
//
// The row that finished is the control. It is what fails when the line
// is written for having shut down at all rather than for having dropped
// something, and it holds the whole buffer empty rather than one field
// absent, so a line written under any other message fails it too.
func TestServeHTTP_RecordsATerminationThatDroppedWorkInFlight(t *testing.T) {
	// Internal: no serve command holds a request past its grace.
	tests := []struct {
		name  string
		holds bool
	}{
		{name: "a request that finishes inside the grace", holds: false},
		{name: "a request that outlives it", holds: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out lines
			obs := recordingObservability(&out)

			entered, release := make(chan struct{}), make(chan struct{})
			t.Cleanup(func() { close(release) })
			srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				if tc.holds {
					select {
					case <-release:
					case <-r.Context().Done():
					}
				}
				w.WriteHeader(http.StatusNoContent)
			})}
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(context.Background())
			served := make(chan error, 1)
			go func() {
				served <- obs.serveHTTP(ctx, testShutdownGrace, srv, func() error { return srv.Serve(lis) })
			}()

			client := &http.Client{}
			t.Cleanup(client.CloseIdleConnections)
			answered := make(chan struct{})
			go func() {
				defer close(answered)
				resp, err := client.Get("http://" + lis.Addr().String() + "/")
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
			<-entered
			if !tc.holds {
				// The connection is idle before the stop is asked for,
				// so the shutdown has nothing left to wait on.
				<-answered
			}
			cancel()
			require.NoError(t, <-served, "a requested stop is not a failure")

			if !tc.holds {
				assert.Empty(t, out.String(), "a termination that dropped nothing writes no line")
				return
			}
			line := decodeLine(t, &out)
			assert.Equal(t, "shutdown abandoned work in flight", line["msg"])
			assert.Equal(t, slog.LevelWarn.String(), line["level"],
				"deliberate is not a fault of this process, and neither is it ordinary")
			assert.Equal(t, float64(testShutdownGrace), line["grace"])
			assert.Equal(t, context.DeadlineExceeded.Error(), line["error"],
				"the cause Shutdown reported, which the exit code withholds")
		})
	}
}

// blockedEngine holds a Check until it is released or its call is
// cancelled, so GracefulStop has an RPC to wait on past the bound.
// Reading the call's context is what Stop then reaches: a handler
// ignoring it outlives Stop and the abandoning branch never returns.
type blockedEngine struct {
	authzv1.UnimplementedAuthzServiceServer
	entered chan struct{}
	release chan struct{}
	holds   bool
}

func (e blockedEngine) Check(ctx context.Context, _ *authzv1.CheckRequest) (*authzv1.CheckResponse, error) {
	close(e.entered)
	if e.holds {
		select {
		case <-e.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &authzv1.CheckResponse{}, nil
}

// TestServeGRPC_RecordsATerminationThatDroppedWorkInFlight is the
// engine's half of the same question, and carries the same control.
// The line differs in one field: GracefulStop reports no cause, so
// there is none to carry.
func TestServeGRPC_RecordsATerminationThatDroppedWorkInFlight(t *testing.T) {
	// Internal: no serve command holds a call past its grace.
	tests := []struct {
		name  string
		holds bool
	}{
		{name: "an RPC that finishes inside the grace", holds: false},
		{name: "an RPC that outlives it", holds: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out lines
			obs := recordingObservability(&out)

			engine := blockedEngine{
				entered: make(chan struct{}),
				release: make(chan struct{}),
				holds:   tc.holds,
			}
			t.Cleanup(func() { close(engine.release) })
			srv := grpc.NewServer()
			authzv1.RegisterAuthzServiceServer(srv, engine)
			lis, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)

			ctx, cancel := context.WithCancel(context.Background())
			served := make(chan error, 1)
			go func() {
				served <- obs.serveGRPC(ctx, testShutdownGrace, srv, func() error { return srv.Serve(lis) })
			}()

			conn, err := grpc.NewClient(lis.Addr().String(),
				grpc.WithTransportCredentials(insecure.NewCredentials()))
			require.NoError(t, err)
			t.Cleanup(func() { _ = conn.Close() })
			answered := make(chan struct{})
			go func() {
				defer close(answered)
				_, _ = authzv1.NewAuthzServiceClient(conn).Check(context.Background(),
					&authzv1.CheckRequest{SubjectGrn: testSubject})
			}()
			<-engine.entered
			if !tc.holds {
				<-answered
			}
			cancel()
			require.NoError(t, <-served, "a requested stop is not a failure")

			if !tc.holds {
				assert.Empty(t, out.String(), "a termination that dropped nothing writes no line")
				return
			}
			line := decodeLine(t, &out)
			assert.Equal(t, "shutdown abandoned work in flight", line["msg"])
			assert.Equal(t, slog.LevelWarn.String(), line["level"])
			assert.Equal(t, float64(testShutdownGrace), line["grace"])
			assert.NotContains(t, line, "error", "GracefulStop reports no cause to carry")
		})
	}
}
