package machineapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/platform/internal/errorx"
	"github.com/garamsh/garam/platform/internal/grn"
	"github.com/garamsh/garam/platform/internal/machine"
	"github.com/garamsh/garam/platform/internal/machineapi"
	"github.com/garamsh/garam/platform/internal/operator"
)

// recheck is machineapi.Rechecker in memory: it records whom it was
// asked about and answers the error it holds.
type recheck struct {
	err      error
	rechecks []grn.GRN
}

func (c *recheck) Recheck(_ context.Context, identity machine.Identity) error {
	c.rechecks = append(c.rechecks, identity.GRN())
	return c.err
}

// shown is operator.Service as the proof asks it: whether a forwarded
// token proves the controller to the caller. It records what it was
// asked and answers the error it holds.
type shown struct {
	operator.Service
	err      error
	subject  grn.GRN
	audience grn.GRN
	token    string
}

func (s *shown) VerifyShown(_ context.Context, subject, audience grn.GRN, token string) error {
	s.subject, s.audience, s.token = subject, audience, token
	return s.err
}

// proofs is operator.ProofService in memory.
type proofs struct {
	proof    *operator.ControllerProof
	err      error
	asked    int
	audience grn.GRN
	agent    *grn.GRN
}

func (p *proofs) Prove(_ context.Context, audience, _ grn.GRN, agent *grn.GRN) (*operator.ControllerProof, error) {
	p.asked++
	p.audience, p.agent = audience, agent
	return p.proof, p.err
}

// proofRoute is the route for the controller of acme the tests prove.
const proofRoute = "/operators/grn:acme:default:operator:controller/introspection"

func proofListener(t *testing.T, c *recheck, v *shown, p *proofs) (string, *http.Client) {
	t.Helper()
	return serveTLS(t, machineapi.NewServer(nil, v, nil, nil, nil, nil, nil, nil, c, p, nil, nil, nil, nil),
		grn.New("acme", "default", "operator", "hosted"))
}

// forwardedToken is a controller's token in the form a hosted operator
// forwards. What it names is not read here: verifying it is shown's.
const forwardedToken = "garam_ot_" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func proofBody(t *testing.T, token string, agent string) string {
	t.Helper()
	body := map[string]any{"token": token}
	if agent != "" {
		body["agent"] = agent
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return string(raw)
}

// TestIntrospectController_ProvesWhatTheForwardedTokenNames holds that
// the audience is the caller's, the controller is the path's and is
// verified against the token forwarded, and the answer is the proof the
// service gave, with agent null where none was named.
func TestIntrospectController_ProvesWhatTheForwardedTokenNames(t *testing.T) {
	controller := grn.New("acme", "default", "operator", "controller")
	hosted := grn.New("acme", "default", "operator", "hosted")
	org := grn.New("root", "default", "org", "acme")
	agent := agentGRN("acme", "0a1b2c3d4e5f6071")
	c, v := &recheck{}, &shown{}
	p := &proofs{proof: &operator.ControllerProof{Controller: controller, Org: org,
		Agent: &operator.ProvenAgent{Agent: agent, Epoch: "7"}}}
	url, client := proofListener(t, c, v, p)

	status, version, body := versioned(t, client, http.MethodPost, url+proofRoute, operator.TokenAuthorityContractVersion,
		proofBody(t, forwardedToken, agent.String()))
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, operator.TokenAuthorityContractVersion, version)
	assert.JSONEq(t, `{"operator":"grn:acme:default:operator:controller","org":"grn:root:default:org:acme",
		"agent":{"grn":"grn:acme:default:agent:0a1b2c3d4e5f6071","epoch":"7"}}`, body)
	assert.Equal(t, hosted, p.audience)
	require.NotNil(t, p.agent)
	assert.Equal(t, agent, *p.agent)
	assert.Equal(t, controller, v.subject)
	assert.Equal(t, hosted, v.audience)
	assert.Equal(t, forwardedToken, v.token, "the token verified is the one forwarded")
	assert.Equal(t, []grn.GRN{hosted}, c.rechecks)
	assert.NotContains(t, body, forwardedToken)

	p.proof = &operator.ControllerProof{Controller: controller, Org: org}
	status, _, body = versioned(t, client, http.MethodPost, url+proofRoute, operator.TokenAuthorityContractVersion,
		proofBody(t, forwardedToken, ""))
	require.Equal(t, http.StatusOK, status, body)
	assert.JSONEq(t, `{"operator":"grn:acme:default:operator:controller","org":"grn:root:default:org:acme","agent":null}`, body)
	assert.Nil(t, p.agent)
}

// sessionProof is what the doubles answer a request that passes.
func sessionProof() *operator.ControllerProof {
	return &operator.ControllerProof{Controller: grn.New("acme", "default", "operator", "controller"),
		Org: grn.New("root", "default", "org", "acme")}
}

// TestIntrospectController_RefusesMalformedInputUnread holds that what
// does not parse is refused before either the caller's state or the
// controller's is read. Each case sends the accepted request through
// the same listener first, which reads both and is proved, and then the
// request with the one malformed part.
func TestIntrospectController_RefusesMalformedInputUnread(t *testing.T) {
	accepted := proofBody(t, forwardedToken, agentGRN("acme", "0a1b2c3d4e5f6071").String())
	v2 := operator.TokenAuthorityContractVersion
	tests := []struct {
		name, path, version, body string
		status                    int
	}{
		{"no contract version", proofRoute, "", accepted, http.StatusBadRequest},
		{"the retired certificate value", proofRoute, "operation-authority.v1", accepted, http.StatusBadRequest},
		{"a body that is not JSON", proofRoute, v2, "token", http.StatusBadRequest},
		{"no token", proofRoute, v2, `{}`, http.StatusBadRequest},
		{"an agent that is not a GRN", proofRoute, v2, proofBody(t, forwardedToken, "bot"), http.StatusBadRequest},
		{"a controller that is not a GRN", "/operators/controller/introspection", v2, accepted, http.StatusBadRequest},
		{"another organization's controller", "/operators/grn:globex:default:operator:controller/introspection",
			v2, accepted, http.StatusNotFound},
		{"an agent named as the controller", "/operators/grn:acme:default:agent:bot/introspection",
			v2, accepted, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, v, p := &recheck{}, &shown{}, &proofs{proof: sessionProof()}
			url, client := proofListener(t, c, v, p)
			status, _, body := versioned(t, client, http.MethodPost, url+proofRoute, v2, accepted)
			require.Equal(t, http.StatusOK, status, "the control: %s", body)
			require.Len(t, c.rechecks, 1)
			require.NotEmpty(t, v.token)
			require.Equal(t, 1, p.asked)

			*c, *v, *p = recheck{}, shown{}, proofs{proof: sessionProof()}
			status, version, body := versioned(t, client, http.MethodPost, url+tt.path, tt.version, tt.body)
			assert.Equal(t, tt.status, status, body)
			assert.Equal(t, v2, version)
			assert.Empty(t, c.rechecks, "the caller is not read again")
			assert.Empty(t, v.token, "no token is verified")
			assert.Zero(t, p.asked, "nothing is proved")
		})
	}
}

// TestIntrospectController_MapsEachRefusal holds the agreed statuses:
// the caller's own withdrawn registration and expired token are 403, a
// forwarded token that does not prove the controller is 422, and
// authority that cannot be read 503 naming no cause. Each case is first
// answered 200 by the same listener with nothing refusing, and then
// with the one refusal set.
func TestIntrospectController_MapsEachRefusal(t *testing.T) {
	unavailable := errorx.Wrap(errorx.New(errorx.KindInternal, "connection refused"), "registration", errorx.KindUnavailable)
	tests := []struct {
		name            string
		recheck, verify error
		prove           error
		status          int
	}{
		{"the caller's registration withdrawn", errorx.New(errorx.KindNotFound, "no record"), nil, nil, http.StatusForbidden},
		{"the caller's token expired", machine.ErrOutsideValidity, nil, nil, http.StatusForbidden},
		{"the caller's registration unreadable", unavailable, nil, nil, http.StatusServiceUnavailable},
		{"a token that does not prove the controller", nil, operator.ErrTokenNotProven, nil, http.StatusUnprocessableEntity},
		{"the controller's token unreadable", nil, unavailable, nil, http.StatusServiceUnavailable},
		{"no covering grant", nil, nil, operator.ErrControllerNotDelegated, http.StatusForbidden},
		{"an unknown agent", nil, nil, operator.ErrAgentNotFound, http.StatusNotFound},
		{"an agent assigned elsewhere", nil, nil, operator.ErrAgentElsewhere, http.StatusForbidden},
		{"policy that cannot be decided", nil, nil, unavailable, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, v, p := &recheck{}, &shown{}, &proofs{proof: sessionProof()}
			url, client := proofListener(t, c, v, p)
			request := proofBody(t, forwardedToken, "")
			v2 := operator.TokenAuthorityContractVersion
			status, _, body := versioned(t, client, http.MethodPost, url+proofRoute, v2, request)
			require.Equal(t, http.StatusOK, status, "the control: %s", body)

			c.err, v.err = tt.recheck, tt.verify
			if tt.prove != nil {
				*p = proofs{err: tt.prove}
			}
			status, version, body := versioned(t, client, http.MethodPost, url+proofRoute, v2, request)
			assert.Equal(t, tt.status, status, body)
			assert.Equal(t, v2, version)
			if tt.status == http.StatusServiceUnavailable {
				assert.Contains(t, body, `"message":"internal error"`)
			}
		})
	}
}
