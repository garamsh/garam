package integration_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/cli"
	"github.com/garamsh/garam/internal/org"
)

// runIssueCertificate runs the shipped command against the test
// database with the key-encryption key at keyFile, writing the
// credential into a directory of its own.
func runIssueCertificate(t *testing.T, keyFile, subjectGRN string) (string, error) {
	t.Helper()
	t.Setenv("GARAM_DATABASE_URL", os.Getenv("GARAM_TEST_DATABASE_URL"))
	t.Setenv("GARAM_CA_KEY_FILE", keyFile)
	outDir := t.TempDir()
	root := cli.NewRootCommand()
	root.SetArgs([]string{"issue-certificate", subjectGRN, "--out-dir", outDir})
	return outDir, root.Execute()
}

// TestIssueCertificate_MintsAnOperatorsFirstCertificate carries the one
// bootstrap ADR-0015 leaves out of band through what the repository
// ships: the command mints an operator's certificate against real
// Postgres, and that certificate authenticates on the machine listener
// once the organization has registered the operator — the pair the
// suite's `installed` helper stands in for everywhere else.
//
// **The mint alone is no longer enough, and this is where that is
// stated.** The command reads no record and writes none, so what it
// produces is a certificate for an operator the organization may never
// have registered, and the listener refuses that (ADR-0070). The
// registered call afterwards is the control: the same certificate on
// the same listener, so what admits it is the record rather than
// anything the mint did differently. This is the state a deployment
// that bootstrapped out of band and skipped the registration is in,
// and registering is the whole repair.
//
// The refused agent subject sits beside the minted operator, so what
// refuses it is the operator check rather than a mint that
// authenticates nobody.
func TestIssueCertificate_MintsAnOperatorsFirstCertificate(t *testing.T) {
	p := pool(t)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(context.Background(), org.NewOrganization(orgRootGRN("acme"), "acme")))

	// The key-encryption key reaches the command the way ADR-0015 has
	// key material reach a process: a file its owner alone can read.
	keyFile, sealer := keyEncryptionKey(t)

	_, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)

	operatorGRN := scopedGRN("acme", agent.ServiceTypeOperator, "one")
	outDir, err := runIssueCertificate(t, keyFile, operatorGRN.String())
	require.NoError(t, err)

	certPEM, err := os.ReadFile(filepath.Join(outDir, "certificate.pem"))
	require.NoError(t, err)
	keyPEM, err := os.ReadFile(filepath.Join(outDir, "key.pem"))
	require.NoError(t, err)
	// The key is the credential; the permission bits are what ADR-0015
	// charges every file holding one with.
	info, err := os.Stat(filepath.Join(outDir, "key.pem"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the private key is readable by its owner alone")
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	require.NoError(t, err)

	// The command reads no record and writes none, so nothing has
	// registered this operator and the listener does not admit it.
	status, body := machineRequest(t, machineClient(t, pair), http.MethodGet, url+"/identity")
	require.Equal(t, http.StatusUnauthorized, status, string(body))

	registerOperator(t, p, operatorGRN)

	status, body = machineRequest(t, machineClient(t, pair), http.MethodGet, url+"/identity")
	require.Equal(t, http.StatusOK, status, string(body))
	assert.JSONEq(t, `{"grn":"`+operatorGRN.String()+`"}`, string(body))

	// An agent subject is refused, and nothing is minted for it: an
	// agent's certificate comes from the surface an assigned operator
	// calls, never from this command.
	agentOutDir, err := runIssueCertificate(t, keyFile, scopedGRN("acme", agent.ServiceType, "bot-1").String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not an operator")
	_, statErr := os.Stat(filepath.Join(agentOutDir, "certificate.pem"))
	assert.True(t, os.IsNotExist(statErr), "a refused subject leaves no certificate behind")
}
