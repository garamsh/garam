package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/message"
	"github.com/garamsh/garam/internal/operator"
)

// tokenStack is the managed execution stack, whose hosted control
// service and controller each hold a key and the token garam accepts
// for it, which is what the contract values take (ADR-0100 G1c,
// ADR-0102, ADR-0118).
type tokenStack struct {
	*executionStack
}

func newTokenStack(t *testing.T) *tokenStack {
	t.Helper()
	return &tokenStack{executionStack: newManagedStack(t)}
}

// tokenSend performs one request over the token client with the
// headers given, and answers it.
func (s *tokenStack) tokenSend(t *testing.T, method, path string, headers map[string]string, body string) reply {
	t.Helper()
	req, err := http.NewRequest(method, s.machine+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := s.client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return reply{status: resp.StatusCode, body: string(answer), version: resp.Header.Get(operator.ContractVersionHeader)}
}

// v2 sends one request presenting token under version, with the
// operation authority or reference in its own header where one is
// given, and holds the answer to naming that version.
func (s *tokenStack) v2(t *testing.T, method, path, version, token, operation, body string) reply {
	t.Helper()
	headers := map[string]string{"Authorization": "Garam-Token " + token, operator.ContractVersionHeader: version}
	if operation != "" {
		headers["Garam-Operation"] = operation
	}
	got := s.tokenSend(t, method, path, headers, body)
	assert.Equal(t, version, got.version, "every answer names the value the token takes")
	return got
}

// shownToHosted exchanges the controller's key for a token shown to
// the hosted control service.
func (s *tokenStack) shownToHosted(t *testing.T) string {
	t.Helper()
	return exchangeKey(t, s.client, s.machine, s.controller, s.controllerKey, &s.hosted)
}

// createdV2 creates an agent on the controller under
// managed-enrollment.v2 and answers it with its creation reference.
func (s *tokenStack) createdV2(t *testing.T, requestID string) managed {
	t.Helper()
	ref := s.creationRef(t, s.member, requestID)
	got := s.v2(t, http.MethodPost, "/operators/"+s.controller.String()+"/managed-agents",
		agent.TokenEnrollmentContractVersion, s.hostedToken, "", creationBody(requestID, ref))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var m machinegen.ManagedAgent
	require.NoError(t, json.Unmarshal([]byte(got.body), &m))
	g, err := grn.Parse(m.Grn)
	require.NoError(t, err)
	return managed{agent: g, ref: ref}
}

func tokenActivationBody(requestID string, g agent.Generation, replaces, ref *string, token string) string {
	raw, _ := json.Marshal(map[string]any{
		"requestId": requestID, "epoch": "1", "generation": string(g),
		"replacesActivationId": replaces, "operationRef": ref, "token": token,
	})
	return string(raw)
}

// standingV2 answers what execution-fence.v2 introspection says of the
// forwarded token for g.
func (s *tokenStack) standingV2(t *testing.T, g grn.GRN, token string) machinegen.ExecutionIntrospection {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"token": token})
	require.NoError(t, err)
	got := s.v2(t, http.MethodPost, "/agents/"+g.String()+"/execution/introspection",
		agent.TokenExecutionContractVersion, s.hostedToken, "", string(raw))
	require.Equal(t, http.StatusOK, got.status, got.body)
	var in machinegen.ExecutionIntrospection
	require.NoError(t, json.Unmarshal([]byte(got.body), &in))
	return in
}

// claimWith is the agent's claim on its own queue with its token and
// the execution token beside it, as its adapter sends one.
func (s *tokenStack) claimWith(t *testing.T, g grn.GRN, token, execution string) int {
	t.Helper()
	return s.tokenSend(t, http.MethodPost, "/agents/"+g.String()+"/claims", map[string]string{
		"Authorization": "Garam-Token " + token, "Garam-Execution": execution,
	}, "").status
}

// TestTokenContracts_AHostedOperatorDrivesAManagedAgentWithTokensAlone
// is G1c end to end against Postgres and the listener `serve machine`
// assembles. The hosted control service presents its own token and no
// certificate: it creates a managed agent, activates it on the agent
// token the controller minted to be shown to it, inspects it, proves
// the controller by the controller's token shown to it, recovers it
// with no certificate request, and deactivates it. The agent's own
// token claims under the activation, and the recovery refuses it and
// fences the token forwarded before it, while one minted in the new
// lineage is current.
func TestTokenContracts_AHostedOperatorDrivesAManagedAgentWithTokensAlone(t *testing.T) {
	s := newTokenStack(t)
	m := s.createdV2(t, "c-1")
	own := s.agentToken(t, m.agent, nil)
	shown := s.agentToken(t, m.agent, &s.hosted)
	opened := s.lineageOf(t, m.agent)
	require.NotEmpty(t, opened, "creation opened the lineage the controller mints in")

	path := "/agents/" + m.agent.String()
	got := s.v2(t, http.MethodPost, path+"/activations", agent.TokenExecutionContractVersion, s.hostedToken, "",
		tokenActivationBody("first", generation(1), nil, &m.ref, shown.Token))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	activation := activationOf(t, got)
	assert.Equal(t, http.StatusNoContent, s.claimWith(t, m.agent, own.Token, activation.Token),
		"the agent's own token claims under the activation")

	g1 := generation(1)
	raw, err := json.Marshal(map[string]any{"token": shown.Token, "generation": g1})
	require.NoError(t, err)
	got = s.v2(t, http.MethodPost, path+"/execution/introspection", agent.TokenExecutionContractVersion, s.hostedToken, "", string(raw))
	require.Equal(t, http.StatusOK, got.status, got.body)
	var in machinegen.ExecutionIntrospection
	require.NoError(t, json.Unmarshal([]byte(got.body), &in))
	assert.Equal(t, machinegen.ExecutionIntrospectionCredential("current"), in.Credential)
	assert.Equal(t, machinegen.ExecutionIntrospectionGeneration("current"), in.Generation)

	raw, err = json.Marshal(map[string]any{"token": own.Token})
	require.NoError(t, err)
	got = s.v2(t, http.MethodPost, path+"/execution/introspection", agent.TokenExecutionContractVersion, s.hostedToken, "", string(raw))
	assert.Equal(t, http.StatusUnprocessableEntity, got.status,
		"a token minted for garam proves nothing forwarded to it: %s", got.body)

	proof := func(token string) reply {
		raw, err := json.Marshal(map[string]any{"token": token, "agent": m.agent.String()})
		require.NoError(t, err)
		return s.v2(t, http.MethodPost, "/operators/"+s.controller.String()+"/introspection",
			operator.TokenAuthorityContractVersion, s.hostedToken, "", string(raw))
	}
	got = proof(s.shownToHosted(t))
	require.Equal(t, http.StatusOK, got.status, got.body)
	var proven machinegen.ControllerProof
	require.NoError(t, json.Unmarshal([]byte(got.body), &proven))
	assert.Equal(t, s.controller.String(), proven.Operator)
	require.NotNil(t, proven.Agent)
	assert.Equal(t, "1", proven.Agent.Epoch)
	got = proof(s.controllerToken)
	assert.Equal(t, http.StatusUnprocessableEntity, got.status,
		"the controller's token for garam does not prove it to the caller: %s", got.body)

	body := `{"requestId":"rec-1","epoch":"1"}`
	digest := sha256.Sum256([]byte(body))
	status, minted := s.mintRecovery(t, m.agent, hex.EncodeToString(digest[:]), "h-rec-1")
	require.Equal(t, http.StatusCreated, status, minted)
	authority := authorityOf(t, minted).Authority
	got = s.v2(t, http.MethodPost, path+"/credential-recovery", agent.TokenExecutionContractVersion, s.hostedToken, "", body)
	assert.Equal(t, http.StatusUnauthorized, got.status, "control: no authority in its header recovers nothing: %s", got.body)
	got = s.v2(t, http.MethodPost, path+"/credential-recovery", agent.TokenExecutionContractVersion, s.hostedToken, authority, body)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var answer map[string]any
	require.NoError(t, json.Unmarshal([]byte(got.body), &answer))
	assert.ElementsMatch(t, []string{"grn", "lineage"}, keysOf(answer), "a v2 recovery signs nothing and names no chain")
	assert.Equal(t, s.lineageOf(t, m.agent), answer["lineage"], "it names the lineage it opened")
	assert.NotEqual(t, opened, answer["lineage"], "which is the next one")

	assert.Equal(t, http.StatusUnauthorized, s.claimWith(t, m.agent, own.Token, activation.Token),
		"the recovery refuses the replaced lineage's token on the next request")
	assert.Equal(t, machinegen.ExecutionIntrospectionCredential("fenced"), s.standingV2(t, m.agent, shown.Token).Credential,
		"the token forwarded before the recovery is answered fenced, not unknown")
	next := s.agentToken(t, m.agent, &s.hosted)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredential("current"), s.standingV2(t, m.agent, next.Token).Credential,
		"control: a token minted in the new lineage is current")

	again := s.v2(t, http.MethodPost, path+"/credential-recovery", agent.TokenExecutionContractVersion, s.hostedToken, authority, body)
	require.Equal(t, http.StatusCreated, again.status, again.body)
	assert.JSONEq(t, got.body, again.body, "a retry answers the recorded lineage")

	replaces := activation.ActivationId
	got = s.v2(t, http.MethodPost, path+"/activations", agent.TokenExecutionContractVersion, s.hostedToken, "",
		tokenActivationBody("second", generation(2), &replaces, nil, shown.Token))
	assert.Equal(t, http.StatusConflict, got.status, "the replaced lineage's token activates nothing: %s", got.body)
	assert.Contains(t, got.body, "credential is not current", "the fence refused it")
	got = s.v2(t, http.MethodPost, path+"/activations", agent.TokenExecutionContractVersion, s.hostedToken, "",
		tokenActivationBody("second", generation(2), &replaces, nil, next.Token))
	require.Equal(t, http.StatusCreated, got.status, "control: the new lineage's token activates: %s", got.body)
	second := activationOf(t, got)

	got = s.v2(t, http.MethodPost, path+"/activations/"+second.ActivationId+"/deactivation",
		agent.TokenExecutionContractVersion, s.hostedToken, "", "")
	assert.Equal(t, http.StatusOK, got.status, got.body)
}

// keysOf answers the names of m's members.
func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// lineageOf reads the credential lineage g's execution holds now.
func (s *tokenStack) lineageOf(t *testing.T, g grn.GRN) string {
	t.Helper()
	var lineage string
	require.NoError(t, s.pool.QueryRow(context.Background(),
		`SELECT coalesce(lineage::text, '') FROM agent_executions WHERE agent_grn = $1`, g.String()).Scan(&lineage))
	return lineage
}

// mintRecovery asks, as the owner, for an agent:recover handoff of g to
// the hosted control service, bound to digest.
func (s *tokenStack) mintRecovery(t *testing.T, g grn.GRN, digest, requestID string) (int, string) {
	t.Helper()
	body := `{"audience":"` + s.hosted.String() + `","operation":"agent:recover","target":"` + g.String() +
		`","requestId":"` + requestID + `","bodySha256":"` + digest + `"}`
	return call(t, http.DefaultClient, http.MethodPost, s.browser+"/orgs/acme/operation-authorities", s.owner, body)
}

// TestTokenContracts_EachFamilyIsServedUnderItsOneValue holds every
// contract family to the one value ADR-0118 leaves it: a request under
// the value the certificate path took is refused with 400 naming the
// value served, whether its token was accepted or refused. The request
// under the served value is the control in each family: it is answered
// past the contract check, whatever the rest of it then meets.
func TestTokenContracts_EachFamilyIsServedUnderItsOneValue(t *testing.T) {
	s := newTokenStack(t)
	agentPath := "/agents/" + s.agent.String()
	for _, family := range []struct {
		name, method, path string
		removed, served    string
	}{
		{"operation authority", http.MethodGet, "/operation-references/unknown", "operation-authority.v1", operator.TokenAuthorityContractVersion},
		{"execution fence", http.MethodPost, agentPath + "/execution/introspection", "execution-fence.v1", agent.TokenExecutionContractVersion},
		{"managed enrollment", http.MethodPost, "/operators/" + s.controller.String() + "/managed-agents", "managed-enrollment.v1", agent.TokenEnrollmentContractVersion},
		{"durable delivery", http.MethodPost, "/outbox-messages", "durable-delivery.v1", message.TokenDeliveryContractVersion},
	} {
		t.Run(family.name, func(t *testing.T) {
			byToken := func(token, version string) reply {
				return s.tokenSend(t, family.method, family.path, map[string]string{
					"Authorization": "Garam-Token " + token, operator.ContractVersionHeader: version,
				}, `{}`)
			}
			got := byToken(s.hostedToken, family.removed)
			assert.Equal(t, http.StatusBadRequest, got.status, got.body)
			assert.Equal(t, family.served, got.version)
			assert.Contains(t, got.body, "served under "+family.served, "the refusal names the value served")
			got = byToken(s.hostedToken, family.served)
			assert.Equal(t, family.served, got.version)
			assert.NotContains(t, got.body, "Garam-Contract-Version", "control: the served value passes the check")

			// A token the listener refused is held to the same value, and
			// under it meets the listener's refusal.
			refused := operator.TokenPrefix + strings.Repeat("0", 64)
			got = byToken(refused, family.served)
			assert.Equal(t, http.StatusUnauthorized, got.status, "a refused token under the served value: %s", got.body)
			assert.Equal(t, family.served, got.version)
			got = byToken(refused, family.removed)
			assert.Equal(t, http.StatusBadRequest, got.status, got.body)
			assert.Contains(t, got.body, "served under "+family.served)
		})
	}
}

// TestTokenContracts_AForwardedTokenProvesItsOwnAgentAlone holds the
// forwarded token to the agent it was minted for: another agent's
// token shown to the same caller proves nothing of this one. The
// agent's own token is the control.
func TestTokenContracts_AForwardedTokenProvesItsOwnAgentAlone(t *testing.T) {
	s := newTokenStack(t)
	m := s.createdV2(t, "c-1")
	shown := s.agentToken(t, m.agent, &s.hosted)
	path := "/agents/" + m.agent.String() + "/execution/introspection"
	assert.Equal(t, machinegen.ExecutionIntrospectionCredential("current"), s.standingV2(t, m.agent, shown.Token).Credential,
		"control: the agent's own token is admitted")

	another := s.agentToken(t, s.createdV2(t, "c-2").agent, &s.hosted)
	raw, err := json.Marshal(map[string]any{"token": another.Token})
	require.NoError(t, err)
	got := s.v2(t, http.MethodPost, path, agent.TokenExecutionContractVersion, s.hostedToken, "", string(raw))
	assert.Equal(t, http.StatusUnprocessableEntity, got.status,
		"another agent's token shown to the caller proves nothing of this one: %s", got.body)
}

// TestTokenContracts_ATokensAudienceIsAnOperatorTheOrganizationHolds
// holds 000041's foreign keys: an exchange naming an audience nobody
// registered is refused, and deleting the hosted operator takes every
// token minted to be shown to it, the operator's and the agent's. The
// tokens minted for garam are the control: they stay.
func TestTokenContracts_ATokensAudienceIsAnOperatorTheOrganizationHolds(t *testing.T) {
	s := newTokenStack(t)
	nobody := scopedGRN("acme", operator.ServiceType, "nobody")
	_, err := s.client.PostForm(s.machine+"/oauth/token", url.Values{
		"grant_type":            {"client_credentials"},
		"client_id":             {s.controller.String()},
		"client_assertion_type": {operator.ClientAssertionTypeJWT},
		"client_assertion":      {freshAssertion(t, s.controllerKey, s.controller)},
		"audience":              {nobody.String()},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, countRows(t, s.pool, `SELECT count(*) FROM operator_tokens WHERE audience_grn = $1`, nobody.String()),
		"an unregistered audience is refused and records nothing")

	m := s.createdV2(t, "c-1")
	s.shownToHosted(t)
	s.agentToken(t, m.agent, &s.hosted)
	s.agentToken(t, m.agent, nil)
	shown := func(table string) (int, int) {
		var toHosted, toGaram int
		require.NoError(t, s.pool.QueryRow(context.Background(),
			`SELECT count(*) FILTER (WHERE audience_grn = $1), count(*) FILTER (WHERE audience_grn IS NULL) FROM `+table,
			s.hosted.String()).Scan(&toHosted, &toGaram))
		return toHosted, toGaram
	}
	for _, table := range []string{"operator_tokens", "agent_tokens"} {
		toHosted, toGaram := shown(table)
		require.Positive(t, toHosted, table)
		require.Positive(t, toGaram, table)
	}

	require.NoError(t, operator.NewPostgresStore(s.pool).Delete(context.Background(), s.hosted))
	for _, table := range []string{"operator_tokens", "agent_tokens"} {
		toHosted, toGaram := shown(table)
		assert.Zero(t, toHosted, "deleting the audience takes the tokens shown to it: %s", table)
		assert.Positive(t, toGaram, "control: garam's tokens stay: %s", table)
	}
}
