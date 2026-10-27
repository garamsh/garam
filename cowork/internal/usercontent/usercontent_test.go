package usercontent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/cowork/internal/blob"
	"github.com/garamsh/garam/cowork/internal/usercontent"
)

type versions map[string]string

func (v versions) Served(_ context.Context, org, id string, number int) (blob.Blob, error) {
	body, ok := v[org+"/"+id]
	if !ok || number != 1 {
		return blob.Blob{}, blob.ErrNotFound
	}
	return blob.Blob{ContentType: "text/html; charset=utf-8", Body: []byte(body)}, nil
}

var secret = []byte(strings.Repeat("s", 32))

func serve(t *testing.T, signer *usercontent.Signer, url string) *http.Response {
	t.Helper()
	srv := httptest.NewServer(signer.Handler(versions{"acme/a1": "<p>hi</p>"}, []string{"localhost:3100"}))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + url[strings.Index(url, "/a/"):])
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestHandler_ServesASignedVersionUnderAPolicyThatReachesNothing.
func TestHandler_ServesASignedVersionUnderAPolicyThatReachesNothing(t *testing.T) {
	signer, err := usercontent.NewSigner("http://localhost:8092", secret)
	require.NoError(t, err)
	resp := serve(t, signer, signer.URL("acme", "a1", 1))
	require.Equal(t, http.StatusOK, resp.StatusCode)
	csp := resp.Header.Get("Content-Security-Policy")
	for _, d := range []string{"default-src 'none'", "connect-src 'none'", "form-action 'none'", "sandbox allow-scripts", "frame-ancestors localhost:3100"} {
		assert.Contains(t, csp, d)
	}
	assert.NotContains(t, csp, "allow-same-origin")
	assert.Equal(t, "nosniff", resp.Header.Get("X-Content-Type-Options"))
}

// TestHandler_RefusesAnAddressItDidNotSign: one signed for another
// version, one altered, one under another secret.
func TestHandler_RefusesAnAddressItDidNotSign(t *testing.T) {
	signer, err := usercontent.NewSigner("http://localhost:8092", secret)
	require.NoError(t, err)
	other, err := usercontent.NewSigner("http://localhost:8092", []byte(strings.Repeat("x", 32)))
	require.NoError(t, err)

	good := signer.URL("acme", "a1", 1)
	for name, url := range map[string]string{
		"another version": strings.Replace(good, "/a1/1?", "/a1/2?", 1),
		"another secret":  other.URL("acme", "a1", 1),
		"no signature":    good[:strings.Index(good, "?")],
		"altered":         strings.Replace(good, "sig=", "sig=x", 1),
	} {
		assert.Equal(t, http.StatusForbidden, serve(t, signer, url).StatusCode, name)
	}
}

// TestHandler_RefusesALapsedAddress: an address works for its
// lifetime and not after.
func TestHandler_RefusesALapsedAddress(t *testing.T) {
	now := time.Now()
	signer, err := usercontent.NewSigner("http://localhost:8092", secret)
	require.NoError(t, err)
	signer = signer.WithClock(func() time.Time { return now })
	url := signer.URL("acme", "a1", 1)
	now = now.Add(9 * time.Minute)
	assert.Equal(t, http.StatusOK, serve(t, signer, url).StatusCode, "within its lifetime")
	now = now.Add(2 * time.Minute)
	assert.Equal(t, http.StatusForbidden, serve(t, signer, url).StatusCode, "lapsed")
}

// TestNewSigner_RefusesAShortSecretAndAnOriginWithAPath.
func TestNewSigner_RefusesAShortSecretAndAnOriginWithAPath(t *testing.T) {
	_, err := usercontent.NewSigner("http://localhost:8092", []byte("short"))
	assert.Error(t, err)
	_, err = usercontent.NewSigner("http://localhost:8092/x", secret)
	assert.Error(t, err)
}
