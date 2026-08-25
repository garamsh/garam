package e2e_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/org"
)

// defineAgent records a definition of an agent operator is to run, as
// the console writes one through internal/api.
func defineAgent(t *testing.T, p *pgxpool.Pool, s *agent.PostgresStore, operatorGRN grn.GRN, values agent.Values) *agent.Definition {
	t.Helper()
	d, err := agent.NewDefinition(
		orgRootGRN(operatorGRN.Organization),
		registerOperator(t, p, operatorGRN), values, scopedGRN(operatorGRN.Organization, "user", "ada"))
	require.NoError(t, err)
	require.NoError(t, s.Define(context.Background(), d))
	return d
}

// definitionsOf polls the operator's definitions, as the adapter
// ADR-0033 has polling does.
func definitionsOf(t *testing.T, client *http.Client, url string) []machinegen.Definition {
	t.Helper()
	status, body := machineRequest(t, client, http.MethodGet, url+"/definitions")
	require.Equal(t, http.StatusOK, status, string(body))
	var definitions []machinegen.Definition
	require.NoError(t, json.Unmarshal(body, &definitions))
	return definitions
}

// TestDefinitionSurface_AnOperatorReadsClaimsAndRunsWhatNamesIt carries
// ADR-0033 end to end over the mTLS listener against real Postgres: an
// operator reads exactly the definitions naming it, the certificate
// route refuses the definition's agent as unassigned until the claim,
// and the claim is what admits the operator to it. A definition is
// claimed once — a second claim conflicts — and a definition naming
// another operator answers as one that does not exist, as the read
// scopes it.
func TestDefinitionSurface_AnOperatorReadsClaimsAndRunsWhatNamesIt(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(orgRootGRN("globex"), "globex")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	outOfBand, err := ca.NewService(ca.NewPostgresStore(p), sealer, ca.Config{})
	require.NoError(t, err)
	_, evaluator := policyEngine(t, p)
	url := machineListener(t, p, sealer, evaluator)

	oneGRN := scopedGRN("acme", agent.ServiceTypeOperator, "one")
	one := machineClient(t, installed(t, outOfBand, oneGRN))
	two := machineClient(t, installed(t, outOfBand, scopedGRN("acme", agent.ServiceTypeOperator, "two")))

	store := agent.NewPostgresStore(p)
	mine := defineAgent(t, p, store, oneGRN, agent.Values{"model": "haiku"})
	siblings := defineAgent(t, p, store, scopedGRN("acme", agent.ServiceTypeOperator, "two"), agent.Values{})
	defineAgent(t, p, store, scopedGRN("globex", agent.ServiceTypeOperator, "one"), agent.Values{})

	// The read is scoped by the definition's operator field: each
	// operator reads exactly the definitions naming it, and the claim
	// state rides along — absent before any claim.
	read := definitionsOf(t, one, url)
	require.Len(t, read, 1, "the definitions naming this operator and no other's")
	assert.Equal(t, mine.Agent().String(), read[0].AgentGrn)
	assert.Equal(t, map[string]string{"model": "haiku"}, read[0].Values)
	assert.Nil(t, read[0].Claim, "an unclaimed definition carries no claim")

	// The control: before the claim the certificate route refuses the
	// definition's agent GRN as unassigned, exactly as it refuses an
	// agent that was never defined.
	status, body := machineRequest(t, one, http.MethodPost, url+"/agents/"+mine.Agent().String()+"/certificate")
	require.Equal(t, http.StatusForbidden, status, string(body))

	// The claim binds the operator to the agent at the first epoch.
	status, body = machineRequest(t, one, http.MethodPost, url+"/definitions/"+mine.Agent().String()+"/claim")
	require.Equal(t, http.StatusCreated, status, string(body))
	var assignment machinegen.AgentAssignment
	require.NoError(t, json.Unmarshal(body, &assignment))
	assert.Equal(t, mine.Agent().String(), assignment.Grn)
	assert.Equal(t, oneGRN.String(), assignment.Operator)
	assert.Equal(t, 1, assignment.Epoch)

	// The claim is where the agent comes to exist: nothing reported it,
	// and the row is there with nothing reported on it.
	a, err := store.Get(ctx, mine.Agent())
	require.NoError(t, err)
	assert.Equal(t, agent.ProvisioningStatePending, a.ProvisioningState())

	// The poll now carries the claim, so an operator that lost the
	// claim's answer recovers the epoch it must present.
	read = definitionsOf(t, one, url)
	require.Len(t, read, 1)
	require.NotNil(t, read[0].Claim)
	assert.Equal(t, 1, read[0].Claim.Epoch)

	// And the certificate route issues for the claimed agent.
	status, body = machineRequest(t, one, http.MethodPost, url+"/agents/"+mine.Agent().String()+"/certificate")
	require.Equal(t, http.StatusCreated, status, string(body))
	var issued machinegen.AgentCertificate
	require.NoError(t, json.Unmarshal(body, &issued))
	assert.Equal(t, mine.Agent().String(), issued.Grn)

	// A definition is claimed once: a second claim of the same agent
	// GRN is a conflict, never an override.
	status, body = machineRequest(t, one, http.MethodPost, url+"/definitions/"+mine.Agent().String()+"/claim")
	assert.Equal(t, http.StatusConflict, status, string(body))

	// The operator the definition does not name reads its own
	// definitions and not this one, and its claim of this one answers
	// as a definition that does not exist — beside its claim of its
	// own, so what refuses is the naming and not the route.
	read = definitionsOf(t, two, url)
	require.Len(t, read, 1)
	assert.Equal(t, siblings.Agent().String(), read[0].AgentGrn)
	status, body = machineRequest(t, two, http.MethodPost, url+"/definitions/"+mine.Agent().String()+"/claim")
	assert.Equal(t, http.StatusNotFound, status, string(body))
	status, body = machineRequest(t, two, http.MethodPost, url+"/definitions/"+siblings.Agent().String()+"/claim")
	require.Equal(t, http.StatusCreated, status, string(body))
}

// TestAgentStore_ConcurrentClaimsSerializeToOneWinner runs the race
// ADR-0033 names: claims of one definition arriving together serialize
// on the definition's row lock, so exactly one binds the agent and
// every other reads the assignment the winner wrote and conflicts.
func TestAgentStore_ConcurrentClaimsSerializeToOneWinner(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	s := agent.NewPostgresStore(p)
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(orgRootGRN("acme"), "acme")))

	operator := scopedGRN("acme", agent.ServiceTypeOperator, "one")
	d := defineAgent(t, p, s, operator, agent.Values{})

	const contenders = 4
	start := make(chan struct{})
	outcomes := make([]error, contenders)
	var wg sync.WaitGroup
	for i := range contenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, outcomes[i] = s.Claim(ctx, d.Agent(), operator)
		}()
	}
	close(start)
	wg.Wait()

	won := 0
	for _, err := range outcomes {
		if err == nil {
			won++
			continue
		}
		assert.ErrorIs(t, err, agent.ErrDefinitionClaimed, "a losing claim conflicts rather than overrides")
	}
	assert.Equal(t, 1, won, "exactly one of the racing claims binds the agent")

	assignment, err := s.GetAssignment(ctx, d.Agent())
	require.NoError(t, err)
	assert.Equal(t, operator, assignment.Operator())
	assert.Equal(t, 1, assignment.Epoch(), "the one claim wrote the first epoch")
}
