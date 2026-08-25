package machineapi_test

import (
	"context"
	"crypto/x509"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/operator"
)

// operatorService is an operator.Service recording the credential the
// surface read off the connection. Renewing is the whole of what this
// surface asks for; minting out of band is a command's path (ADR-0032)
// and reaches no handler, so it panics through the embedded interface.
type operatorService struct {
	operator.Service
	certificate *operator.Certificate
	err         error
	renewed     []operator.Credential
}

func (s *operatorService) Renew(_ context.Context, c operator.Credential) (*operator.Certificate, error) {
	s.renewed = append(s.renewed, c)
	if s.err != nil {
		return nil, s.err
	}
	return s.certificate, nil
}

// presentedBy returns the certificate the client authenticates with,
// which is what the surface has to read the renewal off.
func presentedBy(t *testing.T, client *http.Client) *x509.Certificate {
	t.Helper()
	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok)
	require.Len(t, transport.TLSClientConfig.Certificates, 1)
	leaf, err := x509.ParseCertificate(transport.TLSClientConfig.Certificates[0].Certificate[0])
	require.NoError(t, err)
	return leaf
}

// TestServer_RenewsTheCertificateTheConnectionAuthenticatedWith covers
// what the surface reads off the handshake and what comes back. The
// route names no subject, so what it passes on is the certificate
// presented — the identity it names, and which certificate it is.
func TestServer_RenewsTheCertificateTheConnectionAuthenticatedWith(t *testing.T) {
	holder := operatorGRN("acme")
	notAfter := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	operators := &operatorService{certificate: &operator.Certificate{
		Serial:         "successor",
		CertificatePEM: []byte("certificate"),
		PrivateKeyPEM:  []byte("private key"),
		IssuerPEM:      []byte("issuer"),
		ServerRootPEM:  []byte("server root"),
		NotAfter:       notAfter,
	}}
	url, client := listener(t, &agentService{}, operators, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, holder)

	status, body := do(t, client, http.MethodPost, url+"/identity/certificate")
	require.Equal(t, http.StatusCreated, status)
	assert.JSONEq(t, `{
		"grn": "`+holder.String()+`",
		"certificatePem": "certificate",
		"privateKeyPem": "private key",
		"issuerPem": "issuer",
		"serverRootPem": "server root",
		"notAfter": "2026-08-26T12:00:00Z"
	}`, body)

	presented := presentedBy(t, client)
	require.Len(t, operators.renewed, 1)
	assert.Equal(t, operator.Credential{
		Subject:   holder,
		Serial:    operator.Serial(presented.SerialNumber.String()),
		NotBefore: presented.NotBefore,
		NotAfter:  presented.NotAfter,
	}, operators.renewed[0], "the renewal names the certificate the connection presented")
}

// TestServer_RenewalRefusalsCarryTheirKind holds each refusal the rule
// makes against the answer above: a caller reads a renewal it may ask
// for again off a different kind from one only a mint out of band
// recovers, and neither is invented at this boundary.
func TestServer_RenewalRefusalsCarryTheirKind(t *testing.T) {
	holder := operatorGRN("acme")
	operators := &operatorService{}
	url, client := listener(t, &agentService{}, operators, &messageService{}, consenting(), unwatched(t), &memberGroups{}, &engine{}, holder)

	refusals := map[string]struct {
		err    error
		status int
		kind   string
	}{
		"too early":       {operator.ErrRenewalTooEarly, http.StatusConflict, "failed_precondition"},
		"superseded":      {operator.ErrSuperseded, http.StatusConflict, "already_exists"},
		"expired":         {operator.ErrCertificateExpired, http.StatusForbidden, "permission_denied"},
		"not an operator": {operator.ErrNotAnOperator, http.StatusForbidden, "permission_denied"},
	}
	for name, refusal := range refusals {
		t.Run(name, func(t *testing.T) {
			operators.err = refusal.err
			status, body := do(t, client, http.MethodPost, url+"/identity/certificate")
			assert.Equal(t, refusal.status, status)
			assert.JSONEq(t, `{"kind":"`+refusal.kind+`","message":"`+refusal.err.Error()+`"}`, body)
		})
	}
}
