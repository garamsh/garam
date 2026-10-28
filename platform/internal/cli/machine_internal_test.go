package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/ca"
	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/health"
	"github.com/garamsh/garam/platform/internal/machineapi"
)

func machineGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, "agent", id)
}

func orgGRN(org string) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "org", org)
}

// writeKeyEncryptionKey writes a key-encryption key the way ADR-0015
// has one reach a process: a file its owner alone can read.
func writeKeyEncryptionKey(t *testing.T, dir string) string {
	t.Helper()
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	path := filepath.Join(dir, "ca.key")
	require.NoError(t, os.WriteFile(path, []byte(hex.EncodeToString(key)), 0o600))
	return path
}

// selfSigned returns a certificate and its key in PEM form. What either
// carries is not what the assemblies below check, so one shape answers
// wherever they need a certificate.
func selfSigned(t *testing.T, commonName string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

// writeServerRoot writes the garam server root a machine identity pins
// garam by (ADR-0040). It is a certificate of its own rather than the
// listener's, because the two are what this change separates.
//
// Owner-only like the key material beside it, though the reader of this
// file reads its contents and not its mode: the umask subtracts from
// os.WriteFile's mode argument, so a mode reaching past the owner is one
// the environment decides rather than this helper.
func writeServerRoot(t *testing.T, dir string) string {
	t.Helper()
	certPEM, _ := selfSigned(t, "garam server root")
	path := filepath.Join(dir, "server-root.pem")
	require.NoError(t, os.WriteFile(path, certPEM, 0o600))
	return path
}

// writeTLSKeyPair writes the certificate the listener terminates TLS
// with, as key material arrives: the key readable by its owner alone.
func writeTLSKeyPair(t *testing.T, dir string) (string, string) {
	t.Helper()
	certPEM, keyPEM := selfSigned(t, "garam machine listener")
	certFile := filepath.Join(dir, "tls.crt")
	keyFile := filepath.Join(dir, "tls.key")
	require.NoError(t, os.WriteFile(certFile, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyFile, keyPEM, 0o600))
	return certFile, keyFile
}

// TestMachineServer_AnswersAPanicOutsideTheSurfacesOwnMiddleware is
// `serve machine`'s wiring. The generated middleware slice reaches the
// spec operations alone, so a panic raised outside them — the identity
// check machine.NewServer puts around the handler it is given, or a
// handler reached any other way — meets the recovery machineServer puts
// outermost or none at all.
//
// Without it the connection is dropped rather than answered and the Get
// below fails outright. The plain-text body is what says this recovery
// answered: the surface's own writes the specification's Error as JSON.
// The line names no route, for the reason apiHandler's test gives.
func TestMachineServer_AnswersAPanicOutsideTheSurfacesOwnMiddleware(t *testing.T) {
	// Internal: NewMachineServer takes no handler that could panic.
	subject := machineGRN("acme", "bot-1")

	var served lines
	answering, client := machineListenerAround(t, recordingObservability(&served), subject,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	resp, err := client.Get(answering + "/identity")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	// The control: the same listener carries a request to a handler that
	// does not panic, so what the 500 below reports is the panic rather
	// than the token or the identity check refusing.
	require.Equal(t, http.StatusNoContent, resp.StatusCode)

	var out lines
	url, client := machineListenerAround(t, recordingObservability(&out), subject,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("machine listener defect") }))

	resp, err = client.Get(url + "/identity")
	require.NoError(t, err, "the client read no answer at all")
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, "internal error\n", string(body), "no internals reach the body")

	line := decodeLine(t, &out)
	assert.Equal(t, "http panic", line["msg"])
	assert.Equal(t, float64(http.StatusInternalServerError), line["status"])
	assert.Contains(t, line["error"], "panic: machine listener defect",
		"the line is the only record this panic leaves")
}

// TestMachineProbeHandler_AnswersAPanicOnTheListenerBesideTheTLSOne is
// `serve machine`'s wiring for the port a probe reaches without a
// certificate. Nothing on that port is a spec operation, so the
// recovery this assembly puts outermost is the only one a panic here
// meets.
func TestMachineProbeHandler_AnswersAPanicOnTheListenerBesideTheTLSOne(t *testing.T) {
	// Internal: serve machine mounts no probe a test can make panic.
	var served lines
	status, _ := readiness(t, recordingObservability(&served).machineProbeHandler(presentProbe{}))
	require.Equal(t, http.StatusOK, status, "the probe this handler mounts is reachable")

	var out lines
	status, body := readiness(t, recordingObservability(&out).machineProbeHandler(panickingProbe{}))

	require.Equal(t, http.StatusInternalServerError, status)
	assert.Equal(t, "internal error\n", body, "no internals reach the body")

	line := decodeLine(t, &out)
	assert.Equal(t, "http panic", line["msg"])
	assert.Equal(t, "GET "+health.ReadyPath, line["operation"])
	assert.Contains(t, line["error"], "panic: readiness defect",
		"the line is the only record this panic leaves")
}

// submission is a message post carrying a body of n bytes.
func submission(recipient grn.GRN, n int) string {
	return `{"recipient":"` + recipient.String() + `","idempotencyKey":"key-1","body":"` +
		strings.Repeat("x", n) + `"}`
}

// postSubmission sends one message post and returns the status and the
// body that came back.
func postSubmission(t *testing.T, client *http.Client, url, body string) (int, string) {
	t.Helper()
	resp, err := client.Post(url+"/messages", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(got)
}

// TestMachineServer_RefusesARequestBodyPastTheBound holds the machine
// listener's bound on what one request may weigh. The submission schema
// bounds a body at 65536 characters (ADR-0034) and nothing enforces
// that before the whole string is in memory, so the refusal here is the
// listener's rather than the schema's.
//
// The control is the same submission inside the bound, which the engine
// refuses: without it a refusal would hold as readily for a listener
// that admits no post at all.
func TestMachineServer_RefusesARequestBodyPastTheBound(t *testing.T) {
	// Internal: NewMachineServer authenticates against Postgres first.
	sender := machineGRN("acme", "bot-1")
	recipient := machineGRN("acme", "bot-2")
	url, client := machineListenerServing(t, recordingObservability(&lines{}), sender,
		machineapi.NewServer(nil, nil, refusalRecord{}, nil, nil, noGroups{}, deniedEngine{}, nil, nil, nil, nil, nil, nil, nil))

	status, body := postSubmission(t, client, url, submission(recipient, 1024))
	require.Equal(t, http.StatusForbidden, status, "the control: refused for whom it names")
	require.JSONEq(t, `{"kind":"permission_denied","message":"sender may not send to this recipient"}`, body)

	status, body = postSubmission(t, client, url, submission(recipient, maxRequestBody))

	assert.Equal(t, http.StatusBadRequest, status, "past the bound the body is not read at all")
	assert.JSONEq(t, `{"kind":"invalid_argument","message":"the request is not in the required form"}`, body)
}

// TestTransitCustodian_TakesTheProviderWholeOrNotAtAll holds ADR-0104's
// configuration rule: nothing set keeps every key sealed under the
// key-encryption key, everything set turns the provider on, and any
// part set alone refuses the start rather than running on a provider
// the deployment believes is there. The complete setting is the
// control each partial one is read against.
func TestTransitCustodian_TakesTheProviderWholeOrNotAtAll(t *testing.T) {
	// Internal: which custodian a start chose is not observable.
	dir := t.TempDir()
	root := writeServerRoot(t, dir)
	complete := map[envName]string{
		envTransitAddr:       "https://openbao.example:8200",
		envTransitCAFile:     root,
		envTransitTokenFile:  filepath.Join(dir, "token"),
		envTransitSigningKey: "garam-message-signing",
	}
	set := func(t *testing.T, values map[envName]string) {
		t.Helper()
		for _, name := range []envName{envTransitAddr, envTransitCAFile, envTransitTokenFile, envTransitSigningKey, envTransitMount} {
			t.Setenv(string(name), values[name])
		}
	}

	set(t, nil)
	custodian, err := transitCustodian()
	require.NoError(t, err, "nothing set")
	assert.Nil(t, custodian, "nothing set keeps the keys sealed under the key-encryption key")

	set(t, complete)
	custodian, err = transitCustodian()
	require.NoError(t, err, "everything set")
	assert.NotNil(t, custodian, "everything set turns the provider on")

	without := func(name envName) map[envName]string {
		values := map[envName]string{}
		for k, v := range complete {
			if k != name {
				values[k] = v
			}
		}
		return values
	}
	refused := map[string]map[envName]string{
		"no address":          without(envTransitAddr),
		"no CA file":          without(envTransitCAFile),
		"no token file":       without(envTransitTokenFile),
		"no key":              without(envTransitSigningKey),
		"a mount alone":       {envTransitMount: "transit"},
		"a plain http URL":    {envTransitAddr: "http://openbao.example:8200", envTransitCAFile: root, envTransitTokenFile: complete[envTransitTokenFile], envTransitSigningKey: "k"},
		"a CA file not there": {envTransitAddr: complete[envTransitAddr], envTransitCAFile: filepath.Join(dir, "token"), envTransitTokenFile: complete[envTransitTokenFile], envTransitSigningKey: "k"},
	}
	for name, values := range refused {
		t.Run(name, func(t *testing.T) {
			set(t, values)
			_, err := transitCustodian()
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), err.Error())
		})
	}
}
