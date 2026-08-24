// Internal test package: the provider assembly and the readiness check
// over the engine channel are white-box seams — the serve command
// cannot run in a unit test, so both are exercised directly.
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

	"github.com/garamsh/garam/internal/authn"
	"github.com/garamsh/garam/internal/errorx"
)

func TestGoogleProvider_NotConfigured(t *testing.T) {
	// No credentials: no provider, and no discovery round trip — the
	// default issuer would answer here, so a nil provider can only
	// come from construction never being attempted.
	p, err := googleProvider(context.Background(), authn.GoogleConfig{})
	require.NoError(t, err)
	assert.Nil(t, p)
}

func TestGoogleProvider_RefusesPartialCredentials(t *testing.T) {
	tests := []struct {
		name string
		cfg  authn.GoogleConfig
	}{
		{"client id without secret", authn.GoogleConfig{ClientID: "client-id"}},
		{"secret without client id", authn.GoogleConfig{ClientSecret: "secret"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := googleProvider(context.Background(), tc.cfg)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), "kind: %v", err)
		})
	}
}

// TestGoogleProvider_DiscoveryFailure is also the control for the two
// gates above: a fully configured provider reaches construction, so
// their refusals come from the credential check and not from a helper
// that refuses everything.
func TestGoogleProvider_DiscoveryFailure(t *testing.T) {
	// Port 1 is reserved and unservable, so discovery cannot connect.
	_, err := googleProvider(context.Background(), authn.GoogleConfig{
		ClientID:     "client-id",
		ClientSecret: "secret",
		Issuer:       "http://127.0.0.1:1",
	})
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "kind: %v", err)
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
