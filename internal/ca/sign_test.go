package ca_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/ca/mocks"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

func operatorGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, "operator", id)
}

// certificateRequest returns a PEM-encoded PKCS#10 request over a fresh
// key on the given curve, with template applied so a test can put
// something in it that must be ignored.
func certificateRequest(t *testing.T, curve elliptic.Curve, template *x509.CertificateRequest) ([]byte, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(curve, rand.Reader)
	require.NoError(t, err)
	if template == nil {
		template = &x509.CertificateRequest{}
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), key
}

// signForNewOrg signs a request through a service whose store holds no
// authority yet, and returns the certificate with the authority the
// service created on demand.
func signForNewOrg(t *testing.T, subject grn.GRN, request []byte) (*ca.Certificate, *ca.Authority) {
	t.Helper()
	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, orgOf(subject)).Return(nil, ca.ErrAuthorityNotFound).Once()
	var created *ca.Authority
	store.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, a *ca.Authority) error {
			created = a
			return nil
		}).Once()

	svc := newService(t, store, newSealer(t), ca.Config{})
	cert, err := svc.Sign(context.Background(), subject, request)
	require.NoError(t, err)
	require.NotNil(t, created)
	return cert, created
}

func TestService_SignCertifiesTheRequestedKeyForTheSubjectNamed(t *testing.T) {
	subject := operatorGRN("acme", "field-one")
	request, key := certificateRequest(t, elliptic.P256(), nil)

	cert, authority := signForNewOrg(t, subject, request)

	assert.Equal(t, subject, cert.Subject())
	require.NoError(t, verifyAgainst(t, cert, authority))

	// The certificate certifies the key the caller generated. Nothing
	// else could: garam never saw the private half.
	leaf := parseCertificatePEM(t, cert.CertificatePEM())
	public, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	require.True(t, ok)
	assert.Equal(t, key.PublicKey, *public)
}

// TestService_SignNamesTheSubjectItWasGivenAndNotTheOneRequested is
// the whole security property of the enrollment route: a request may
// ask for any identity it likes and gets the caller's.
func TestService_SignNamesTheSubjectItWasGivenAndNotTheOneRequested(t *testing.T) {
	subject := operatorGRN("acme", "field-one")
	impersonated := operatorGRN("acme", "field-two")
	asked, err := url.Parse(impersonated.String())
	require.NoError(t, err)
	request, _ := certificateRequest(t, elliptic.P256(), &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: impersonated.String()},
		URIs:    []*url.URL{asked},
	})

	cert, _ := signForNewOrg(t, subject, request)

	leaf := parseCertificatePEM(t, cert.CertificatePEM())
	require.Len(t, leaf.URIs, 1)
	assert.Equal(t, subject.String(), leaf.URIs[0].String())
	assert.Empty(t, leaf.Subject.CommonName)
}

func TestService_SignRefusesARequestThatProvesNothing(t *testing.T) {
	subject := operatorGRN("acme", "field-one")
	// The control: the same call with a well-formed request is
	// accepted, so what the cases below meet is the refusal under test
	// and not a service that signs nothing at all.
	accepted, _ := certificateRequest(t, elliptic.P256(), nil)
	_, authority := signForNewOrg(t, subject, accepted)

	tampered, _ := certificateRequest(t, elliptic.P256(), nil)
	block, _ := pem.Decode(tampered)
	require.NotNil(t, block)
	// Flipping a byte of the request's own signature is what makes it
	// prove nothing: the bytes still parse as a request.
	block.Bytes[len(block.Bytes)-1] ^= 0xff
	unsigned := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: block.Bytes})

	wrongCurve, _ := certificateRequest(t, elliptic.P384(), nil)
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: authority.Certificate})

	tests := map[string][]byte{
		"a request whose signature does not verify": unsigned,
		"a key on another curve":                    wrongCurve,
		"a certificate rather than a request":       certificatePEM,
		"bytes that are not pem at all":             []byte("field-one"),
		"nothing":                                   nil,
		"two requests where one was asked for":      append(append([]byte{}, accepted...), accepted...),
	}
	for name, request := range tests {
		t.Run(name, func(t *testing.T) {
			svc := newService(t, mocks.NewMockStore(t), newSealer(t), ca.Config{})
			_, err := svc.Sign(context.Background(), subject, request)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument), err)
		})
	}
}
