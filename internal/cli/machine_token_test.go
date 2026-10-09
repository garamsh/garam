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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/operator"
)

// liveTokens is the operator TokenStore's read half: the one token it
// answers is liveToken, for liveOperator, and audiencedToken for the
// same operator minted for a hosted one.
type liveTokens struct {
	operator.TokenStore
	// reads counts the lookups made, where a test asks.
	reads *int
}

var (
	liveOperator   = grn.New("acme", grn.DefaultProject, operator.ServiceType, "one")
	liveToken      = operator.TokenPrefix + strings.Repeat("a", 64)
	audiencedToken = operator.TokenPrefix + strings.Repeat("b", 64)
)

func (k liveTokens) ResolveToken(_ context.Context, hash string) (operator.TokenHolder, error) {
	if k.reads != nil {
		*k.reads++
	}
	switch hash {
	case operator.HashToken(liveToken):
		return operator.TokenHolder{Operator: liveOperator, ExpiresAt: time.Now().Add(time.Hour)}, nil
	case operator.HashToken(audiencedToken):
		hosted := grn.New("acme", grn.DefaultProject, operator.ServiceType, "control")
		return operator.TokenHolder{Operator: liveOperator, Audience: &hosted, ExpiresAt: time.Now().Add(time.Hour)}, nil
	}
	return operator.TokenHolder{}, operator.ErrTokenInvalid
}

// TestIdentityTokens_AnswersAGaramTokenAndNothingElse covers the seam's
// adapter: a live token minted for garam is the operator it names, and
// one minted for a hosted operator, one with another prefix and one no
// row stands for are each refused as unauthenticated.
func TestIdentityTokens_AnswersAGaramTokenAndNothingElse(t *testing.T) {
	tokens := identityTokens{operators: liveTokens{}}

	subject, _, err := tokens.Resolve(context.Background(), liveToken)
	require.NoError(t, err)
	assert.Equal(t, liveOperator, subject)

	for name, token := range map[string]string{
		"another audience": audiencedToken,
		"no live row":      operator.TokenPrefix + strings.Repeat("c", 64),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := tokens.Resolve(context.Background(), token)
			assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), "%v", err)
		})
	}
}

// TestIdentityTokens_RefusesAMalformedTokenWithoutAReadForIt holds that
// a value no issuance could have produced costs the store nothing: each
// malformed value is refused with no lookup, and the well-formed one
// beside them is looked up, so what spared the store is the format.
func TestIdentityTokens_RefusesAMalformedTokenWithoutAReadForIt(t *testing.T) {
	var reads int
	tokens := identityTokens{operators: liveTokens{reads: &reads}}

	for name, token := range map[string]string{
		"another prefix": "garam_at_" + strings.Repeat("a", 64),
		"too short":      operator.TokenPrefix + strings.Repeat("a", 63),
		"uppercase hex":  operator.TokenPrefix + strings.Repeat("A", 64),
		"not hex":        operator.TokenPrefix + strings.Repeat("g", 64),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := tokens.Resolve(context.Background(), token)
			assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated), "%v", err)
		})
	}
	assert.Equal(t, 0, reads, "no malformed value was looked up")

	_, _, err := tokens.Resolve(context.Background(), liveToken)
	require.NoError(t, err)
	assert.Equal(t, 1, reads, "the well-formed token was")
}

// TestCountMachineRequests_CountsEachRequestByHowItAuthenticated drives
// the machine listener with a certificate, a token and neither, and
// reads the counter the probe port's /metrics serves. Each method is
// counted once for its own request, so a counter that labelled every
// request alike would fail two of the three.
func TestCountMachineRequests_CountsEachRequestByHowItAuthenticated(t *testing.T) {
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
	registry := prometheus.NewRegistry()
	_, err := newMachineRequestCounter(registry)
	require.NoError(t, err)
	_, err = newMachineRequestCounter(registry)
	assert.Error(t, err)
	_, err = newMachineRequestCounter(nil)
	assert.NoError(t, err, "no registerer registers nowhere")
}
