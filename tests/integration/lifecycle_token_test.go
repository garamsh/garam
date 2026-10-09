package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	machinegen "github.com/garamsh/garam/api/machine"
	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// tokenStack is the managed execution stack once the hosted control
// service and the controller each hold a credential and the token
// garam accepts for it, which is what the v2 contract values take
// (ADR-0100 G1c).
type tokenStack struct {
	*executionStack
	client *http.Client
	// controllerCredential is what the controller exchanges, for
	// garam's token or for one shown to the hosted control service.
	controllerCredential string
	hostedToken          string
	controllerToken      string
}

func newTokenStack(t *testing.T) *tokenStack {
	t.Helper()
	s := &tokenStack{executionStack: newManagedStack(t)}
	s.client, _ = tokenClient(t)
	s.hostedToken = exchangeOver(t, s.client, s.machine, reissued(t, s.executionStack, s.hosted))
	s.controllerCredential = reissued(t, s.executionStack, s.controller)
	s.controllerToken = exchangeOver(t, s.client, s.machine, s.controllerCredential)
	return s
}

// reissued re-issues subject's credential, as the console does, and
// answers it.
func reissued(t *testing.T, s *executionStack, subject grn.GRN) string {
	t.Helper()
	c, err := operator.NewIssuedCredential(subject, scopedGRN(subject.Organization, "user", "ada"))
	require.NoError(t, err)
	require.NoError(t, operator.NewPostgresStore(s.pool).ReissueCredential(context.Background(), c))
	return c.Value()
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

// shownToHosted exchanges the controller's credential for a token
// shown to the hosted control service.
func (s *tokenStack) shownToHosted(t *testing.T) string {
	t.Helper()
	got := s.tokenSend(t, http.MethodPost, "/tokens", map[string]string{
		"Authorization": "Garam-Credential " + s.controllerCredential, operator.ContractVersionHeader: operator.TokenContractVersion,
	}, `{"audience":"`+s.hosted.String()+`"}`)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var token machinegen.MachineToken
	require.NoError(t, json.Unmarshal([]byte(got.body), &token))
	return token.Token
}

// agentToken is the controller's mint of g's token: garam's where
// audience is nil, and one shown to audience otherwise.
func (s *tokenStack) agentToken(t *testing.T, g grn.GRN, audience *grn.GRN) machinegen.AgentToken {
	t.Helper()
	body := `{}`
	if audience != nil {
		body = `{"audience":"` + audience.String() + `"}`
	}
	got := s.v2(t, http.MethodPost, "/agents/"+g.String()+"/tokens", operator.TokenContractVersion, s.controllerToken, "", body)
	require.Equal(t, http.StatusCreated, got.status, got.body)
	var minted machinegen.AgentToken
	require.NoError(t, json.Unmarshal([]byte(got.body), &minted))
	return minted
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
	assert.Zero(t, countRows(t, s.pool, `SELECT count(*) FROM agent_credentials WHERE agent_grn = $1`, m.agent.String()),
		"and records no credential, since it signed none")

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

	got = s.v2(t, http.MethodPost, path+"/activations/"+activation.ActivationId+"/deactivation",
		agent.TokenExecutionContractVersion, s.hostedToken, "", "")
	assert.Equal(t, http.StatusOK, got.status, got.body)
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

// TestTokenContracts_EachCredentialIsServedUnderItsOwnValue holds every
// contract family G1c moves to the rule ADR-0100 states: a request is
// served under the value its credential takes, and one whose value
// disagrees is refused with 400 naming that value. The request whose
// value agrees is the control in each family: it is answered past the
// contract check, whatever the rest of it then meets.
func TestTokenContracts_EachCredentialIsServedUnderItsOwnValue(t *testing.T) {
	s := newTokenStack(t)
	agentPath := "/agents/" + s.agent.String()
	for _, family := range []struct {
		name, method, path string
		certificate, token string
	}{
		{"operation authority", http.MethodGet, "/operation-references/unknown", operator.ContractVersion, operator.TokenAuthorityContractVersion},
		{"execution fence", http.MethodPost, agentPath + "/execution/introspection", agent.ExecutionContractVersion, agent.TokenExecutionContractVersion},
		{"cutover", http.MethodGet, agentPath + "/cutover", agent.CutoverContractVersion, agent.TokenCutoverContractVersion},
		{"managed enrollment", http.MethodPost, "/operators/" + s.controller.String() + "/managed-agents", agent.EnrollmentContractVersion, agent.TokenEnrollmentContractVersion},
	} {
		t.Run(family.name, func(t *testing.T) {
			byToken := func(version string) reply {
				return s.tokenSend(t, family.method, family.path, map[string]string{
					"Authorization": "Garam-Token " + s.hostedToken, operator.ContractVersionHeader: version,
				}, `{}`)
			}
			got := byToken(family.certificate)
			assert.Equal(t, http.StatusBadRequest, got.status, got.body)
			assert.Equal(t, family.token, got.version)
			assert.Contains(t, got.body, "served under "+family.token, "the refusal names the token's value")
			got = byToken(family.token)
			assert.Equal(t, family.token, got.version)
			assert.NotContains(t, got.body, "Garam-Contract-Version", "control: the token's value passes the check")

			got = s.send(t, s.hostedTLS, family.method, family.path, family.token, "", `{}`)
			assert.Equal(t, http.StatusBadRequest, got.status, got.body)
			assert.Equal(t, family.certificate, got.version)
			assert.Contains(t, got.body, family.certificate, "the refusal names the certificate's value")
			got = s.send(t, s.hostedTLS, family.method, family.path, family.certificate, "", `{}`)
			assert.Equal(t, family.certificate, got.version)
			assert.NotContains(t, got.body, "Garam-Contract-Version", "control: the certificate's value passes the check")
		})
	}
}

// TestTokenContracts_EachValueTakesItsOwnCredentialField holds what
// each v2 value takes in place of a certificate: a forwarded token,
// with certificatePem refused beside it, and a recovery with no
// certificate request. The certificate under v1 is the control: the
// same body with its own field is admitted.
func TestTokenContracts_EachValueTakesItsOwnCredentialField(t *testing.T) {
	s := newTokenStack(t)
	m := s.createdV2(t, "c-1")
	shown := s.agentToken(t, m.agent, &s.hosted)
	path := "/agents/" + m.agent.String() + "/execution/introspection"

	both, err := json.Marshal(map[string]any{"token": shown.Token, "certificatePem": s.leaf})
	require.NoError(t, err)
	got := s.v2(t, http.MethodPost, path, agent.TokenExecutionContractVersion, s.hostedToken, "", string(both))
	assert.Equal(t, http.StatusBadRequest, got.status, "certificatePem is refused beside a token: %s", got.body)
	assert.Equal(t, machinegen.ExecutionIntrospectionCredential("current"), s.standingV2(t, m.agent, shown.Token).Credential,
		"control: the token alone is admitted")

	got = s.lifecycle(t, s.hostedTLS, path, "", string(both))
	assert.Equal(t, http.StatusBadRequest, got.status, "token is refused beside a certificate: %s", got.body)

	another := s.agentToken(t, s.createdV2(t, "c-2").agent, &s.hosted)
	raw, err := json.Marshal(map[string]any{"token": another.Token})
	require.NoError(t, err)
	got = s.v2(t, http.MethodPost, path, agent.TokenExecutionContractVersion, s.hostedToken, "", string(raw))
	assert.Equal(t, http.StatusUnprocessableEntity, got.status,
		"another agent's token shown to the caller proves nothing of this one: %s", got.body)

	got = s.v2(t, http.MethodPost, "/agents/"+m.agent.String()+"/credential-recovery", agent.TokenExecutionContractVersion,
		s.hostedToken, "any", `{"requestId":"rec-1","epoch":"1","certificateRequestPem":"x"}`)
	assert.Equal(t, http.StatusBadRequest, got.status, "a v2 recovery refuses a certificate request: %s", got.body)

	got = s.tokenSend(t, http.MethodPost, "/agents/"+m.agent.String()+"/initial-certificate", map[string]string{
		"Authorization": "Garam-Token " + s.hostedToken, operator.ContractVersionHeader: agent.TokenEnrollmentContractVersion,
	}, `{}`)
	assert.Equal(t, http.StatusUnauthorized, got.status, "initial issuance goes with the certificate path: %s", got.body)
}

// TestTokenContracts_ACutoverReferenceTravelsInItsOwnHeaderBesideAToken
// holds agent-cutover.v2's one change: the agent:cutover reference
// moves from Authorization, which the token takes, to a
// Garam-Operation header. The read without it is the control: the
// same request is refused for want of the reference alone.
func TestTokenContracts_ACutoverReferenceTravelsInItsOwnHeaderBesideAToken(t *testing.T) {
	s := &tokenStack{executionStack: newLegacyExecutionStack(t)}
	s.delegate(t, "agent:activate", "agent:recover", "agent:configure", "agent:cutover")
	s.client, _ = tokenClient(t)
	s.hostedToken = exchangeOver(t, s.client, s.machine, reissued(t, s.executionStack, s.hosted))
	refs := s.cutoverRefs(t, s.agent)
	path := "/agents/" + s.agent.String() + "/cutover"

	got := s.v2(t, http.MethodGet, path, agent.TokenCutoverContractVersion, s.hostedToken, "", "")
	assert.Equal(t, http.StatusUnauthorized, got.status, "no reference reads nothing: %s", got.body)
	got = s.v2(t, http.MethodGet, path, agent.TokenCutoverContractVersion, s.hostedToken, refs[""], "")
	require.Equal(t, http.StatusOK, got.status, got.body)
	var c machinegen.Cutover
	require.NoError(t, json.Unmarshal([]byte(got.body), &c))
	require.NotNil(t, c.SourceDigest)

	got = s.v2(t, http.MethodPost, path+"/freeze", agent.TokenCutoverContractVersion, s.hostedToken, refs["/freeze"],
		freezeBody("import-1", *c.SourceDigest, c.Epoch))
	require.Equal(t, http.StatusCreated, got.status, got.body)
	got = s.v2(t, http.MethodPost, path+"/switch", agent.TokenCutoverContractVersion, s.hostedToken, refs["/switch"],
		stageBody("import-1"))
	assert.Equal(t, http.StatusOK, got.status, got.body)
}

// TestTokenContracts_ATokensAudienceIsAnOperatorTheOrganizationHolds
// holds 000041's foreign keys: an exchange naming an audience nobody
// registered is refused, and deleting the hosted operator takes every
// token minted to be shown to it, the operator's and the agent's. The
// tokens minted for garam are the control: they stay.
func TestTokenContracts_ATokensAudienceIsAnOperatorTheOrganizationHolds(t *testing.T) {
	s := newTokenStack(t)
	got := s.tokenSend(t, http.MethodPost, "/tokens", map[string]string{
		"Authorization": "Garam-Credential " + s.controllerCredential, operator.ContractVersionHeader: operator.TokenContractVersion,
	}, `{"audience":"`+scopedGRN("acme", operator.ServiceType, "nobody").String()+`"}`)
	assert.Equal(t, http.StatusBadRequest, got.status, "an unregistered audience: %s", got.body)

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
		require.Equal(t, 1, toHosted, table)
		require.Positive(t, toGaram, table)
	}

	require.NoError(t, operator.NewPostgresStore(s.pool).Delete(context.Background(), s.hosted))
	for _, table := range []string{"operator_tokens", "agent_tokens"} {
		toHosted, toGaram := shown(table)
		assert.Zero(t, toHosted, "deleting the audience takes the tokens shown to it: %s", table)
		assert.Positive(t, toGaram, "control: garam's tokens stay: %s", table)
	}
}
