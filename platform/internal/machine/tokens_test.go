package machine_test

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/lib/errorx"
	"github.com/garamsh/garam/lib/grn"
	"github.com/garamsh/garam/platform/internal/machine"
)

// tokens is the Tokens seam in memory: the GRN each live token names,
// and how many times the listener asked.
type tokens struct {
	mu    sync.Mutex
	live  map[string]grn.GRN
	asked int
}

func newTokens() *tokens { return &tokens{live: map[string]grn.GRN{}} }

func (k *tokens) Resolve(_ context.Context, token string) (machine.Bearer, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.asked++
	subject, ok := k.live[token]
	if !ok {
		return machine.Bearer{}, errorx.New(errorx.KindUnauthenticated, "token not live")
	}
	return machine.Bearer{Subject: subject, Serial: "t" + token, IssuedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour)}, nil
}

func (k *tokens) set(token string, subject grn.GRN) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.live[token] = subject
}

func (k *tokens) withdraw(token string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.live, token)
}

// methodHandler answers with the GRN the request's token proved, and
// with the refusal a request carrying none was given.
func methodHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := machine.IdentityOf(r)
		if !ok {
			reason := "no identity"
			if err := machine.RefusalOf(r); err != nil {
				reason = err.Error()
			}
			http.Error(w, reason, http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, identity.GRN().String())
	})
}

// presenting performs one request over client carrying the token, and
// returns the status and body with the body drained, so the connection
// stays open for the next.
func presenting(t *testing.T, client *http.Client, url, token string) (int, string, *tls.ConnectionState) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url+"/identity", nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", machine.TokenScheme+" "+token)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body), resp.TLS
}

// TestNewServer_ReadsATokenOnEveryRequestOverAnOpenConnection is
// ADR-0100's revocation at the listener: a token is read for each
// request that presents it, so withdrawing it refuses the next request
// on the connection that was already open. Both requests travel over
// one connection — the second reports that it did not resume or redial
// — so what refuses it is the per-request read and not a new
// handshake.
func TestNewServer_ReadsATokenOnEveryRequestOverAnOpenConnection(t *testing.T) {
	known := newTokens()
	subject := operatorGRNOf("acme", "one")
	known.set("garam_ot_live", subject)
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	url := serveTokens(t, dir, known, methodHandler())

	var connections int
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    roots,
			MinVersion: tls.VersionTLS13,
			VerifyConnection: func(tls.ConnectionState) error {
				connections++
				return nil
			},
		},
		ForceAttemptHTTP2: true,
	}}
	t.Cleanup(client.CloseIdleConnections)

	status, body, _ := presenting(t, client, url, "garam_ot_live")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, subject.String(), body)

	known.withdraw("garam_ot_live")
	status, body, _ = presenting(t, client, url, "garam_ot_live")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, "token not live", "the refusal carries the reason the read gave")
	assert.Equal(t, 1, connections, "both requests travelled over the one connection")
	assert.Equal(t, 2, known.asked, "each request that presented the token was read")
}

// TestNewServer_ReadsAnotherAuthorizationSchemeAsNoToken holds that
// only the token scheme is read as a token: a request carrying another
// scheme in Authorization carries no identity, and nothing is read for
// it. The token on the same connection is the control.
func TestNewServer_ReadsAnotherAuthorizationSchemeAsNoToken(t *testing.T) {
	known := newTokens()
	subject := operatorGRNOf("acme", "one")
	known.set("garam_ot_live", subject)
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	url := serveTokens(t, dir, known, methodHandler())
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: roots, MinVersion: tls.VersionTLS13,
	}}}
	t.Cleanup(client.CloseIdleConnections)

	status, body := presentingAuthorization(t, client, url, "Garam-Token garam_ot_live")
	require.Equal(t, http.StatusOK, status, body)
	asked := known.asked

	status, body = presentingAuthorization(t, client, url, "Garam-Execution an-execution-token")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, "no identity")
	assert.Equal(t, asked, known.asked, "another scheme is not read as a token")
}

// presentingAuthorization performs one request over client carrying
// each value as an Authorization header of its own, and returns the
// status and body.
func presentingAuthorization(t *testing.T, client *http.Client, url string, values ...string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url+"/identity", nil)
	require.NoError(t, err)
	for _, v := range values {
		req.Header.Add("Authorization", v)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(body)
}

// TestNewServer_ReadsABearerTokenAsItReadsAGaramToken is ADR-0102's
// alias at the listener: the same token under `Bearer`, in any case,
// resolves through the same read to the same identity, and withdrawing
// it refuses the next request on the open connection. Garam-Token on
// the same token is the control the alias is measured against.
func TestNewServer_ReadsABearerTokenAsItReadsAGaramToken(t *testing.T) {
	known := newTokens()
	subject := operatorGRNOf("acme", "one")
	known.set("garam_ot_live", subject)
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	url := serveTokens(t, dir, known, methodHandler())
	var connections int
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: roots, MinVersion: tls.VersionTLS13,
		VerifyConnection: func(tls.ConnectionState) error { connections++; return nil },
	}}}
	t.Cleanup(client.CloseIdleConnections)

	for _, value := range []string{"Garam-Token garam_ot_live", "Bearer garam_ot_live", "bearer garam_ot_live", "BEARER garam_ot_live"} {
		status, body := presentingAuthorization(t, client, url, value)
		require.Equal(t, http.StatusOK, status, value)
		assert.Equal(t, subject.String(), body, value)
	}

	known.withdraw("garam_ot_live")
	status, body := presentingAuthorization(t, client, url, "Bearer garam_ot_live")
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Contains(t, body, "token not live")
	assert.Equal(t, 1, connections, "every request travelled over the one connection")
	assert.Equal(t, 5, known.asked, "each request presenting the token was read, under either scheme")
}

// TestNewServer_RefusesAMalformedBearer holds that the alias proves
// nothing without a token after it (ADR-0102): a Bearer value with no
// token leaves the request with no identity, where the same scheme
// with the live token is the control.
func TestNewServer_RefusesAMalformedBearer(t *testing.T) {
	known := newTokens()
	subject := operatorGRNOf("acme", "one")
	known.set("garam_ot_live", subject)
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	url := serveTokens(t, dir, known, methodHandler())
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: roots, MinVersion: tls.VersionTLS13,
	}}}
	t.Cleanup(client.CloseIdleConnections)

	status, body := presentingAuthorization(t, client, url, "Bearer garam_ot_live")
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, subject.String(), body, "the control: the live token authenticates")

	for _, value := range []string{"Bearer", "Bearer ", "bearer"} {
		status, _ := presentingAuthorization(t, client, url, value)
		assert.Equal(t, http.StatusUnauthorized, status, value)
	}
}

// TestNewServer_RefusesATokenAmongSeveralAuthorizationHeaders holds
// that combined credentials carry no identity (ADR-0102): a token in
// either of two Authorization headers is refused before it is read,
// where the same token alone authenticates.
func TestNewServer_RefusesATokenAmongSeveralAuthorizationHeaders(t *testing.T) {
	known := newTokens()
	subject := operatorGRNOf("acme", "one")
	known.set("garam_ot_live", subject)
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	url := serveTokens(t, dir, known, methodHandler())
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs: roots, MinVersion: tls.VersionTLS13,
	}}}
	t.Cleanup(client.CloseIdleConnections)

	status, body := presentingAuthorization(t, client, url, "Bearer garam_ot_live")
	require.Equal(t, http.StatusOK, status, body)
	asked := known.asked

	for _, values := range [][]string{
		{"Bearer garam_ot_live", "Garam-Token garam_ot_live"},
		{"Garam-Execution an-execution-token", "Bearer garam_ot_live"},
		{"Garam-Token garam_ot_live", "Garam-Credential garam_oc_x"},
	} {
		status, body := presentingAuthorization(t, client, url, values...)
		assert.Equal(t, http.StatusUnauthorized, status, values)
		assert.Contains(t, body, "one Authorization header", values)
	}
	assert.Equal(t, asked, known.asked, "no token among several was read")
}

// stalledTokens resolves its one token after release closes, as a store
// whose read waits on a lock does, and gives up only when the context
// it was handed ends.
type stalledTokens struct {
	release chan struct{}
}

func (s stalledTokens) Resolve(ctx context.Context, _ string) (machine.Bearer, error) {
	select {
	case <-s.release:
		return machine.Bearer{Subject: liveSubject, Serial: "t1", IssuedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Hour)}, nil
	case <-ctx.Done():
		return machine.Bearer{}, ctx.Err()
	}
}

// refusalKind answers the kind of the refusal a request was handed, or
// the GRN its token proved.
func refusalKind() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if identity, ok := machine.IdentityOf(r); ok {
			_, _ = io.WriteString(w, identity.GRN().String())
			return
		}
		_, _ = io.WriteString(w, errorx.KindOf(machine.RefusalOf(r)).String())
	})
}

// TestNewServer_BoundsTokenResolutionByItsDeadline holds the read that
// resolves a token to ResolveDeadline (#1613): a resolver that never
// answers hands the request an unavailable refusal at the deadline,
// whatever error the ended read returned, rather than holding it until
// the store lets go. The same resolver released before the deadline
// is the control: it admits the token.
func TestNewServer_BoundsTokenResolutionByItsDeadline(t *testing.T) {
	stalled := stalledTokens{release: make(chan struct{})}
	dir := t.TempDir()
	roots := serverPair(t, dir, presented)
	url := serveTokens(t, dir, stalled, refusalKind())
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13},
	}}
	t.Cleanup(client.CloseIdleConnections)

	start := time.Now()
	status, body, _ := presenting(t, client, url, liveToken)
	elapsed := time.Since(start)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, errorx.KindUnavailable.String(), body, "a read the deadline ended is unavailable")
	assert.GreaterOrEqual(t, elapsed, machine.ResolveDeadline)
	assert.Less(t, elapsed, machine.ResolveDeadline+5*time.Second, "answered at the deadline")

	close(stalled.release)
	status, body, _ = presenting(t, client, url, liveToken)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, liveSubject.String(), body, "the control: a read that answers admits the token")
}
