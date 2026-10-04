package machineapi_test

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
	"github.com/garamsh/garam/internal/machineapi"
	"github.com/garamsh/garam/internal/operator"
)

// certificateCheck is machine.Service as a decision asks it: the
// caller's identity again, and a forwarded certificate for a subject.
// It records what it was asked and answers the error it holds; the
// handshake is the listener's own service and not this one.
type certificateCheck struct {
	machine.Service
	recheck  error
	verify   error
	rechecks []grn.GRN
	subject  grn.GRN
	leaf     *x509.Certificate
}

func (c *certificateCheck) Recheck(_ context.Context, identity machine.Identity) error {
	c.rechecks = append(c.rechecks, identity.GRN())
	return c.recheck
}

func (c *certificateCheck) Verify(_ context.Context, subject grn.GRN, leaf *x509.Certificate) error {
	c.subject, c.leaf = subject, leaf
	return c.verify
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

func proofListener(t *testing.T, certificates *certificateCheck, p *proofs) (string, *http.Client) {
	t.Helper()
	return serveTLS(t, machineapi.NewServer(nil, nil, nil, nil, nil, nil, nil, nil, certificates, p),
		grn.New("acme", "default", "operator", "hosted"))
}

// forwarded is a certificate in the PEM form a hosted operator forwards.
// What it names is not read here: verifying it is certificateCheck's.
func forwarded(t *testing.T) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: loopback(t).Certificate[0]}))
}

func proofBody(t *testing.T, certificatePEM string, agent string) string {
	t.Helper()
	body := map[string]any{"certificatePem": certificatePEM}
	if agent != "" {
		body["agent"] = agent
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return string(raw)
}

// TestIntrospectController_ProvesWhatTheForwardedCertificateNames holds
// that the audience is the handshake's, the controller is the path's
// and is verified against the certificate forwarded, and the answer is
// the proof the service gave, with agent null where none was named.
func TestIntrospectController_ProvesWhatTheForwardedCertificateNames(t *testing.T) {
	controller := grn.New("acme", "default", "operator", "controller")
	org := grn.New("root", "default", "org", "acme")
	agent := agentGRN("acme", "0a1b2c3d4e5f6071")
	certificates := &certificateCheck{}
	p := &proofs{proof: &operator.ControllerProof{Controller: controller, Org: org,
		Agent: &operator.ProvenAgent{Agent: agent, Epoch: "7"}}}
	url, client := proofListener(t, certificates, p)
	pemText := forwarded(t)

	status, version, body := versioned(t, client, http.MethodPost, url+proofRoute, operator.ContractVersion,
		proofBody(t, pemText, agent.String()))
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, operator.ContractVersion, version)
	assert.JSONEq(t, `{"operator":"grn:acme:default:operator:controller","org":"grn:root:default:org:acme",
		"agent":{"grn":"grn:acme:default:agent:0a1b2c3d4e5f6071","epoch":"7"}}`, body)
	assert.Equal(t, grn.New("acme", "default", "operator", "hosted"), p.audience)
	require.NotNil(t, p.agent)
	assert.Equal(t, agent, *p.agent)
	assert.Equal(t, controller, certificates.subject)
	block, _ := pem.Decode([]byte(pemText))
	assert.Equal(t, block.Bytes, certificates.leaf.Raw, "the certificate verified is the one forwarded")
	assert.Equal(t, []grn.GRN{grn.New("acme", "default", "operator", "hosted")}, certificates.rechecks)
	assert.NotContains(t, body, "BEGIN CERTIFICATE")

	p.proof = &operator.ControllerProof{Controller: controller, Org: org}
	status, _, body = versioned(t, client, http.MethodPost, url+proofRoute, operator.ContractVersion,
		proofBody(t, pemText, ""))
	require.Equal(t, http.StatusOK, status, body)
	assert.JSONEq(t, `{"operator":"grn:acme:default:operator:controller","org":"grn:root:default:org:acme","agent":null}`, body)
	assert.Nil(t, p.agent)
}

// TestIntrospectController_RefusesMalformedInputUnread holds that what
// does not parse is refused before either the caller's state or the
// controller's is read. The test above is the control: the same
// listener reads both for a request that parses.
func TestIntrospectController_RefusesMalformedInputUnread(t *testing.T) {
	pemText := forwarded(t)
	key := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not a key")}))
	tests := []struct {
		name, path, version, body string
		status                    int
	}{
		{"no contract version", proofRoute, "", proofBody(t, pemText, ""), http.StatusBadRequest},
		{"a body that is not JSON", proofRoute, operator.ContractVersion, "certificate", http.StatusBadRequest},
		{"no certificate", proofRoute, operator.ContractVersion, `{}`, http.StatusBadRequest},
		{"a private key in place of a certificate", proofRoute, operator.ContractVersion, proofBody(t, key, ""), http.StatusBadRequest},
		{"two certificates", proofRoute, operator.ContractVersion, proofBody(t, pemText+pemText, ""), http.StatusBadRequest},
		{"a certificate block that does not parse", proofRoute, operator.ContractVersion,
			proofBody(t, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("x")})), ""), http.StatusBadRequest},
		{"an agent that is not a GRN", proofRoute, operator.ContractVersion, proofBody(t, pemText, "bot"), http.StatusBadRequest},
		{"a controller that is not a GRN", "/operators/controller/introspection", operator.ContractVersion,
			proofBody(t, pemText, ""), http.StatusBadRequest},
		{"another organization's controller", "/operators/grn:globex:default:operator:controller/introspection",
			operator.ContractVersion, proofBody(t, pemText, ""), http.StatusNotFound},
		{"an agent named as the controller", "/operators/grn:acme:default:agent:bot/introspection",
			operator.ContractVersion, proofBody(t, pemText, ""), http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certificates, p := &certificateCheck{}, &proofs{}
			url, client := proofListener(t, certificates, p)
			status, version, body := versioned(t, client, http.MethodPost, url+tt.path, tt.version, tt.body)
			assert.Equal(t, tt.status, status, body)
			assert.Equal(t, operator.ContractVersion, version)
			assert.Empty(t, certificates.rechecks, "the caller is not read again")
			assert.Nil(t, certificates.leaf, "no certificate is verified")
			assert.Zero(t, p.asked, "nothing is proved")
		})
	}
}

// TestIntrospectController_MapsEachRefusal holds the agreed statuses:
// the caller's own withdrawn registration and expired certificate are
// 403, a forwarded certificate that does not prove the controller is
// 422, a withdrawn controller 404, and authority that cannot be read
// 503 naming no cause.
func TestIntrospectController_MapsEachRefusal(t *testing.T) {
	unavailable := errorx.Wrap(errorx.New(errorx.KindInternal, "connection refused"), "registration", errorx.KindUnavailable)
	tests := []struct {
		name            string
		recheck, verify error
		prove           error
		status          int
	}{
		{"the caller's registration withdrawn", errorx.New(errorx.KindNotFound, "no record"), nil, nil, http.StatusForbidden},
		{"the caller's certificate expired", machine.ErrOutsideValidity, nil, nil, http.StatusForbidden},
		{"the caller's registration unreadable", unavailable, nil, nil, http.StatusServiceUnavailable},
		{"a certificate that does not prove the controller", nil, machine.ErrNotProven, nil, http.StatusUnprocessableEntity},
		{"the controller's certificate expired", nil, machine.ErrOutsideValidity, nil, http.StatusForbidden},
		{"the controller's registration withdrawn", nil, errorx.New(errorx.KindNotFound, "no record"), nil, http.StatusNotFound},
		{"the controller's registration unreadable", nil, unavailable, nil, http.StatusServiceUnavailable},
		{"no covering grant", nil, nil, operator.ErrControllerNotDelegated, http.StatusForbidden},
		{"an unknown agent", nil, nil, operator.ErrAgentNotFound, http.StatusNotFound},
		{"an agent assigned elsewhere", nil, nil, operator.ErrAgentElsewhere, http.StatusForbidden},
		{"policy that cannot be decided", nil, nil, unavailable, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			certificates := &certificateCheck{recheck: tt.recheck, verify: tt.verify}
			url, client := proofListener(t, certificates, &proofs{err: tt.prove})
			status, version, body := versioned(t, client, http.MethodPost, url+proofRoute, operator.ContractVersion,
				proofBody(t, forwarded(t), ""))
			assert.Equal(t, tt.status, status, body)
			assert.Equal(t, operator.ContractVersion, version)
			if tt.status == http.StatusServiceUnavailable {
				assert.Contains(t, body, `"message":"internal error"`)
			}
		})
	}
}
