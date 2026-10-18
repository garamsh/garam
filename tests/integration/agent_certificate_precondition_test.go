package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/operator"
	"github.com/garamsh/garam/internal/org"
	orgpg "github.com/garamsh/garam/internal/org/postgres"
)

// issuedTo asks the certificate route for the named agent as client
// presents itself, and returns the certificate it answered with.
func issuedTo(t *testing.T, client *http.Client, url, agentGRN string) machinegen.MachineCertificate {
	t.Helper()
	status, body := machineRequest(t, client, http.MethodPost, url+"/agents/"+agentGRN+"/certificate")
	require.Equal(t, http.StatusCreated, status, string(body))
	var issued machinegen.MachineCertificate
	require.NoError(t, json.Unmarshal(body, &issued))
	return issued
}

// TestAgentCertificate_TheAssignmentIsTheWholePrecondition holds what
// `api/machine.yaml` states of `issueAgentCertificate`: the assignment
// is the whole of the precondition, so the route carries no renewal
// window and no lineage. A caller the assignment admits asks at any
// time and as often as it needs, and a certificate this route has
// already replaced asks again successfully.
//
// An absence is only attributable beside the presence it is an absence
// of: three answers prove a window was not crossed, not that none
// exists. `renewIdentityCertificate` holds both bounds on this same
// listener, over the same issuer and the same 409, and the two
// refusals at the end are what say the answers above are this route's
// rule rather than a window nothing here happened to reach.
func TestAgentCertificate_TheAssignmentIsTheWholePrecondition(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	require.NoError(t, orgpg.New(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	_, sealer := keyEncryptionKey(t)
	_, engine := policyEngine(t, p)
	url := machineListener(t, p, sealer, engine)

	outOfBand, err := ca.NewService(capg.New(p), sealer, ca.Config{})
	require.NoError(t, err)
	holder := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", operator.ServiceType, "one")))

	status, body := machineRequest(t, holder, http.MethodPost, url+"/agents")
	require.Equal(t, http.StatusCreated, status, string(body))
	var created machinegen.AgentAssignment
	require.NoError(t, json.Unmarshal(body, &created))

	// Three back to back with nothing between them, each answered, and
	// each answer a certificate of its own rather than a copy of the
	// one before it. The second is what a renewal window would refuse.
	first := issuedTo(t, holder, url, created.Grn)
	second := issuedTo(t, holder, url, created.Grn)
	third := issuedTo(t, holder, url, created.Grn)

	// The certificate the first call answered with, replaced twice over
	// by the two after it, renews itself over the connection it
	// authenticates: the route records no certificate it issued, so
	// there is no lineage for a replaced one to have lost.
	fourth := issuedTo(t, machineClient(t, pairOf(t, first)), url, created.Grn)

	distinct := map[string]struct{}{}
	for _, issued := range []machinegen.MachineCertificate{first, second, third, fourth} {
		distinct[serialOf(t, pairOf(t, issued))] = struct{}{}
	}
	assert.Len(t, distinct, 4, "each call signed a certificate of its own")

	// What the route does hold is the assignment, so the answers above
	// are what it admits rather than what the route answers anybody. An
	// operator of the agent's own organization holding nothing asks and
	// is refused.
	stranger := machineClient(t, installed(t, p, outOfBand, scopedGRN("acme", operator.ServiceType, "two")))
	status, body = machineRequest(t, stranger, http.MethodPost, url+"/agents/"+created.Grn+"/certificate")
	assert.Equal(t, http.StatusForbidden, status, string(body))
	assert.Contains(t, string(body), `"kind":"permission_denied"`)

	// The control for the window. An operator certificate with its
	// whole life ahead of it is refused as too early on the route that
	// has a window — the refusal the agent route above would have
	// carried if it had one, from the same process a moment later.
	tooEarly := machineClient(t, installed(t, p,
		aged(t, p, sealer, time.Hour, time.Now()), scopedGRN("acme", operator.ServiceType, "three")))
	status, body = renew(t, tooEarly, url)
	assert.Equal(t, http.StatusConflict, status, string(body))
	assert.Contains(t, string(body), `"kind":"failed_precondition"`)

	// The control for the lineage. Three quarters through an hour-long
	// certificate is inside the window, so this one is replaced — and
	// the certificate it replaced asks again and is refused as
	// superseded, where the agent's fourth call above was answered.
	inWindow := machineClient(t, installed(t, p,
		aged(t, p, sealer, time.Hour, time.Now().Add(-45*time.Minute)), scopedGRN("acme", operator.ServiceType, "four")))
	status, body = renew(t, inWindow, url)
	require.Equal(t, http.StatusCreated, status, string(body))
	status, body = renew(t, inWindow, url)
	assert.Equal(t, http.StatusConflict, status, string(body))
	assert.Contains(t, string(body), `"kind":"already_exists"`)
}
