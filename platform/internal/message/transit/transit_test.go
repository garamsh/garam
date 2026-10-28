package transit_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/message"
	"github.com/garamsh/garam/platform/internal/message/transit"
)

// The real Transit is what tests/integration signs against; these
// substitute it over TLS to hold what garam sends and how it reads
// each answer, which a real server cannot be made to answer wrongly.

const token = "s.test-token"

// fakeTransit answers rotate and sign for one ecdsa-p256 key, and
// records the token and body of every request.
type fakeTransit struct {
	keyType  string
	versions []*ecdsa.PrivateKey
	tokens   []string
	signed   []map[string]any
	status   int
}

func (f *fakeTransit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.tokens = append(f.tokens, r.Header.Get("X-Vault-Token"))
	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(`{"errors":["permission denied"]}`))
		return
	}
	switch r.URL.Path {
	case "/v1/transit/keys/garam/rotate":
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		f.versions = append(f.versions, key)
		keys := map[string]any{}
		for i, k := range f.versions {
			der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
			keys[strconv.Itoa(i+1)] = map[string]string{"public_key": string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))}
		}
		writeData(w, map[string]any{"type": f.keyType, "latest_version": len(f.versions), "keys": keys})
	case "/v1/transit/sign/garam":
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.signed = append(f.signed, req)
		input, _ := base64.StdEncoding.DecodeString(req["input"].(string))
		version := int(req["key_version"].(float64))
		digest := sha256.Sum256(input)
		rr, ss, _ := ecdsa.Sign(rand.Reader, f.versions[version-1], digest[:])
		raw := make([]byte, 64)
		rr.FillBytes(raw[:32])
		ss.FillBytes(raw[32:])
		writeData(w, map[string]any{"signature": "vault:v" + strconv.Itoa(version) + ":" + base64.RawURLEncoding.EncodeToString(raw)})
	default:
		http.NotFound(w, r)
	}
}

func writeData(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// serve starts f over TLS and answers a custodian for it, its token
// written owner-only as key material arrives.
func serve(t *testing.T, f http.Handler) (*transit.Custodian, string) {
	t.Helper()
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	tokenFile := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte(token+"\n"), 0o600))
	c, err := transit.New(transit.Settings{Address: srv.URL, CAFile: caFile, TokenFile: tokenFile, Key: "garam"})
	require.NoError(t, err)
	return c, tokenFile
}

// TestCustodian_SignsWithTheVersionItsKeyNames holds the round trip a
// hand-out takes: rotate makes a version whose public half comes from
// the same answer, and a signature under it names that version and
// verifies under that public half. The second version is the control:
// the first key still signs as the first.
func TestCustodian_SignsWithTheVersionItsKeyNames(t *testing.T) {
	f := &fakeTransit{keyType: "ecdsa-p256"}
	c, _ := serve(t, f)
	ctx := context.Background()

	first, err := c.Create(ctx)
	require.NoError(t, err)
	second, err := c.Create(ctx)
	require.NoError(t, err)
	assert.Equal(t, message.TransitVersion{Mount: "transit", Key: "garam", Version: 1}, first.Held)
	assert.Equal(t, message.TransitVersion{Mount: "transit", Key: "garam", Version: 2}, second.Held)

	signer, err := c.Open(ctx, &message.SigningKey{ID: "k-1", PublicKey: first.PublicKey, Held: first.Held})
	require.NoError(t, err)
	input := []byte("eyJhbGciOiJFUzI1NiJ9.eyJpc3MiOiJ4In0")
	raw, err := signer.Sign(ctx, input)
	require.NoError(t, err)

	require.Len(t, f.signed, 1)
	assert.Equal(t, map[string]any{
		"input":                base64.StdEncoding.EncodeToString(input),
		"key_version":          float64(1),
		"hash_algorithm":       "sha2-256",
		"marshaling_algorithm": "jws",
	}, f.signed[0])
	public, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), first.PublicKey)
	require.NoError(t, err)
	digest := sha256.Sum256(input)
	assert.True(t, ecdsa.Verify(public, digest[:], new(big.Int).SetBytes(raw[:32]), new(big.Int).SetBytes(raw[32:])),
		"the signature verifies under the public half rotate answered")
	assert.Equal(t, []string{token, token, token}, f.tokens, "every call presents the token, trimmed")
}

// TestCustodian_RefusesAKeyOfAnotherType holds the one key type a
// message is signed under: an aes key rotates as readily, and is
// refused rather than stored.
func TestCustodian_RefusesAKeyOfAnotherType(t *testing.T) {
	c, _ := serve(t, &fakeTransit{keyType: "aes256-gcm96"})
	_, err := c.Create(context.Background())
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition), err.Error())

	ok, _ := serve(t, &fakeTransit{keyType: "ecdsa-p256"})
	_, err = ok.Create(context.Background())
	assert.NoError(t, err, "the control: an ecdsa-p256 key is taken")
}

// TestCustodian_HoldsOnlyVersionsOfItsOwnKey holds which rows this
// custodian signs: a version of its key and mount, and neither a
// sealed key nor a version of another key.
func TestCustodian_HoldsOnlyVersionsOfItsOwnKey(t *testing.T) {
	c, _ := serve(t, &fakeTransit{keyType: "ecdsa-p256"})
	assert.True(t, c.Holds(message.TransitVersion{Mount: "transit", Key: "garam", Version: 3}))
	assert.False(t, c.Holds(message.TransitVersion{Mount: "transit", Key: "other", Version: 3}))
	assert.False(t, c.Holds(message.TransitVersion{Mount: "kms", Key: "garam", Version: 3}))
	assert.False(t, c.Holds(message.SealedKey("sealed")))
}

// TestCustodian_UnreachableIsUnavailableAndRefusalIsNot holds the kinds
// a claim and a rotation read (ADR-0104): a Transit nobody answers, a
// token not yet written and a 5xx are unavailable, which a rotation
// defers; a 403 is garam's misconfiguration. No error carries the
// token.
func TestCustodian_UnreachableIsUnavailableAndRefusalIsNot(t *testing.T) {
	ctx := context.Background()

	denied, _ := serve(t, &fakeTransit{status: http.StatusForbidden})
	_, err := denied.Create(ctx)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInternal), err.Error())
	assert.Contains(t, err.Error(), "permission denied")

	failing, _ := serve(t, &fakeTransit{status: http.StatusServiceUnavailable})
	_, err = failing.Create(ctx)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), err.Error())

	untokened, tokenFile := serve(t, &fakeTransit{keyType: "ecdsa-p256"})
	require.NoError(t, os.Remove(tokenFile))
	_, err = untokened.Create(ctx)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), err.Error())

	srv := httptest.NewTLSServer(http.NotFoundHandler())
	dir := t.TempDir()
	caFile := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	tokenFile = filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte(token), 0o600))
	address := srv.URL
	srv.Close()
	unreachable, err := transit.New(transit.Settings{Address: address, CAFile: caFile, TokenFile: tokenFile, Key: "garam"})
	require.NoError(t, err)
	_, err = unreachable.Create(ctx)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), err.Error())

	assert.NotContains(t, err.Error(), token)
}

// TestCustodian_DoesNotFollowARedirect holds the token to the address
// the deployment named: a redirect elsewhere is answered as a refusal
// and the token does not travel to it.
func TestCustodian_DoesNotFollowARedirect(t *testing.T) {
	var elsewhere []string
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere = append(elsewhere, r.Header.Get("X-Vault-Token"))
	}))
	t.Cleanup(target.Close)
	c, _ := serve(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))

	_, err := c.Create(context.Background())
	require.Error(t, err)
	assert.Empty(t, elsewhere, "the token reached the redirect's target")
}
