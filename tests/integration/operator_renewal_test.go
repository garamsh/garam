package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
)

// keyEncryptionKey writes the key every certificate authority is sealed
// under the way ADR-0015 has key material arrive — a file its owner
// alone can read — and returns the path beside the sealer that opens
// it. The command under test reads the file; the listener beside it
// takes the sealer.
func keyEncryptionKey(t *testing.T) (string, ca.Sealer) {
	t.Helper()
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "ca.key")
	require.NoError(t, os.WriteFile(path, []byte(hex.EncodeToString(key)), 0o600))
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	return path, sealer
}

// aged issues certificates as garam would have issued them minutes ago:
// one lifetime long, signed at issuedAt. A certificate garam signs now
// is outside its renewal window by construction, so a test that renews
// one has to hold a certificate that has already run most of its
// course.
func aged(t *testing.T, p *pgxpool.Pool, sealer ca.Sealer, lifetime time.Duration, issuedAt time.Time) ca.Service {
	t.Helper()
	issuer, err := ca.NewService(capg.New(p), sealer, ca.Config{
		CertificateLifetime: lifetime,
		Now:                 func() time.Time { return issuedAt },
	})
	require.NoError(t, err)
	return issuer
}

// serialOf returns the serial the certificate carries, which is what
// the record naming an operator's current certificate holds.
func serialOf(t *testing.T, pair tls.Certificate) string {
	t.Helper()
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	require.NoError(t, err)
	return leaf.SerialNumber.String()
}

// currentSerial reads the certificate garam records as the operator's
// current one. The rule is enforced in the statement that writes it, so
// the row is what says the write happened.
func currentSerial(t *testing.T, p *pgxpool.Pool, subject string) string {
	t.Helper()
	var serial string
	require.NoError(t, p.QueryRow(context.Background(),
		`SELECT serial FROM operator_certificates WHERE operator_grn = $1`, subject).Scan(&serial))
	return serial
}

// renew asks for the successor to the certificate client presents.
func renew(t *testing.T, client *http.Client, url string) (int, []byte) {
	t.Helper()
	return machineRequest(t, client, http.MethodPost, url+"/identity/certificate")
}

// TestOperatorRenewal_ReplacesTheCertificateThePresenterHolds carries
// ADR-0046 end to end against real Postgres and a real handshake: an
// operator inside its renewal window presents the certificate it holds
// and receives the next one, the record moves to it, and the
// certificate it replaced renews nothing afterwards.
//
// The refused calls each sit beside one that is admitted, because a
// route that refused every renewal would answer all three the same way.
func TestOperatorRenewal_ReplacesTheCertificateThePresenterHolds(t *testing.T) {
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(context.Background(), org.NewOrganization(orgRootGRN("acme"), "acme")))
	keyFile, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)

	subject := scopedGRN("acme", operator.ServiceType, "one")
	// Three quarters through an hour-long certificate, so what is left
	// is inside the last third the rule admits.
	held := installed(t, p, aged(t, p, sealer, time.Hour, time.Now().Add(-45*time.Minute)), subject)
	client := machineClient(t, held)

	status, body := renew(t, client, url)
	require.Equal(t, http.StatusCreated, status, string(body))
	var issued machinegen.MachineCertificate
	require.NoError(t, json.Unmarshal(body, &issued))
	assert.Equal(t, subject.String(), issued.Grn)

	// The successor authenticates, which is the whole point of asking
	// for it, and the record now names it.
	successor := pairOf(t, issued)
	status, body = machineRequest(t, machineClient(t, successor), http.MethodGet, url+"/identity")
	require.Equal(t, http.StatusOK, status, string(body))
	assert.JSONEq(t, `{"grn":"`+subject.String()+`"}`, string(body))
	assert.Equal(t, serialOf(t, successor), currentSerial(t, p, subject.String()))

	// The certificate that was replaced still authenticates — nothing
	// here revokes one — and it renews nothing, which is what keeps a
	// copy of it from being a credential without end.
	status, body = machineRequest(t, client, http.MethodGet, url+"/identity")
	require.Equal(t, http.StatusOK, status, string(body))
	status, body = renew(t, client, url)
	assert.Equal(t, http.StatusConflict, status)
	assert.Contains(t, string(body), `"kind":"already_exists"`)
	assert.Equal(t, serialOf(t, successor), currentSerial(t, p, subject.String()),
		"a refused renewal moves nothing")

	// A certificate with its whole life ahead of it is refused as too
	// early, under a kind that says to ask again. It names an operator
	// with no record at all, so what refuses it is the window.
	fresh := machineClient(t, installed(t, p, aged(t, p, sealer, time.Hour, time.Now()), scopedGRN("acme", operator.ServiceType, "two")))
	status, body = renew(t, fresh, url)
	assert.Equal(t, http.StatusConflict, status)
	assert.Contains(t, string(body), `"kind":"failed_precondition"`)

	// Minting out of band again takes the lineage back: the command
	// ADR-0032 ships records what it mints, so the certificate that held
	// the lineage a moment ago no longer does. That is the recovery from
	// a renewal answer nobody received, and the closest thing to a
	// revocation garam has.
	outDir, err := runIssueCertificate(t, keyFile, subject.String())
	require.NoError(t, err)
	minted, err := tls.LoadX509KeyPair(filepath.Join(outDir, "certificate.pem"), filepath.Join(outDir, "key.pem"))
	require.NoError(t, err)
	assert.Equal(t, serialOf(t, minted), currentSerial(t, p, subject.String()))
	assert.NotEqual(t, serialOf(t, successor), serialOf(t, minted))
}
