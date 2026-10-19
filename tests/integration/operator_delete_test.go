package integration_test

import (
	"context"
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// identifies reports the status the listener answers a caller
// presenting pair. Each call is a client of its own, so it is a
// connection of its own, and it reads the record as it stands now
// rather than as the last handshake found it.
func identifies(t *testing.T, url string, pair tls.Certificate) int {
	t.Helper()
	status, _ := machineRequest(t, machineClient(t, pair), http.MethodGet, url+"/identity")
	return status
}

// TestOperatorDelete_StopsTheCertificateItAuthenticatesWith is
// ADR-0070 end to end against real Postgres and a real handshake: an
// operator holding a certificate minted out of band authenticates, and
// stops authenticating the moment
// its organization deletes the record. The certificate is unchanged
// across the two calls and has not expired, so what refuses the second
// is the delete.
//
// The admitted call before it is the control. Without it the refusal
// would hold for an operator that had never authenticated at all,
// which is a different state and the one most likely to arrive by
// accident. The second operator is the other control: it is deleted by
// nothing, presents a certificate from the same authority on the same
// listener, and goes on authenticating — so what stopped the first is
// its own record and not the check refusing everyone.
func TestOperatorDelete_StopsTheCertificateItAuthenticatesWith(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	keyFile, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	store := operator.NewPostgresStore(p)

	deleted := register(t, store, "one")
	kept := register(t, store, "two")
	deletedCertificate := mintedOutOfBand(t, keyFile, deleted)
	keptCertificate := mintedOutOfBand(t, keyFile, kept)

	require.Equal(t, http.StatusOK, identifies(t, url, deletedCertificate),
		"the operator authenticates before its record is deleted")
	require.Equal(t, http.StatusOK, identifies(t, url, keptCertificate))

	require.NoError(t, store.Delete(ctx, deleted))

	assert.Equal(t, http.StatusUnauthorized, identifies(t, url, deletedCertificate),
		"the certificate stops authenticating when the record goes")
	assert.Equal(t, http.StatusOK, identifies(t, url, keptCertificate),
		"the operator nobody deleted is untouched")

	// The delete leaves the identifier holding nothing: no record and no
	// certificate garam would measure a renewal against. The
	// certificate row has no foreign key to go by (000019), so its
	// absence is the statement's rather than the cascade's.
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operators WHERE grn = $1`, deleted.String()))
	assert.Equal(t, 0, countRows(t, p,
		`SELECT count(*) FROM operator_certificates WHERE operator_grn = $1`, deleted.String()))
	assert.Equal(t, 1, countRows(t, p,
		`SELECT count(*) FROM operator_certificates WHERE operator_grn = $1`, kept.String()),
		"the operator nobody deleted keeps its lineage")

	// Registering the identifier again is a clean start rather than an
	// operator that reads as holding a certificate off a row the delete
	// left behind — which is what the certificate row's removal is for.
	reregistered := register(t, store, "one")
	fresh, err := store.Get(ctx, reregistered)
	require.NoError(t, err)
	assert.True(t, fresh.CertificateIssuedAt().IsZero())
}
