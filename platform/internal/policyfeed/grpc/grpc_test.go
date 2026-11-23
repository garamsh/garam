package grpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/resolver"
	"google.golang.org/grpc/resolver/manual"
	"google.golang.org/grpc/status"

	policyv1 "github.com/garamsh/garam/lib/policy-sdk-go/gen/garam/policy/v1"
	"github.com/garamsh/garam/platform/internal/signer"
)

// These tests are the one ADR-0178 §What a test of it must show
// specifies: a TCP proxy that blackholes without closing, a manual
// resolver and two backends. The keepalive is Dial's own, 10 s and 3 s:
// grpc-go raises a smaller Time to 10 s, so the parameters cannot be
// shortened, and each test waits out the detection window it measures.

// counting answers Evaluate and ApplyEntityChanges and counts both.
type counting struct {
	policyv1.UnimplementedPolicyServiceServer
	evaluates, applies atomic.Int64
}

func (c *counting) Evaluate(
	context.Context, *policyv1.EvaluateRequest,
) (*policyv1.EvaluateResponse, error) {
	c.evaluates.Add(1)
	return &policyv1.EvaluateResponse{Decision: true}, nil
}

func (c *counting) ApplyEntityChanges(
	context.Context, *policyv1.ApplyEntityChangesRequest,
) (*policyv1.ApplyEntityChangesResponse, error) {
	c.applies.Add(1)
	return &policyv1.ApplyEntityChangesResponse{}, nil
}

// blackhole forwards TCP to a backend until told to stop, and then
// reads and discards what a client sends without answering or closing:
// the connection of a lost Node, which the client's operating system
// does not report for minutes.
type blackhole struct {
	lis  net.Listener
	dead atomic.Bool
}

func newBlackhole(t *testing.T, backend string) *blackhole {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	b := &blackhole{lis: lis}
	var mu sync.Mutex
	var conns []net.Conn
	t.Cleanup(func() {
		_ = lis.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	go func() {
		for {
			client, err := lis.Accept()
			if err != nil {
				return
			}
			server, err := net.Dial("tcp", backend)
			if err != nil {
				_ = client.Close()
				continue
			}
			mu.Lock()
			conns = append(conns, client, server)
			mu.Unlock()
			go b.pump(server, client)
			go b.pump(client, server)
		}
	}()
	return b
}

// pump copies src to dst, and once the proxy is dead reads src to
// nowhere.
func (b *blackhole) pump(dst, src net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if err != nil {
			return
		}
		if !b.dead.Load() {
			if _, err := dst.Write(buf[:n]); err != nil {
				return
			}
		}
	}
}

func (b *blackhole) addr() string { return b.lis.Addr().String() }

// backend runs a TLS server of c on a loopback port under cert and
// answers its address.
func backend(t *testing.T, cert tls.Certificate, c *counting) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := grpc.NewServer(grpc.Creds(credentials.NewServerTLSFromCert(&cert)))
	policyv1.RegisterPolicyServiceServer(srv, c)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return lis.Addr().String()
}

// testCert is a certificate for the name the manual resolver's target
// carries, which is what Dial verifies the peer against, and the file
// holding it as the CA.
func testCert(t *testing.T) (tls.Certificate, string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "policy"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"policy"},
		IsCA:         true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	caFile := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(caFile,
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600))
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, caFile
}

// twoBackends starts one backend behind a blackhole and one beside it,
// and dials them through a manual resolver, as Dial dials a headless
// Service's two pods. Both are warmed until each has answered.
func twoBackends(t *testing.T, withoutKeepalive bool) (
	policyv1.PolicyServiceClient, *blackhole, *counting, *counting,
) {
	t.Helper()
	cert, caFile := testCert(t)
	lost, live := &counting{}, &counting{}
	hole := newBlackhole(t, backend(t, cert, lost))
	liveAddr := backend(t, cert, live)

	r := manual.NewBuilderWithScheme("garamtest")
	r.InitialState(resolver.State{Addresses: []resolver.Address{
		{Addr: hole.addr()}, {Addr: liveAddr},
	}})
	// Dial is what serve api and serve machine call; the control is the
	// same dial with the keepalive left out.
	var conn *grpc.ClientConn
	var err error
	if withoutKeepalive {
		conn, err = dial("garamtest:///policy", caFile, "https://issuer.test", testKey(t),
			keepalive.ClientParameters{}, grpc.WithResolvers(r))
	} else {
		conn, err = Dial("garamtest:///policy", caFile, "https://issuer.test", testKey(t),
			grpc.WithResolvers(r))
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	client := policyv1.NewPolicyServiceClient(conn)

	for i := 0; i < 20 && (lost.evaluates.Load() == 0 || live.evaluates.Load() == 0); i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err := client.Evaluate(ctx, &policyv1.EvaluateRequest{})
		cancel()
		require.NoError(t, err)
	}
	require.NotZero(t, lost.evaluates.Load(), "the call never reached the first backend")
	require.NotZero(t, live.evaluates.Load(), "the call never reached the second backend")
	return client, hole, lost, live
}

func testKey(t *testing.T) *signer.Key {
	t.Helper()
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	require.NoError(t, err)
	key, err := signer.ParseKey(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	require.NoError(t, err)
	return key
}

// detection is what keepalive promises for a lost peer, and margin is
// what the test allows beyond it for a scheduler and a retry's backoff.
const (
	detection = 13 * time.Second
	margin    = 5 * time.Second
)

func TestDial_ALostBackendIsLeftWithinTheKeepaliveWindow(t *testing.T) {
	t.Parallel()
	client, hole, lost, live := twoBackends(t, false)
	hole.dead.Store(true)
	lostBefore, liveBefore := lost.evaluates.Load(), live.evaluates.Load()

	// round_robin sends a call to each backend in turn, so within two
	// calls one is pinned to the blackholed one. It is retried once on
	// UNAVAILABLE, onto the live backend, so it answers and does so
	// within the window.
	var slowest time.Duration
	for i := 0; i < 4; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		start := time.Now()
		_, err := client.Evaluate(ctx, &policyv1.EvaluateRequest{})
		took := time.Since(start)
		cancel()
		require.NoError(t, err, "call %d", i)
		slowest = max(slowest, took)
	}
	assert.Greater(t, slowest, 5*time.Second,
		"no call waited on the blackholed backend, so nothing was measured")
	assert.Less(t, slowest, detection+margin,
		"a call pinned to the blackholed backend outlived keepalive's Time plus Timeout")
	assert.Equal(t, lostBefore, lost.evaluates.Load(), "the blackholed backend answered")
	assert.Equal(t, int64(4), live.evaluates.Load()-liveBefore, "every call ended on the live backend")

	// The next call goes to the live backend at once.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err := client.Evaluate(ctx, &policyv1.EvaluateRequest{})
	require.NoError(t, err)
	assert.Less(t, time.Since(start), 2*time.Second)
}

func TestDial_AWriteToALostBackendIsAttemptedOnce(t *testing.T) {
	t.Parallel()
	client, hole, lost, live := twoBackends(t, false)
	hole.dead.Store(true)
	lostBefore, liveBefore := lost.applies.Load(), live.applies.Load()

	var failed, answered int64
	var failure error
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		start := time.Now()
		_, err := client.ApplyEntityChanges(ctx, &policyv1.ApplyEntityChangesRequest{})
		took := time.Since(start)
		cancel()
		if err != nil {
			failed++
			failure = err
			assert.Less(t, took, detection+margin, "the write outlived keepalive's window")
			continue
		}
		answered++
	}
	require.Equal(t, int64(1), failed, "exactly the write sent to the blackholed backend fails")
	assert.Equal(t, codes.Unavailable, status.Code(failure))
	assert.Equal(t, lostBefore, lost.applies.Load(), "the blackholed backend applied a write")
	assert.Equal(t, answered, live.applies.Load()-liveBefore,
		"the live backend saw a copy of the write that failed: it was attempted twice")
}

func TestDial_WithoutKeepaliveACallToALostBackendHangsToItsDeadline(t *testing.T) {
	t.Parallel()
	// The control: with the keepalive left out, as before this change,
	// a call pinned to the blackholed backend is not failed by the
	// transport and runs to its deadline, so the tests above fail with
	// the keepalive reverted.
	client, hole, _, _ := twoBackends(t, true)
	hole.dead.Store(true)

	deadline := detection + margin + 2*time.Second
	var longest time.Duration
	var code codes.Code
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), deadline)
		start := time.Now()
		_, err := client.Evaluate(ctx, &policyv1.EvaluateRequest{})
		took := time.Since(start)
		cancel()
		if took > longest {
			longest, code = took, status.Code(err)
		}
	}
	assert.Equal(t, codes.DeadlineExceeded, code, "the pinned call was ended by something other than its deadline")
	assert.GreaterOrEqual(t, longest, deadline-time.Second, "the pinned call ended before its deadline")
}

func TestServiceConfig_RetriesOnlyTheReadOnlyDecisionCalls(t *testing.T) {
	cfg := serviceConfig()
	for _, method := range []string{`"method":"Evaluate"`, `"method":"Evaluations"`} {
		assert.Contains(t, cfg, method)
	}
	for _, method := range []string{"ApplyEntityChanges", "CreatePolicy", "DeletePolicy", "ListAuditRecords"} {
		assert.NotContains(t, cfg, method, "a call that is not a read-only decision is retried")
	}
	assert.Contains(t, cfg, `"maxAttempts":2`)
	assert.Contains(t, cfg, `"retryableStatusCodes":["UNAVAILABLE"]`)
	assert.Contains(t, cfg, `"round_robin"`)
}

func TestTarget_ResolvesThroughDNSUnlessAResolverIsNamed(t *testing.T) {
	assert.Equal(t, "dns:///garam-policy:8443", target("garam-policy:8443"))
	assert.Equal(t, "dns:///127.0.0.1:1", target("127.0.0.1:1"))
	assert.Equal(t, "garamtest:///policy", target("garamtest:///policy"))
	assert.Equal(t, "unix:///run/policy.sock", target("unix:///run/policy.sock"))
}
