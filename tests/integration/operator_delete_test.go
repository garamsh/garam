package integration_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// enrol spends the operator's token against the machine listener and
// returns the pair the operator holds afterwards: the certificate
// garam signed, and the key the operator generated and never sent
// (ADR-0064).
func enrol(t *testing.T, url string, e *operator.Enrollment) tls.Certificate {
	t.Helper()
	request, key := enrollmentRequest(t)
	body, err := json.Marshal(machinegen.Enrollment{
		Token:                 e.Token(),
		CertificateRequestPem: request,
	})
	require.NoError(t, err)
	status, answered := machineSend(t, anonymousMachineClient(t), http.MethodPost, url+"/enrollment", string(body))
	require.Equal(t, http.StatusCreated, status, string(answered))
	var signed machinegen.EnrolledCertificate
	require.NoError(t, json.Unmarshal(answered, &signed))
	pair, err := tls.X509KeyPair([]byte(signed.CertificatePem), pemKey(t, key))
	require.NoError(t, err)
	return pair
}

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
// operator enrols, authenticates, and stops authenticating the moment
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
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)
	store := operator.NewPostgresStore(p)

	deleted, deletedToken := register(t, store, "one")
	kept, keptToken := register(t, store, "two")
	deletedCertificate := enrol(t, url, deletedToken)
	keptCertificate := enrol(t, url, keptToken)

	require.Equal(t, http.StatusOK, identifies(t, url, deletedCertificate),
		"the operator authenticates before its record is deleted")
	require.Equal(t, http.StatusOK, identifies(t, url, keptCertificate))

	require.NoError(t, store.Delete(ctx, deleted))

	assert.Equal(t, http.StatusUnauthorized, identifies(t, url, deletedCertificate),
		"the certificate stops authenticating when the record goes")
	assert.Equal(t, http.StatusOK, identifies(t, url, keptCertificate),
		"the operator nobody deleted is untouched")

	// The delete leaves the identifier holding nothing: no record, no
	// certificate garam would measure a renewal against, and no token.
	// The certificate row has no foreign key to go by (000019), so its
	// absence is the statement's rather than the cascade's.
	assert.Equal(t, 0, countRows(t, p, `SELECT count(*) FROM operators WHERE grn = $1`, deleted.String()))
	assert.Equal(t, 0, countRows(t, p,
		`SELECT count(*) FROM operator_certificates WHERE operator_grn = $1`, deleted.String()))
	assert.Equal(t, 0, countRows(t, p,
		`SELECT count(*) FROM operator_enrollment_tokens WHERE operator_grn = $1`, deleted.String()))
	assert.Equal(t, 1, countRows(t, p,
		`SELECT count(*) FROM operator_certificates WHERE operator_grn = $1`, kept.String()),
		"the operator nobody deleted keeps its lineage")

	// Registering the identifier again is a clean start rather than an
	// operator that reads as already enrolled off a row the delete left
	// behind — which is what the certificate row's removal is for.
	reregistered, _ := register(t, store, "one")
	fresh, err := store.Get(ctx, reregistered)
	require.NoError(t, err)
	assert.Equal(t, operator.EnrollmentStatePending, fresh.EnrollmentState())
}

// TestOperatorDelete_RefusesAnOperatorADefinitionNames holds constraint
// 4 of #1085 as a test rather than as the data: `agent_definitions` is
// empty on the deployment, so what decides this is the foreign key
// 000018 declares and not a row anybody has.
//
// The delete that succeeds is the control. The two operators are
// registered the same way in the same organization, and only one is
// named by a definition, so what refuses that one is the definition
// rather than the route refusing every delete.
func TestOperatorDelete_RefusesAnOperatorADefinitionNames(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	store := operator.NewPostgresStore(p)
	agents := agent.NewPostgresStore(p)

	named := scopedGRN("acme", operator.ServiceType, "named")
	defineAgent(t, p, agents, named, agent.Values{"image": "ghcr.io/acme/bot:1"})
	unnamed, _ := register(t, store, "unnamed")

	err := store.Delete(ctx, named)
	require.Error(t, err)
	assert.ErrorIs(t, err, operator.ErrOperatorNamedByDefinition)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))
	assert.Equal(t, 1, countRows(t, p, `SELECT count(*) FROM operators WHERE grn = $1`, named.String()),
		"a refused delete removes nothing")

	require.NoError(t, store.Delete(ctx, unnamed))

	// A record the organization does not hold is a missing one, and it
	// is told from the refusal above by kind: one says the operator is
	// not there and the other says it is and something names it.
	assert.ErrorIs(t, store.Delete(ctx, scopedGRN("acme", operator.ServiceType, "never")),
		operator.ErrOperatorNotFound)
}
