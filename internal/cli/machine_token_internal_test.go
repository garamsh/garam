package cli

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/agent"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/operator"
)

// liveTokens is the operator TokenStore's read half: the one token it
// answers is liveToken, for liveOperator, and audiencedToken for the
// same operator minted for a hosted one.
type liveTokens struct {
	operator.TokenStore
}

var (
	liveOperator   = grn.New("acme", grn.DefaultProject, operator.ServiceType, "one")
	liveToken      = operator.TokenPrefix + strings.Repeat("a", 64)
	audiencedToken = operator.TokenPrefix + strings.Repeat("b", 64)
)

func (liveTokens) ResolveToken(_ context.Context, hash string) (operator.TokenHolder, error) {
	switch hash {
	case operator.HashToken(liveToken):
		return operator.TokenHolder{Operator: liveOperator, ExpiresAt: time.Now().Add(time.Hour)}, nil
	case operator.HashToken(audiencedToken):
		hosted := grn.New("acme", grn.DefaultProject, operator.ServiceType, "control")
		return operator.TokenHolder{Operator: liveOperator, Audience: &hosted, ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	return operator.TokenHolder{}, operator.ErrTokenInvalid
}

// liveAgentTokens is the agent AccessTokenStore's read half: the one
// token it answers is liveAgentToken, for liveAgent.
type liveAgentTokens struct{ agent.AccessTokenStore }

var (
	liveAgent       = grn.New("acme", grn.DefaultProject, agent.ServiceType, "0a1b2c3d4e5f6071")
	liveAgentToken  = agent.AccessTokenPrefix + strings.Repeat("d", 64)
	liveAgentSerial = "t" + strings.Repeat("e", 32)
)

func (liveAgentTokens) ResolveAccessToken(_ context.Context, hash string) (agent.AccessTokenHolder, error) {
	if hash != agent.HashAccessToken(liveAgentToken) {
		return agent.AccessTokenHolder{}, agent.ErrAccessTokenInvalid
	}
	return agent.AccessTokenHolder{Agent: liveAgent, Serial: liveAgentSerial,
		IssuedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}, nil
}

// TestIdentityTokens_AnswersAGaramTokenAndNothingElse covers the seam's
// adapter: a live token minted for garam is the operator it names, and
// one minted for a hosted operator, one with another prefix and one no
// row stands for are each refused as unauthenticated.
func TestIdentityTokens_AnswersAGaramTokenAndNothingElse(t *testing.T) {
	// Internal: NewMachineServer resolves a live token in Postgres.
	tokens := identityTokens{operators: liveTokens{}, agents: liveAgentTokens{}}

	bearer, err := tokens.Resolve(context.Background(), liveToken)
	require.NoError(t, err)
	assert.Equal(t, liveOperator, bearer.Subject)
	assert.Empty(t, bearer.Serial, "an operator's token carries no serial")

	bearer, err = tokens.Resolve(context.Background(), liveAgentToken)
	require.NoError(t, err)
	assert.Equal(t, liveAgent, bearer.Subject)
	assert.Equal(t, liveAgentSerial, bearer.Serial, "an agent's token carries the serial the fence reads")

	for name, token := range map[string]string{
		"another audience":        audiencedToken,
		"no live row":             operator.TokenPrefix + strings.Repeat("c", 64),
		"an agent's, no live row": agent.AccessTokenPrefix + strings.Repeat("c", 64),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tokens.Resolve(context.Background(), token)
			assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), "%v", err)
		})
	}
}

// TestCountMachineRequests_CountsEachRequestByHowItAuthenticated drives
// the machine listener with a certificate, a token and neither, and
// reads the counter the probe port's /metrics serves. Each method is
// counted once for its own request, so a counter that labelled every
// request alike would fail two of the three.
func TestCountMachineRequests_CountsEachRequestByHowItAuthenticated(t *testing.T) {
	// Internal: a request is counted once Postgres authenticated it.
	var out lines
	obs := recordingObservability(&out)
	counter, err := newMachineRequestCounter(obs.registry)
	require.NoError(t, err)
	subject := grn.New("acme", grn.DefaultProject, "agent", "bot-1")
	url, certified := machineListenerAround(t, obs, subject,
		countMachineRequests(counter)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})))

	request := func(client *http.Client, token string) {
		req, err := http.NewRequest(http.MethodGet, url+"/identity", nil)
		require.NoError(t, err)
		if token != "" {
			req.Header.Set("Authorization", "Garam-Token "+token)
		}
		resp, err := client.Do(req)
		require.NoError(t, err)
		_ = resp.Body.Close()
	}
	// What garam presents is not under test, and this client presents
	// no certificate.
	anonymous := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	}}}
	t.Cleanup(anonymous.CloseIdleConnections)

	request(certified, "")
	request(anonymous, liveToken)
	request(anonymous, liveToken)
	request(anonymous, "")

	rec := httptest.NewRecorder()
	obs.machineProbeHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	scraped := rec.Body.String()
	assert.Contains(t, scraped, machineRequestsMetric+`{authentication="certificate"} 1`)
	assert.Contains(t, scraped, machineRequestsMetric+`{authentication="token"} 2`)
	assert.Contains(t, scraped, machineRequestsMetric+`{authentication="none"} 1`)
}

// newMachineRequestCounter must refuse a second registration rather
// than serve two series under one name.
func TestNewMachineRequestCounter_RegistersOnce(t *testing.T) {
	// Internal: NewMachineServer's second counter masks this one's.
	registry := prometheus.NewRegistry()
	_, err := newMachineRequestCounter(registry)
	require.NoError(t, err)
	_, err = newMachineRequestCounter(registry)
	assert.Error(t, err)
	_, err = newMachineRequestCounter(nil)
	assert.NoError(t, err, "no registerer registers nowhere")
}

// TestCountTokenExchanges_TellsTheCohortsApartByExchangeMethod holds
// the cohort evidence ADR-0102 asks for: an exchange is counted by how
// it authenticated — a shared-secret supervisor, a private_key_jwt
// operator, or a credential neither, which an operator's retired
// shared secret now is (ADR-0117) — and by how it ended, a route that
// exchanges nothing is not counted, and no label carries what the
// caller sent beyond its credential's prefix.
func TestCountTokenExchanges_TellsTheCohortsApartByExchangeMethod(t *testing.T) {
	// Internal: an exchange is answered only from Postgres.
	registry := prometheus.NewRegistry()
	counter, err := newTokenExchangeCounter(registry)
	require.NoError(t, err)
	status := map[string]int{}
	answer := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status[r.Header.Get("X-Case")]) }
	mux := http.NewServeMux()
	for _, pattern := range []string{"POST /tokens", "POST /oauth/token", "GET /identity"} {
		mux.Handle(pattern, countTokenExchanges(counter)(http.HandlerFunc(answer)))
	}
	operatorSecret := operator.CredentialScheme + " garam_oc_" + strings.Repeat("a", 64)
	supervisorSecret := operator.CredentialScheme + " " + agent.SupervisorCredentialPrefix + strings.Repeat("b", 64)
	send := func(method, path, authorization string, answered int) {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", authorization)
		c := method + path + authorization
		status[c] = answered
		req.Header.Set("X-Case", c)
		mux.ServeHTTP(httptest.NewRecorder(), req)
	}

	send(http.MethodPost, "/tokens", operatorSecret, http.StatusCreated)
	send(http.MethodPost, "/tokens", supervisorSecret, http.StatusUnauthorized)
	send(http.MethodPost, "/oauth/token", "", http.StatusOK)
	send(http.MethodPost, "/oauth/token", "", http.StatusOK)
	send(http.MethodPost, "/oauth/token", "x", http.StatusTooManyRequests)
	send(http.MethodPost, "/oauth/token", "y", http.StatusServiceUnavailable)
	send(http.MethodGet, "/identity", "Garam-Token "+liveToken, http.StatusOK)

	rec := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	scraped := rec.Body.String()
	assert.Contains(t, scraped, tokenExchangesMetric+`{method="unrecognised",outcome="issued"} 1`)
	assert.NotContains(t, scraped, "operator_shared_secret")
	assert.Contains(t, scraped, tokenExchangesMetric+`{method="supervisor_shared_secret",outcome="refused"} 1`)
	assert.Contains(t, scraped, tokenExchangesMetric+`{method="operator_private_key_jwt",outcome="issued"} 2`)
	assert.Contains(t, scraped, tokenExchangesMetric+`{method="operator_private_key_jwt",outcome="limited"} 1`)
	assert.Contains(t, scraped, tokenExchangesMetric+`{method="operator_private_key_jwt",outcome="failed"} 1`)
	assert.NotContains(t, scraped, strings.Repeat("a", 64), "no credential value reaches a label")
	assert.NotContains(t, scraped, liveToken)
}

// heldSightings is the evidence Recorder in memory.
type heldSightings struct{ written []machine.Sighting }

func (h *heldSightings) Record(_ context.Context, s machine.Sighting) error {
	h.written = append(h.written, s)
	return nil
}

// TestRecordIssuances_NotesEachIssuanceUnderItsPath holds ADR-0109's
// second recording point: a token an exchange or a mint issued is
// noted under the path its route and credential name, keyed by the
// subject the handler reported and stamped with the token's expiry, and
// a token naming an audience is noted on the audience path as well. A
// refused exchange reports nothing and is noted nowhere, which the
// issued one beside it on the same route is the control for; a route
// that issues nothing is noted nowhere either, and neither is a
// credential no exchange recognises, an operator's retired shared
// secret among them (ADR-0117).
func TestRecordIssuances_NotesEachIssuanceUnderItsPath(t *testing.T) {
	// Internal: an issuance is answered only from Postgres.
	recorder := &heldSightings{}
	evidence := machine.NewEvidence(recorder, time.Minute, nil)
	until := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	operatorGRN := grn.New("acme", grn.DefaultProject, "operator", "manager")
	supervisorGRN := grn.New("acme", grn.DefaultProject, "supervisor", "host-1")
	agentGRN := grn.New("acme", grn.DefaultProject, "agent", "bot-1")
	reports := map[string]*machine.Issuance{}
	answer := func(w http.ResponseWriter, r *http.Request) {
		if issued := reports[r.Header.Get("X-Case")]; issued != nil {
			machine.ReportIssuance(r, *issued)
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}
	mux := http.NewServeMux()
	for _, pattern := range []string{"POST /tokens", "POST /oauth/token", "POST /agents/{agent}/tokens", "GET /identity"} {
		mux.Handle(pattern, recordIssuances(evidence)(http.HandlerFunc(answer)))
	}
	operatorSecret := operator.CredentialScheme + " garam_oc_" + strings.Repeat("a", 64)
	supervisorSecret := operator.CredentialScheme + " " + agent.SupervisorCredentialPrefix + strings.Repeat("b", 64)
	send := func(c, method, path, authorization string, issued *machine.Issuance) {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", authorization)
		req.Header.Set("X-Case", c)
		reports[c] = issued
		mux.ServeHTTP(httptest.NewRecorder(), req)
	}

	send("refused secret", http.MethodPost, "/tokens", supervisorSecret, nil)
	send("operator secret", http.MethodPost, "/tokens", operatorSecret,
		&machine.Issuance{Subject: operatorGRN, Until: until})
	send("supervisor secret", http.MethodPost, "/tokens", supervisorSecret,
		&machine.Issuance{Subject: supervisorGRN, Until: until})
	send("assertion", http.MethodPost, "/oauth/token", "", &machine.Issuance{Subject: operatorGRN, Audience: true, Until: until})
	send("mint", http.MethodPost, "/agents/"+agentGRN.String()+"/tokens", "Garam-Token "+liveToken,
		&machine.Issuance{Subject: agentGRN, Until: until})
	send("identity", http.MethodGet, "/identity", "Garam-Token "+liveToken, &machine.Issuance{Subject: agentGRN, Until: until})

	assert.Equal(t, []machine.Sighting{
		{Caller: supervisorGRN, Path: machine.PathSharedSecret, IssuedUntil: until},
		{Caller: operatorGRN, Path: machine.PathPrivateKeyJWT, IssuedUntil: until},
		{Caller: operatorGRN, Path: machine.PathAudienceToken, IssuedUntil: until},
		{Caller: agentGRN, Path: machine.PathAgentMint, IssuedUntil: until},
	}, recorder.written)
}
