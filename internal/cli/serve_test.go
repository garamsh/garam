// Internal test package: the dev-auth flag wiring and the readiness
// check over the engine channel are white-box seams — the serve command
// cannot run in a unit test, so the provider assembly and the check are
// exercised directly.
package cli

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/garamsh/garam/internal/errorx"
)

func TestDevProvider_Disabled(t *testing.T) {
	p, err := devProvider(false, false)
	require.NoError(t, err)
	assert.Nil(t, p)
}

func TestDevProvider_Enabled(t *testing.T) {
	p, err := devProvider(true, false)
	require.NoError(t, err)
	assert.Equal(t, "dev", p.Name())
}

func TestDevProvider_RefusesSecureCookies(t *testing.T) {
	_, err := devProvider(true, true)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
}

// channelTo returns a client channel for addr, closed with the test.
func channelTo(t *testing.T, addr string) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestAuthzChannel_RefusesAChannelThatReachesNoEngine(t *testing.T) {
	// A gRPC server on a port of the kernel's choosing is the engine;
	// the address nothing serves is the same channel without one, which
	// is what the check has to tell apart.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	engine := grpc.NewServer()
	go func() { _ = engine.Serve(lis) }()
	t.Cleanup(engine.Stop)

	served, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, authzChannel{conn: channelTo(t, lis.Addr().String())}.Check(served))

	// Port 1 is reserved and unservable, so this channel never reaches
	// Ready and the check's own deadline is what ends the wait.
	unserved, cancelUnserved := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelUnserved()
	err = authzChannel{conn: channelTo(t, "127.0.0.1:1")}.Check(unserved)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
}
