package e2e_test

import (
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// maxPortAttempts bounds how often reserve asks the kernel again for a
// port it already handed out. A kernel answering a repeat that often is
// out of ports, not unlucky.
const maxPortAttempts = 100

// portReserver hands out loopback addresses for a process to bind, none
// twice in one test binary. A port is free again between Close here and
// Listen in the process, and the kernel may answer the next :0 with
// that same port: two listeners in one test were given one port (#1383).
// Remembering what was handed out closes that; the window against an
// unrelated process stays, which is the best a test can do without
// handing the process an open socket.
type portReserver struct {
	mu     sync.Mutex
	handed map[string]bool
	listen func() (net.Listener, error)
}

func newPortReserver(listen func() (net.Listener, error)) *portReserver {
	return &portReserver{handed: map[string]bool{}, listen: listen}
}

// reserve answers an address no earlier call returned.
func (r *portReserver) reserve() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for range maxPortAttempts {
		lis, err := r.listen()
		if err != nil {
			return "", err
		}
		addr := lis.Addr().String()
		_ = lis.Close()
		if !r.handed[addr] {
			r.handed[addr] = true
			return addr, nil
		}
	}
	return "", errors.New("the kernel kept answering ports already handed out")
}

var loopbackPorts = newPortReserver(func() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
})

// freeAddr reserves a loopback address for the process to bind.
func freeAddr(t *testing.T) string {
	t.Helper()
	addr, err := loopbackPorts.reserve()
	require.NoError(t, err)
	return addr
}

// fixedListener is a listener the kernel did not open, at addr.
type fixedListener struct{ addr net.Addr }

func (l fixedListener) Accept() (net.Conn, error) { return nil, errors.New("not listening") }
func (l fixedListener) Close() error              { return nil }
func (l fixedListener) Addr() net.Addr            { return l.addr }

// kernelAnswering replays ports as the kernel's answers to :0, in order.
func kernelAnswering(ports ...int) func() (net.Listener, error) {
	return func() (net.Listener, error) {
		if len(ports) == 0 {
			return nil, errors.New("no more answers")
		}
		port := ports[0]
		ports = ports[1:]
		return fixedListener{&net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}}, nil
	}
}

// TestPortReserver_NeverHandsOutAPortTwice replays the sequence #1383
// met: the kernel answers the second :0 with the port it gave the
// first, and the reserver asks again instead of handing it out. The
// first answer, a port nothing held, is the control: it is handed out
// as the kernel gave it.
func TestPortReserver_NeverHandsOutAPortTwice(t *testing.T) {
	r := newPortReserver(kernelAnswering(45309, 45309, 45310))

	first, err := r.reserve()
	require.NoError(t, err)
	second, err := r.reserve()
	require.NoError(t, err)

	assert.Equal(t, "127.0.0.1:45309", first)
	assert.Equal(t, "127.0.0.1:45310", second, "the repeat is asked again, not handed out")
}

// TestPortReserver_GivesUpOnAKernelThatOnlyRepeats bounds the asking: a
// kernel that answers nothing but handed-out ports fails the
// reservation rather than spinning.
func TestPortReserver_GivesUpOnAKernelThatOnlyRepeats(t *testing.T) {
	repeats := make([]int, maxPortAttempts+1)
	for i := range repeats {
		repeats[i] = 45309
	}
	r := newPortReserver(kernelAnswering(repeats...))

	_, err := r.reserve()
	require.NoError(t, err)
	_, err = r.reserve()
	assert.Error(t, err)
}
