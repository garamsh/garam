package delivery_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/delivery"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// controlAnswering is the lifecycle authority answering one way to
// everything, over the TLS its address must use.
func controlAnswering(t *testing.T, answer http.HandlerFunc) *delivery.HTTPAuthority {
	t.Helper()
	server := httptest.NewTLSServer(answer)
	t.Cleanup(server.Close)
	authority, err := delivery.NewHTTPAuthority(server.Client(), server.URL, agentGRN(), certificateMode())
	require.NoError(t, err)
	return authority
}

// activationAnswer is what control answers the binding with, as
// agent-execution.v1 writes it.
func activationAnswer(b delivery.Binding) map[string]any {
	return map[string]any{
		"activationId": "activation-1", "tokenVersion": 2, "token": "the-token",
		"grn": b.Agent.String(), "epoch": b.Epoch, "generation": b.Generation, "configRevision": b.Revision,
	}
}

// TestHTTPAuthority_ActivateSendsTheContractAndVerifiesTheBinding
// covers the activation call as ADR-0084 pins it — the route, the
// version, the placement token and the body — and the answer's binding:
// one naming another agent, epoch, generation or revision, or naming no
// activation or token, is refused with its token unused. The answer
// naming what was asked is the control.
func TestHTTPAuthority_ActivateSendsTheContractAndVerifiesTheBinding(t *testing.T) {
	var route, version, authorization string
	var sent map[string]any
	authority := controlAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		route, version, authorization = r.URL.Path, r.Header.Get("Garam-Contract-Version"), r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		w.Header().Set("Garam-Contract-Version", delivery.ExecutionContract)
		writeJSON(w, http.StatusCreated, activationAnswer(binding()))
	})
	activation, err := authority.Activate(context.Background(), "request-1", binding(), "placement-1")
	require.NoError(t, err, "the control: an answer binding what was asked")
	assert.Equal(t, "/v1/agents/"+agentGRN().String()+"/activations", route)
	assert.Equal(t, "agent-execution.v1", version)
	assert.Equal(t, "Garam-Placement placement-1", authorization)
	assert.Equal(t, map[string]any{
		"requestId": "request-1", "epoch": "3", "generation": strings.Repeat("a", 32), "configRevision": "7",
	}, sent)
	assert.Equal(t, delivery.Activation{ID: "activation-1", TokenVersion: 2, Token: "the-token", Binding: binding()}, *activation)
	assert.Equal(t, "[redacted]", activation.Token.String(), "a token never renders as itself")

	wrong := map[string]func(m map[string]any){
		"another agent":      func(m map[string]any) { m["grn"] = senderGRN().String() },
		"another epoch":      func(m map[string]any) { m["epoch"] = "4" },
		"another generation": func(m map[string]any) { m["generation"] = strings.Repeat("b", 32) },
		"another revision":   func(m map[string]any) { m["configRevision"] = "8" },
		"no activation":      func(m map[string]any) { m["activationId"] = "" },
		"no token":           func(m map[string]any) { m["token"] = "" },
		"no token version":   func(m map[string]any) { m["tokenVersion"] = 0 },
	}
	for name, change := range wrong {
		t.Run(name, func(t *testing.T) {
			answer := activationAnswer(binding())
			change(answer)
			authority := controlAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Garam-Contract-Version", delivery.ExecutionContract)
				writeJSON(w, http.StatusOK, answer)
			})
			_, err := authority.Activate(context.Background(), "request-1", binding(), "placement-1")
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition), "a definite refusal: %v", err)
			assert.NotContains(t, err.Error(), "the-token", "the token unused stays unwritten")
		})
	}
}

// TestHTTPAuthority_SortsControlsAnswersIntoUndecidedAndDefinite holds
// the client to ADR-0084's two classes: a 5xx and a failed connection
// are undecided, and every 4xx and an answer outside the contract are
// definite, carrying control's kind for the log.
func TestHTTPAuthority_SortsControlsAnswersIntoUndecidedAndDefinite(t *testing.T) {
	answers := map[string]struct {
		status  int
		version string
		want    errorx.Kind
		said    string
	}{
		"503 undecided":             {http.StatusServiceUnavailable, delivery.ExecutionContract, errorx.KindUnavailable, "503"},
		"403 placement_not_current": {http.StatusForbidden, delivery.ExecutionContract, errorx.KindFailedPrecondition, "placement_not_current"},
		"409 epoch_superseded":      {http.StatusConflict, delivery.ExecutionContract, errorx.KindFailedPrecondition, "epoch_superseded"},
		"an answer of no contract":  {http.StatusCreated, "", errorx.KindUnimplemented, "201"},
		"an answer of another":      {http.StatusForbidden, "agent-execution.v2", errorx.KindUnimplemented, "403"},
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			authority := controlAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
				if answer.version != "" {
					w.Header().Set("Garam-Contract-Version", answer.version)
				}
				kind := strings.SplitN(name, " ", 2)[1]
				writeJSON(w, answer.status, map[string]string{"kind": kind, "message": "refused"})
			})
			_, err := authority.Activate(context.Background(), "request-1", binding(), "placement-1")
			assert.True(t, errorx.IsKind(err, answer.want), "%v", err)
			assert.Contains(t, err.Error(), answer.said)
		})
	}

	server := httptest.NewTLSServer(http.NotFoundHandler())
	unreachable, err := delivery.NewHTTPAuthority(server.Client(), server.URL, agentGRN(), certificateMode())
	require.NoError(t, err)
	server.Close()
	_, err = unreachable.Activate(context.Background(), "request-1", binding(), "placement-1")
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable), "a failed connection is undecided: %v", err)
}

// TestHTTPAuthority_ReportCarriesTheRuntimesStatusUnderItsActivation
// covers the report as ADR-0084 pins it: the activation, then the
// runtime's own fields unchanged, and when it was read.
func TestHTTPAuthority_ReportCarriesTheRuntimesStatusUnderItsActivation(t *testing.T) {
	var route, authorization string
	var sent map[string]any
	authority := controlAnswering(t, func(w http.ResponseWriter, r *http.Request) {
		route, authorization = r.URL.Path, r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&sent))
		w.Header().Set("Garam-Contract-Version", delivery.ExecutionContract)
		w.WriteHeader(http.StatusNoContent)
	})
	status := delivery.RuntimeStatus{
		Agent: agentGRN(), Epoch: "3", Generation: strings.Repeat("a", 32), Revision: "",
		State: delivery.RuntimeDraining, StartedAt: time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC),
	}
	observed := time.Date(2026, 10, 5, 1, 3, 0, 5, time.UTC)
	require.NoError(t, authority.Report(context.Background(), "activation-1", &status, observed))

	superseded := controlAnswering(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Garam-Contract-Version", delivery.ExecutionContract)
		writeJSON(w, http.StatusConflict, map[string]string{"kind": "generation_not_current", "message": "newer"})
	})
	err := superseded.Report(context.Background(), "activation-1", &status, observed)
	assert.ErrorIs(t, err, delivery.ErrGenerationSuperseded, "a 409 is the generation not being current")
	assert.Equal(t, "/v1/agents/"+agentGRN().String()+"/runtime-status", route)
	assert.Empty(t, authorization, "no token goes to control on a report")
	assert.Equal(t, map[string]any{
		"activationId": "activation-1", "grn": agentGRN().String(), "epoch": "3",
		"generation": strings.Repeat("a", 32), "configRevision": "", "state": "draining",
		"startedAt": "2026-10-05T01:02:03Z", "observedAt": "2026-10-05T01:03:00.000000005Z",
	}, sent, "an empty revision is sent as the runtime reported it: unknown")
}

// TestNewHTTPAuthority_RefusesWhatItCannotCall keeps a misconfiguration
// at the start. An https address is the control.
func TestNewHTTPAuthority_RefusesWhatItCannotCall(t *testing.T) {
	_, err := delivery.NewHTTPAuthority(http.DefaultClient, "https://control.example", agentGRN(), certificateMode())
	require.NoError(t, err, "the control: an https address and an agent")
	for name, args := range map[string]struct {
		client *http.Client
		base   string
		agent  grn.GRN
	}{
		"no client":            {nil, "https://control.example", agentGRN()},
		"a plain http address": {http.DefaultClient, "http://control.example", agentGRN()},
		"a relative address":   {http.DefaultClient, "/v1", agentGRN()},
		"no agent":             {http.DefaultClient, "https://control.example", grn.GRN{}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := delivery.NewHTTPAuthority(args.client, args.base, args.agent, certificateMode())
			require.Error(t, err)
		})
	}
}
