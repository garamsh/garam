package ca_test

import (
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

func newService(t *testing.T, store ca.Store, sealer ca.Sealer, cfg ca.Config) ca.Service {
	t.Helper()
	svc, err := ca.NewService(store, sealer, cfg)
	require.NoError(t, err)
	return svc
}

// issueForNewOrg issues through a service whose store holds no
// authority yet, and returns the certificate with the authority the
// service created on demand.
func issueForNewOrg(t *testing.T, subject grn.GRN, sealer ca.Sealer, cfg ca.Config) (*ca.IssuedCertificate, *ca.Authority) {
	t.Helper()
	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, orgOf(subject)).Return(nil, ca.ErrAuthorityNotFound).Once()
	var created *ca.Authority
	store.EXPECT().Create(mock.Anything, mock.Anything).
		RunAndReturn(func(_ context.Context, a *ca.Authority) error {
			created = a
			return nil
		}).Once()

	svc := newService(t, store, sealer, cfg)
	cert, err := svc.Issue(context.Background(), subject)
	require.NoError(t, err)
	require.NotNil(t, created)
	return cert, created
}

// serviceHolding returns a service whose store already holds authority.
func serviceHolding(t *testing.T, authority *ca.Authority, sealer ca.Sealer, cfg ca.Config) ca.Service {
	t.Helper()
	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, authority.Organization).Return(authority, nil)
	return newService(t, store, sealer, cfg)
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
	root, err := x509.ParseCertificate(authority.Certificate)
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

	assert.Equal(t, orgOf(subject), authority.Organization)
	assert.Equal(t, subject, cert.Subject())
	require.NoError(t, verifyAgainst(t, &cert.Certificate, authority))

	// The CA travels with the certificate: its holder needs it to
	// verify the other side of the connection.
	assert.Equal(t, authority.Certificate, parseCertificatePEM(t, cert.IssuerPEM()).Raw)

	leaf := parseCertificatePEM(t, cert.CertificatePEM())
	assert.Equal(t, cert.NotAfter(), leaf.NotAfter.UTC())
	public, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	require.True(t, ok)
	assert.Equal(t, parseKeyPEM(t, cert.PrivateKeyPEM()).PublicKey, *public)
}

// TestService_IssuedCertificateCannotSign pins the fields that keep an
// issued certificate a leaf. Asserting that it is not a CA is not
// enough: an absent basic-constraints extension parses as "not a CA"
// too, and a key usage nobody reads can carry certificate signing.
func TestService_IssuedCertificateCannotSign(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	cert, authority := issueForNewOrg(t, subject, newSealer(t), ca.Config{})

	leaf := parseCertificatePEM(t, cert.CertificatePEM())
	assert.False(t, leaf.IsCA)
	assert.True(t, leaf.BasicConstraintsValid, "basic constraints are not encoded")
	assert.Equal(t, x509.KeyUsageDigitalSignature, leaf.KeyUsage)
	assert.Equal(t, []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, leaf.ExtKeyUsage)

	// The CA is the other half of that: it may sign certificates, and
	// only leaves of them.
	issuer, err := x509.ParseCertificate(authority.Certificate)
	require.NoError(t, err)
	assert.True(t, issuer.IsCA)
	assert.True(t, issuer.BasicConstraintsValid, "basic constraints are not encoded")
	assert.True(t, issuer.MaxPathLenZero, "the CA may issue intermediates")
	assert.Equal(t, 0, issuer.MaxPathLen)
	assert.Equal(t, x509.KeyUsageCertSign, issuer.KeyUsage)
	assert.Empty(t, issuer.ExtKeyUsage)

	// A serial has to be wide enough that two issuers never collide;
	// two that merely differ would hold for a single byte.
	assert.Greater(t, leaf.SerialNumber.BitLen(), 64)
	assert.Greater(t, issuer.SerialNumber.BitLen(), 64)
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

// TestService_IssueRecordsTheSerialTheCertificateCarries pins what a
// renewal is matched by. An operator presents the certificate it holds
// and garam finds the record by serial (ADR-0046), so a record whose
// serial is not the one inside the certificate names no certificate at
// all. x509 draws a serial of its own where a template leaves the field
// unset, so a certificate that parses and chains is no evidence that
// garam chose the number in it.
func TestService_IssueRecordsTheSerialTheCertificateCarries(t *testing.T) {
	cert, _ := issueForNewOrg(t, agentGRN("acme", "bot-1"), newSealer(t), ca.Config{})

	leaf := parseCertificatePEM(t, cert.CertificatePEM())
	require.NotNil(t, leaf.SerialNumber)
	assert.Positive(t, leaf.SerialNumber.Sign(), "a serial is a positive number")
	assert.Equal(t, leaf.SerialNumber.String(), cert.Serial())
}

// TestService_StoresTheAuthorityKeySealed demonstrates what a database
// dump alone yields. The claim is that the bytes are unusable, not that
// they differ from the key: a reversible encoding differs from it too
// and still hands a dump-holder the key, so what is asserted is that
// they neither parse as a key nor open under any other one.
func TestService_StoresTheAuthorityKeySealed(t *testing.T) {
	ctx := context.Background()
	sealer := newSealer(t)
	_, authority := issueForNewOrg(t, agentGRN("acme", "bot-1"), sealer, ca.Config{})

	row := []byte(authority.Organization.String())
	opened, err := sealer.Open(ctx, row, authority.SealedKey)
	require.NoError(t, err)
	key, err := x509.ParsePKCS8PrivateKey(opened)
	require.NoError(t, err)
	private, ok := key.(*ecdsa.PrivateKey)
	require.True(t, ok)
	authorityCert, err := x509.ParseCertificate(authority.Certificate)
	require.NoError(t, err)
	assert.Equal(t, authorityCert.PublicKey, private.Public())

	_, err = x509.ParsePKCS8PrivateKey(authority.SealedKey)
	assert.Error(t, err, "the stored bytes parse as a private key")
	_, err = newSealer(t).Open(ctx, row, authority.SealedKey)
	assert.Error(t, err, "the stored bytes open under a key-encryption key that never sealed them")
}

func TestService_IssueReusesTheOrganizationAuthority(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	sealer := newSealer(t)
	first, authority := issueForNewOrg(t, subject, sealer, ca.Config{})

	// The mock fails the test if this reaches Create.
	second, err := serviceHolding(t, authority, sealer, ca.Config{}).
		Issue(context.Background(), agentGRN("acme", "bot-2"))
	require.NoError(t, err)

	require.NoError(t, verifyAgainst(t, &second.Certificate, authority))
	assert.NotEqual(t,
		parseCertificatePEM(t, first.CertificatePEM()).SerialNumber,
		parseCertificatePEM(t, second.CertificatePEM()).SerialNumber)
}

// TestService_CertificateDoesNotChainToAnotherOrganization is the
// tenancy boundary ADR-0014 put in the trust root. Two certificates
// failing to chain proves only that two keys differ, so the case that
// carries the organization is here beside it: two subjects of one
// organization share its CA, and a subject of another does not.
func TestService_CertificateDoesNotChainToAnotherOrganization(t *testing.T) {
	sealer := newSealer(t)
	acmeCert, acme := issueForNewOrg(t, agentGRN("acme", "bot-1"), sealer, ca.Config{})
	otherCert, other := issueForNewOrg(t, agentGRN("other", "bot-1"), sealer, ca.Config{})

	sibling, err := serviceHolding(t, acme, sealer, ca.Config{}).
		Issue(context.Background(), agentGRN("acme", "bot-2"))
	require.NoError(t, err)

	// Same organization, same trust root.
	require.NoError(t, verifyAgainst(t, &acmeCert.Certificate, acme))
	require.NoError(t, verifyAgainst(t, &sibling.Certificate, acme))
	require.NoError(t, verifyAgainst(t, &otherCert.Certificate, other))

	// Another organization, neither direction.
	err = verifyAgainst(t, &acmeCert.Certificate, other)
	require.Error(t, err)
	assert.IsType(t, x509.UnknownAuthorityError{}, err)
	assert.Error(t, verifyAgainst(t, &sibling.Certificate, other))
	assert.Error(t, verifyAgainst(t, &otherCert.Certificate, acme))
}

// TestService_IssuerPEMIsTheAuthorityThatSignsAndCreatesNone reads the
// authority that signs for a subject without signing: it is the issuer
// a certificate signed for that organization carries. An organization
// with no authority is not found, and the mock fails the test if the
// read reaches Create; another organization's authority is refused as
// Issue refuses it.
func TestService_IssuerPEMIsTheAuthorityThatSignsAndCreatesNone(t *testing.T) {
	ctx := context.Background()
	sealer := newSealer(t)
	cert, acme := issueForNewOrg(t, agentGRN("acme", "bot-1"), sealer, ca.Config{})

	issuer, err := serviceHolding(t, acme, sealer, ca.Config{}).IssuerPEM(ctx, agentGRN("acme", "bot-2"))
	require.NoError(t, err)
	assert.Equal(t, cert.IssuerPEM(), issuer)

	absent := agentGRN("other", "bot-1")
	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, orgOf(absent)).Return(nil, ca.ErrAuthorityNotFound).Once()
	_, err = newService(t, store, sealer, ca.Config{}).IssuerPEM(ctx, absent)
	assert.ErrorIs(t, err, ca.ErrAuthorityNotFound)

	wrong := mocks.NewMockStore(t)
	wrong.EXPECT().Get(mock.Anything, orgOf(absent)).Return(acme, nil).Once()
	_, err = newService(t, wrong, sealer, ca.Config{}).IssuerPEM(ctx, absent)
	assert.True(t, errorx.IsKind(err, errorx.KindInternal), "another organization's authority")
}

// TestService_IssueRefusesAnAuthorityFromAnotherOrganization covers the
// store seam rather than the store that ships: a lookup by primary key
// cannot return the wrong row, but Store is exported and mocked, and a
// certificate signed by another organization's CA is the one failure
// the trust root cannot catch after the fact.
func TestService_IssueRefusesAnAuthorityFromAnotherOrganization(t *testing.T) {
	sealer := newSealer(t)
	_, other := issueForNewOrg(t, agentGRN("other", "bot-1"), sealer, ca.Config{})

	subject := agentGRN("acme", "bot-1")
	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, orgOf(subject)).Return(other, nil).Once()

	_, err := newService(t, store, sealer, ca.Config{}).Issue(context.Background(), subject)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInternal))
}

// TestService_IssueFailsWhenTheStoreReturnsNothing keeps the same seam
// from panicking on a nil authority with a nil error.
func TestService_IssueFailsWhenTheStoreReturnsNothing(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	store := mocks.NewMockStore(t)
	store.EXPECT().Get(mock.Anything, orgOf(subject)).Return(nil, nil).Once()

	_, err := newService(t, store, newSealer(t), ca.Config{}).Issue(context.Background(), subject)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindInternal))
}

func TestService_IssueRejects(t *testing.T) {
	// The subject every row below is one spoiled field of, issued
	// through the same Issue. It is what stops being accepted if the
	// service starts refusing on principle rather than on the
	// identifier it was handed.
	accepted, _ := issueForNewOrg(t, agentGRN("acme", "bot-1"), newSealer(t), ca.Config{})
	require.NotNil(t, accepted)

	tests := map[string]grn.GRN{
		"empty subject":     {},
		"empty identifier":  agentGRN("acme", ""),
		"root owns no ca":   grn.New(grn.RootOwner, grn.DefaultProject, "org", "acme"),
		"identifier splits": agentGRN("acme", "bot?1"),
		"control character": agentGRN("acme", "bot\n1"),
		// A SAN URI is an IA5String, so this cannot be encoded — and it
		// is refused here rather than by the encoder, after a CA exists.
		"beyond ascii": agentGRN("acme", "bot-é"),
		// A colon shifts every segment after it, so this GRN's SAN URI
		// reads back as identifier "agent:bot-1" in organization "ac".
		"segment holds a colon": grn.New("ac:me", grn.DefaultProject, "agent", "bot-1"),
	}
	for name, subject := range tests {
		t.Run(name, func(t *testing.T) {
			// The mock fails the test if a rejected subject reaches the
			// store.
			svc := newService(t, mocks.NewMockStore(t), newSealer(t), ca.Config{})
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
	sealer.EXPECT().Seal(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errorx.New(errorx.KindUnavailable, "sealed by nothing")).Once()

	_, err := newService(t, store, sealer, ca.Config{}).Issue(context.Background(), subject)
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

	cert, err := newService(t, store, sealer, ca.Config{}).Issue(context.Background(), subject)
	require.NoError(t, err)
	assert.NoError(t, verifyAgainst(t, &cert.Certificate, winner))
}

// TestService_IssueRefusesToOutliveTheAuthority covers a lifetime
// longer than the CA has left: the certificate would stop verifying the
// day the CA expires, so it is not signed at all.
func TestService_IssueRefusesToOutliveTheAuthority(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	sealer := newSealer(t)
	_, authority := issueForNewOrg(t, subject, sealer, ca.Config{})
	issuer, err := x509.ParseCertificate(authority.Certificate)
	require.NoError(t, err)

	// An hour before the CA expires, a certificate of any permitted
	// lifetime would outlive it.
	at := func(when time.Time) ca.Config {
		return ca.Config{Now: func() time.Time { return when }}
	}
	_, err = serviceHolding(t, authority, sealer, at(issuer.NotAfter.Add(-time.Hour))).
		Issue(context.Background(), subject)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))
	assert.Contains(t, err.Error(), "outlive")

	// Past that, the refusal names the expiry rather than the overrun.
	_, err = serviceHolding(t, authority, sealer, at(issuer.NotAfter.Add(time.Hour))).
		Issue(context.Background(), subject)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindFailedPrecondition))
	assert.Contains(t, err.Error(), "expired")
}

func TestService_CertificateLifetimeCarriesJitter(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	sealer := newSealer(t)
	lifetime := 24 * time.Hour
	_, authority := issueForNewOrg(t, subject, sealer, ca.Config{CertificateLifetime: lifetime})
	svc := serviceHolding(t, authority, sealer, ca.Config{CertificateLifetime: lifetime})

	var earliest, latest time.Time
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
		if earliest.IsZero() || cert.NotAfter().Before(earliest) {
			earliest = cert.NotAfter()
		}
		if cert.NotAfter().After(latest) {
			latest = cert.NotAfter()
		}
	}
	// Distinct expiries are not spread ones: a two-second spread would
	// satisfy "not identical" and still restart a fleet together, which
	// is the outage the jitter exists to prevent. Eight draws from a
	// tenth of the lifetime landing inside a hundredth of it is about
	// one in a million, so the spread itself is the assertion.
	assert.Greater(t, latest.Sub(earliest), lifetime/100, "expiries are bunched")
}

// TestNewService_RefusesALifetimeOutsideTheBounds covers the parameter
// that bounds a certificate nobody withdraws: expiry is all an agent's
// has (ADR-0014), so a lifetime long enough to defeat it, or short
// enough to expire before the certificate is installed, fails
// construction.
func TestNewService_RefusesALifetimeOutsideTheBounds(t *testing.T) {
	tests := map[string]time.Duration{
		"a nanosecond":          time.Nanosecond,
		"under the minimum":     ca.MinCertificateLifetime - time.Second,
		"over the maximum":      ca.MaxCertificateLifetime + time.Second,
		"nine years":            9 * 365 * 24 * time.Hour,
		"negative":              -time.Hour,
		"negative and enormous": -9 * 365 * 24 * time.Hour,
	}
	for name, lifetime := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := ca.NewService(mocks.NewMockStore(t), newSealer(t), ca.Config{CertificateLifetime: lifetime})
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindInvalidArgument))
		})
	}

	for name, lifetime := range map[string]time.Duration{
		"the minimum": ca.MinCertificateLifetime,
		"the maximum": ca.MaxCertificateLifetime,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ca.NewService(mocks.NewMockStore(t), newSealer(t), ca.Config{CertificateLifetime: lifetime})
			require.NoError(t, err)
		})
	}
}

// TestService_IssueBackdatesAgainstClockSkew holds the clock still, so
// the validity window is exact rather than approximate.
func TestService_IssueBackdatesAgainstClockSkew(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	cert, authority := issueForNewOrg(t, subject, newSealer(t), ca.Config{Now: func() time.Time { return now }})

	leaf := parseCertificatePEM(t, cert.CertificatePEM())
	assert.True(t, leaf.NotBefore.Before(now), "a peer whose clock trails the issuer cannot use it")
	assert.Equal(t, now.Add(-time.Minute), leaf.NotBefore.UTC())

	issuer, err := x509.ParseCertificate(authority.Certificate)
	require.NoError(t, err)
	assert.Equal(t, now.Add(-time.Minute), issuer.NotBefore.UTC())
}

func TestNewService_DefaultsTheCertificateLifetime(t *testing.T) {
	subject := agentGRN("acme", "bot-1")
	issued := time.Now().UTC()
	cert, _ := issueForNewOrg(t, subject, newSealer(t), ca.Config{})

	assert.False(t, cert.NotAfter().After(issued.Add(ca.DefaultCertificateLifetime)))
	assert.True(t, cert.NotAfter().After(issued.Add(ca.DefaultCertificateLifetime-ca.DefaultCertificateLifetime/10-time.Second)))
}
