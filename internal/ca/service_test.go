package ca_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/ca/mocks"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

func agentGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, "agent", id)
}

func orgOf(subject grn.GRN) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "org", subject.Organization)
}

func newSealer(t *testing.T) ca.Sealer {
	t.Helper()
	sealer, err := ca.NewAESGCMSealer(randomKey(t))
	require.NoError(t, err)
	return sealer
}

// issueForNewOrg issues through a service whose store holds no
// authority yet, and returns the certificate with the authority the
// service created on demand.
func issueForNewOrg(t *testing.T, subject grn.GRN, sealer ca.Sealer, cfg ca.Config) (*ca.Certificate, *ca.Authority) {
	t.Helper()
	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, orgOf(subject)).Return(nil, ca.ErrAuthorityNotFound).Once()
	var created *ca.Authority
	store.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, a *ca.Authority) error {
			created = a
			return nil
		}).Once()

	cert, err := ca.NewService(store, sealer, cfg).Issue(context.Background(), subject)
	require.NoError(t, err)
	require.NotNil(t, created)
	return cert, created
}

// serviceHolding returns a service whose store already holds authority.
func serviceHolding(t *testing.T, authority *ca.Authority, sealer ca.Sealer, cfg ca.Config) ca.Service {
	t.Helper()
	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, authority.Organization()).Return(authority, nil)
	return ca.NewService(store, sealer, cfg)
}

func parseCertificatePEM(t *testing.T, encoded []byte) *x509.Certificate {
	t.Helper()
	block, rest := pem.Decode(encoded)
	require.NotNil(t, block)
	require.Empty(t, rest)
	require.Equal(t, "CERTIFICATE", block.Type)
	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	return cert
}

func parseKeyPEM(t *testing.T, encoded []byte) *ecdsa.PrivateKey {
	t.Helper()
	block, rest := pem.Decode(encoded)
	require.NotNil(t, block)
	require.Empty(t, rest)
	require.Equal(t, "PRIVATE KEY", block.Type)
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	require.NoError(t, err)
	key, ok := parsed.(*ecdsa.PrivateKey)
	require.True(t, ok)
	return key
}

// verifyAgainst chains the certificate to the given authority the way a
// listener would.
func verifyAgainst(t *testing.T, cert *ca.Certificate, authority *ca.Authority) error {
	t.Helper()
	root, err := x509.ParseCertificate(authority.Certificate())
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(root)
	_, err = parseCertificatePEM(t, cert.CertificatePEM()).Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	return err
}

func TestService_IssueCreatesTheOrganizationAuthorityOnDemand(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	cert, authority := issueForNewOrg(t, subject, newSealer(t), ca.Config{})

	assert.Equal(t, orgOf(subject), authority.Organization())
	assert.Equal(t, subject, cert.Subject())
	require.NoError(t, verifyAgainst(t, cert, authority))

	// The CA travels with the certificate: its holder needs it to
	// verify the other side of the connection.
	assert.Equal(t, authority.Certificate(), parseCertificatePEM(t, cert.IssuerPEM()).Raw)

	leaf := parseCertificatePEM(t, cert.CertificatePEM())
	assert.False(t, leaf.IsCA)
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, leaf.ExtKeyUsage)
	assert.Equal(t, cert.NotAfter(), leaf.NotAfter.UTC())
	assert.Equal(t, parseKeyPEM(t, cert.PrivateKeyPEM()).PublicKey, *leaf.PublicKey.(*ecdsa.PublicKey))
}

// TestService_IssueNamesTheSubjectInASANURI pins the identity ADR-0014
// decided on, including how it must be read back: a GRN is an opaque
// URI, so Path and Host are empty strings rather than errors, and a
// verifier reading Path recovers an identity every other empty Path
// equals.
func TestService_IssueNamesTheSubjectInASANURI(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	cert, _ := issueForNewOrg(t, subject, newSealer(t), ca.Config{})

	leaf := parseCertificatePEM(t, cert.CertificatePEM())
	require.Len(t, leaf.URIs, 1)
	uri := leaf.URIs[0]
	assert.Equal(t, subject.String(), uri.String())
	assert.Equal(t, "grn", uri.Scheme)
	assert.Equal(t, "acme:default:agent:bot-1", uri.Opaque)
	assert.Empty(t, uri.Path)
	assert.Empty(t, uri.Host)

	// The certificate carries the identity and nothing else.
	assert.Empty(t, leaf.Subject.CommonName)
	assert.Empty(t, leaf.DNSNames)
	assert.Empty(t, leaf.EmailAddresses)
}

// TestService_StoresTheAuthorityKeySealed demonstrates what a database
// dump alone yields: bytes that are not the key.
func TestService_StoresTheAuthorityKeySealed(t *testing.T) {
	sealer := newSealer(t)
	_, authority := issueForNewOrg(t, agentGRN("acme", "bot-1"), sealer, ca.Config{})

	opened, err := sealer.Open(context.Background(), authority.SealedKey())
	require.NoError(t, err)
	key, err := x509.ParsePKCS8PrivateKey(opened)
	require.NoError(t, err)
	authorityCert, err := x509.ParseCertificate(authority.Certificate())
	require.NoError(t, err)
	assert.Equal(t, authorityCert.PublicKey, key.(*ecdsa.PrivateKey).Public())

	assert.NotEqual(t, opened, authority.SealedKey())
	assert.False(t, bytes.Contains(authority.SealedKey(), opened), "the stored bytes contain the key")
}

func TestService_IssueReusesTheOrganizationAuthority(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	sealer := newSealer(t)
	first, authority := issueForNewOrg(t, subject, sealer, ca.Config{})

	// The mock fails the test if this reaches Create.
	second, err := serviceHolding(t, authority, sealer, ca.Config{}).
		Issue(context.Background(), agentGRN("acme", "bot-2"))
	require.NoError(t, err)

	require.NoError(t, verifyAgainst(t, second, authority))
	assert.NotEqual(t,
		parseCertificatePEM(t, first.CertificatePEM()).SerialNumber,
		parseCertificatePEM(t, second.CertificatePEM()).SerialNumber)
}

// TestService_CertificateDoesNotChainToAnotherOrganization is the
// tenancy boundary ADR-0014 put in the trust root: the failure, not the
// success, is what enforces it.
func TestService_CertificateDoesNotChainToAnotherOrganization(t *testing.T) {
	sealer := newSealer(t)
	acmeCert, acme := issueForNewOrg(t, agentGRN("acme", "bot-1"), sealer, ca.Config{})
	otherCert, other := issueForNewOrg(t, agentGRN("other", "bot-1"), sealer, ca.Config{})

	require.NoError(t, verifyAgainst(t, acmeCert, acme))
	require.NoError(t, verifyAgainst(t, otherCert, other))

	err := verifyAgainst(t, acmeCert, other)
	require.Error(t, err)
	assert.IsType(t, x509.UnknownAuthorityError{}, err)
	require.Error(t, verifyAgainst(t, otherCert, acme))
}

func TestService_IssueRejects(t *testing.T) {
	tests := map[string]grn.GRN{
		"empty subject":     {},
		"empty identifier":  agentGRN("acme", ""),
		"root owns no ca":   grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme"),
		"identifier splits": agentGRN("acme", "bot?1"),
		"control character": agentGRN("acme", "bot\n1"),
	}
	for name, subject := range tests {
		t.Run(name, func(t *testing.T) {
			// The mock fails the test if a rejected subject reaches the
			// store.
			svc := ca.NewService(mocks.NewMockStore(t), newSealer(t), ca.Config{})
			_, err := svc.Issue(context.Background(), subject)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}
}

func TestService_IssueFailsWhenSealingFails(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, orgOf(subject)).Return(nil, ca.ErrAuthorityNotFound).Once()
	sealer := mocks.NewMockSealer(t)
	sealer.EXPECT().Seal(mock.Anything, mock.Anything).
		Return(nil, errorx.New(errorx.KindUnavailable, "sealed by nothing")).Once()

	_, err := ca.NewService(store, sealer, ca.Config{}).Issue(context.Background(), subject)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnavailable))
}

// TestService_IssueFailsWhenTheAuthorityKeyDoesNotOpen is the process
// holding the wrong key-encryption key: it signs nothing, and the error
// says which key the stored bytes need.
func TestService_IssueFailsWhenTheAuthorityKeyDoesNotOpen(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	_, authority := issueForNewOrg(t, subject, newSealer(t), ca.Config{})

	_, err := serviceHolding(t, authority, newSealer(t), ca.Config{}).
		Issue(context.Background(), subject)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))
}

// TestService_IssueTakesTheAuthorityAConcurrentWriterCreated covers two
// processes creating an organization's CA at once: the loser signs with
// the winner's authority rather than failing its caller.
func TestService_IssueTakesTheAuthorityAConcurrentWriterCreated(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	sealer := newSealer(t)
	_, winner := issueForNewOrg(t, subject, sealer, ca.Config{})

	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, orgOf(subject)).Return(nil, ca.ErrAuthorityNotFound).Once()
	store.EXPECT().Create(mock.Anything, mock.Anything).Return(ca.ErrAuthorityAlreadyExists).Once()
	store.EXPECT().Get(mock.Anything, orgOf(subject)).Return(winner, nil).Once()

	cert, err := ca.NewService(store, sealer, ca.Config{}).Issue(context.Background(), subject)
	require.NoError(t, err)
	assert.NoError(t, verifyAgainst(t, cert, winner))
}

func TestService_CertificateLifetimeCarriesJitter(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	sealer := newSealer(t)
	lifetime := 24 * time.Hour
	_, authority := issueForNewOrg(t, subject, sealer, ca.Config{CertificateLifetime: lifetime})
	svc := serviceHolding(t, authority, sealer, ca.Config{CertificateLifetime: lifetime})

	expiries := make(map[time.Time]struct{})
	for range 8 {
		issued := time.Now().UTC()
		cert, err := svc.Issue(context.Background(), subject)
		require.NoError(t, err)
		// Jitter only shortens: the configured lifetime is a ceiling,
		// and a tenth of it is the widest the spread goes. The floor
		// takes a second on top of that tenth, because a certificate
		// stores its validity to the second and the expiry is rounded
		// down into it.
		assert.False(t, cert.NotAfter().After(issued.Add(lifetime)))
		assert.True(t, cert.NotAfter().After(issued.Add(lifetime-lifetime/10-time.Second)))
		expiries[cert.NotAfter()] = struct{}{}
	}
	assert.Greater(t, len(expiries), 1, "certificates issued together expire together")
}

func TestNewService_DefaultsTheCertificateLifetime(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	issued := time.Now().UTC()
	cert, _ := issueForNewOrg(t, subject, newSealer(t), ca.Config{})

	assert.False(t, cert.NotAfter().After(issued.Add(ca.DefaultCertificateLifetime)))
	assert.True(t, cert.NotAfter().After(issued.Add(ca.DefaultCertificateLifetime-ca.DefaultCertificateLifetime/10-time.Second)))
}
