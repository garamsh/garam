package machine_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/ca"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
	"github.com/garamsh/garam/internal/machine"
)

func agentGRN(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, "agent", id)
}

func orgOf(subject grn.GRN) grn.GRN {
	return grn.New(grn.RootOwner, grn.DefaultProject, "org", subject.Organization)
}

// authorities is a ca.Store in memory. The tests need internal/ca to
// create a real certificate authority per organization, and nothing
// else a store does.
type authorities map[grn.GRN]*ca.Authority

func (a authorities) Create(_ context.Context, authority *ca.Authority) error {
	if _, held := a[authority.Organization()]; held {
		return ca.ErrAuthorityAlreadyExists
	}
	a[authority.Organization()] = authority
	return nil
}

func (a authorities) Get(_ context.Context, orgGRN grn.GRN) (*ca.Authority, error) {
	authority, held := a[orgGRN]
	if !held {
		return nil, ca.ErrAuthorityNotFound
	}
	return authority, nil
}

// trustRoots reads what a presented certificate must chain to out of
// the same authorities the certificates were issued from — the
// adapter the composition root builds over ca.Store, in the form a
// test can hold.
type trustRoots struct{ store authorities }

func (r trustRoots) Get(ctx context.Context, subject grn.GRN) ([]byte, error) {
	authority, err := r.store.Get(ctx, orgOf(subject))
	if err != nil {
		return nil, err
	}
	return authority.Certificate(), nil
}

// registrations is the record an identity authenticates on, as a test
// holds it: the set of GRNs the organizations still have. A GRN absent
// from it is one whose record was withdrawn, which is what deleting an
// operator does (ADR-0070).
//
// held is what a test starts from, because the check is not what most
// of these tests are about: an authority that signed a certificate has
// a record behind it unless a test says otherwise.
type registrations struct{ withdrawn map[grn.GRN]bool }

func held() registrations { return registrations{withdrawn: map[grn.GRN]bool{}} }

func (r registrations) without(subject grn.GRN) registrations {
	r.withdrawn[subject] = true
	return r
}

func (r registrations) Holds(_ context.Context, subject grn.GRN) error {
	if r.withdrawn[subject] {
		return errorx.New(errorx.KindNotFound, "no record of this identity")
	}
	return nil
}

// issuer is garam's issuing side as the tests hold it: real
// authorities created on demand by internal/ca, and the key of one
// when a test has to sign what the issuing side refuses to sign.
type issuer struct {
	store  authorities
	sealer ca.Sealer
}

func newIssuer(t *testing.T) *issuer {
	t.Helper()
	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	return &issuer{store: authorities{}, sealer: sealer}
}

// service returns the authentication service over the authorities
// issued from, admitting every identity they signed for.
func (i *issuer) service() machine.Service {
	return i.serviceHolding(held())
}

// serviceHolding is the same service over the records the
// organizations still hold.
func (i *issuer) serviceHolding(records registrations) machine.Service {
	return machine.NewService(trustRoots{store: i.store}, records)
}

// issue returns the client certificate garam signs for subject,
// creating the organization's authority on the first one.
func (i *issuer) issue(t *testing.T, subject grn.GRN) tls.Certificate {
	t.Helper()
	svc, err := ca.NewService(i.store, i.sealer, ca.Config{})
	require.NoError(t, err)
	issued, err := svc.Issue(context.Background(), subject)
	require.NoError(t, err)
	pair, err := tls.X509KeyPair(issued.CertificatePEM(), issued.PrivateKeyPEM())
	require.NoError(t, err)
	return pair
}

// authority brings an organization's certificate authority into
// existence the way garam does: on the first certificate it needs.
func (i *issuer) authority(t *testing.T, org string) {
	t.Helper()
	i.issue(t, agentGRN(org, "first"))
}

// leaf is what a test asks an organization's authority to sign: the
// SAN entries to carry, and when the certificate stops being valid.
type leaf struct {
	uris     []string
	hosts    []string
	notAfter time.Time
}

// sign returns a certificate the named organization's own authority
// signed, described entry by entry rather than by subject GRN. What
// garam issues always survives the SAN round trip and always chains to
// its own organization, so the certificates that do not have to be
// minted here.
func (i *issuer) sign(t *testing.T, org string, l leaf) tls.Certificate {
	t.Helper()
	authority, authorityKey := i.open(t, org)
	uris := make([]*url.URL, 0, len(l.uris))
	for _, raw := range l.uris {
		uri, err := url.Parse(raw)
		require.NoError(t, err)
		uris = append(uris, uri)
	}
	notAfter := l.notAfter
	if notAfter.IsZero() {
		notAfter = time.Now().Add(time.Hour)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "minted by the test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		URIs:                  uris,
		DNSNames:              l.hosts,
	}, authority, &key.PublicKey, authorityKey)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// selfSigned returns a certificate nothing but its own key signed,
// naming subject. It is what a caller with no certificate can always
// make for itself, so it is what a check reading the SAN without the
// chain would accept.
func (i *issuer) selfSigned(t *testing.T, subject grn.GRN) tls.Certificate {
	t.Helper()
	uri, err := url.Parse(subject.String())
	require.NoError(t, err)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		URIs:                  []*url.URL{uri},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// open returns an organization's authority and the private key behind
// it, which is sealed exactly as the column holds it.
func (i *issuer) open(t *testing.T, org string) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	ctx := context.Background()
	authority, err := i.store.Get(ctx, grn.New(grn.RootOwner, grn.DefaultProject, "org", org))
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(authority.Certificate())
	require.NoError(t, err)
	keyDER, err := i.sealer.Open(ctx, []byte(authority.Organization().String()), authority.SealedKey())
	require.NoError(t, err)
	parsed, err := x509.ParsePKCS8PrivateKey(keyDER)
	require.NoError(t, err)
	key, ok := parsed.(*ecdsa.PrivateKey)
	require.True(t, ok)
	return certificate, key
}

// authenticate runs one certificate through the service, as the
// handshake hands it over.
func authenticate(t *testing.T, svc machine.Service, pair tls.Certificate) (machine.Identity, error) {
	t.Helper()
	return svc.Authenticate(context.Background(), pair.Certificate)
}

func TestService_AuthenticatesACertificateItsOrganizationsAuthoritySigned(t *testing.T) {
	issuer := newIssuer(t)
	subject := agentGRN("acme", "bot-1")

	identity, err := authenticate(t, issuer.service(), issuer.issue(t, subject))
	require.NoError(t, err)
	assert.Equal(t, subject, identity.GRN())
}

// TestService_CarriesThePresentedCertificate holds the identity against
// the certificate it was read from: which certificate authenticated is
// what decides whether it may be replaced (ADR-0046), and an identity
// that named the subject alone could not answer that.
//
// The second certificate is the control. One certificate matching its
// own fields holds for an identity carrying anything at all; two
// certificates of one subject differ in the serial alone, so the
// identity following it is the serial being read.
func TestService_CarriesThePresentedCertificate(t *testing.T) {
	issuer := newIssuer(t)
	svc := issuer.service()
	subject := agentGRN("acme", "bot-1")
	issuer.authority(t, "acme")

	notAfter := time.Now().Add(90 * time.Minute).Truncate(time.Second)
	first := issuer.sign(t, "acme", leaf{uris: []string{subject.String()}, notAfter: notAfter})
	identity, err := authenticate(t, svc, first)
	require.NoError(t, err)
	presented, err := x509.ParseCertificate(first.Certificate[0])
	require.NoError(t, err)
	assert.Equal(t, presented.SerialNumber.String(), identity.Serial())
	assert.Equal(t, presented.NotBefore, identity.NotBefore())
	assert.Equal(t, presented.NotAfter, identity.NotAfter())

	second := issuer.sign(t, "acme", leaf{uris: []string{subject.String()}, notAfter: notAfter})
	other, err := authenticate(t, svc, second)
	require.NoError(t, err)
	assert.Equal(t, subject, other.GRN())
	assert.NotEqual(t, identity.Serial(), other.Serial(),
		"two certificates naming one identity are told apart by the serial")
}

// TestService_RefusesACertificateAnotherOrganizationsAuthoritySigned is
// the tenancy boundary ADR-0014 put in the trust root. Two certificates
// failing to verify proves only that two keys differ, so the case that
// carries the organization is here beside it: a second identity of the
// same organization authenticates against the same root, and only the
// certificate whose signer disagrees with its own SAN is refused.
func TestService_RefusesACertificateAnotherOrganizationsAuthoritySigned(t *testing.T) {
	issuer := newIssuer(t)
	svc := issuer.service()
	acme := agentGRN("acme", "bot-1")
	sibling := agentGRN("acme", "bot-2")
	other := agentGRN("other", "bot-1")

	// One authority per organization, both real and both created by
	// internal/ca on the certificate that needed them.
	for _, subject := range []grn.GRN{acme, sibling, other} {
		identity, err := authenticate(t, svc, issuer.issue(t, subject))
		require.NoError(t, err)
		require.Equal(t, subject, identity.GRN())
	}

	// The same identities, signed by the authority of the organization
	// they do not belong to.
	for name, forged := range map[string]tls.Certificate{
		"acme signed by other": issuer.sign(t, "other", leaf{uris: []string{acme.String()}}),
		"other signed by acme": issuer.sign(t, "acme", leaf{uris: []string{other.String()}}),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := authenticate(t, svc, forged)
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
		})
	}
}

// TestService_RefusesASANThatIsNotOneWholeGRN covers what this boundary
// requires of a SAN: exactly one URI, and a URI that parses whole as a
// GRN. The separator cases are the trap the issuing side was fixed for,
// read from the other side of the same wire.
//
// Every case here is signed by the organization's own authority, and
// the control is a certificate from that same authority carrying a SAN
// that does parse. What separates them is the SAN alone.
func TestService_RefusesASANThatIsNotOneWholeGRN(t *testing.T) {
	issuer := newIssuer(t)
	svc := issuer.service()
	subject := agentGRN("acme", "bot-1")
	issuer.authority(t, "acme")

	control, err := authenticate(t, svc, issuer.sign(t, "acme", leaf{uris: []string{subject.String()}}))
	require.NoError(t, err, "a certificate this authority signed with a whole GRN in its SAN")
	require.Equal(t, subject, control.GRN())

	tests := map[string]leaf{
		// '?' and '#' move the tail of a URI into its query or its
		// fragment, and neither is a segment character.
		"query separator":    {uris: []string{"grn:acme:default:agent:bot?1"}},
		"fragment separator": {uris: []string{"grn:acme:default:agent:bot#1"}},
		// Two identities have no single answer to who is calling.
		"two identities": {uris: []string{subject.String(), agentGRN("acme", "bot-2").String()}},
		// None has none either, which is what refuses a server
		// certificate presented here: it names a host, not an identity.
		"no identity": {hosts: []string{"gateway.acme.example"}},
		// Neither hierarchical form is the canonical string, whatever
		// Host and Path recover from it.
		"hierarchical uri": {uris: []string{"grn://acme/default/agent/bot-1"}},
		"not a grn":        {uris: []string{"https://acme.example/agent/bot-1"}},
	}
	for name, l := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := authenticate(t, svc, issuer.sign(t, "acme", l))
			require.Error(t, err)
			assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
		})
	}
}

// TestService_RefusesAnExpiredCertificate holds everything but the
// expiry still: both certificates carry the same identity and are
// signed by the same authority, so the one that is refused is refused
// for having run out. Expiry is the only revocation ADR-0014 leaves.
func TestService_RefusesAnExpiredCertificate(t *testing.T) {
	issuer := newIssuer(t)
	svc := issuer.service()
	subject := agentGRN("acme", "bot-1")
	issuer.authority(t, "acme")

	identity, err := authenticate(t, svc,
		issuer.sign(t, "acme", leaf{uris: []string{subject.String()}, notAfter: time.Now().Add(time.Hour)}))
	require.NoError(t, err)
	require.Equal(t, subject, identity.GRN())

	_, err = authenticate(t, svc,
		issuer.sign(t, "acme", leaf{uris: []string{subject.String()}, notAfter: time.Now().Add(-time.Minute)}))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
}

// TestService_RefusesAnOrganizationWithNoAuthority keeps the refusal
// off the kind that would answer a question the caller did not ask:
// whether the organization exists.
//
// The certificate acme's own authority signs for acme is the control.
// Both are signed by that authority and read through the same service,
// and only the organization the SAN names differs, so what refuses the
// second is the missing authority rather than the signer the test had
// to borrow to mint it.
func TestService_RefusesAnOrganizationWithNoAuthority(t *testing.T) {
	issuer := newIssuer(t)
	svc := issuer.service()
	subject := agentGRN("acme", "bot-1")
	issuer.authority(t, "acme")

	identity, err := authenticate(t, svc, issuer.sign(t, "acme", leaf{uris: []string{subject.String()}}))
	require.NoError(t, err)
	require.Equal(t, subject, identity.GRN())

	_, err = authenticate(t, svc,
		issuer.sign(t, "acme", leaf{uris: []string{agentGRN("nobody", "bot-1").String()}}))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
}

// TestService_RefusesAConnectionThatPresentedNothing reads the empty
// chain against a real one through the same service: without it a
// service that authenticates nothing at all refuses this connection
// too, and reads exactly the same.
func TestService_RefusesAConnectionThatPresentedNothing(t *testing.T) {
	issuer := newIssuer(t)
	svc := issuer.service()
	subject := agentGRN("acme", "bot-1")

	identity, err := authenticate(t, svc, issuer.issue(t, subject))
	require.NoError(t, err)
	require.Equal(t, subject, identity.GRN())

	_, err = svc.Authenticate(context.Background(), nil)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated))
}

// operatorGRNOf names an operator of org, which is the identity a
// withdrawn record reaches (ADR-0070).
func operatorGRNOf(org, id string) grn.GRN {
	return grn.New(org, grn.DefaultProject, "operator", id)
}

// TestService_RefusesAnIdentityWhoseRecordWasWithdrawn is the reversal
// ADR-0070 takes: a certificate that chains to its organization's
// authority and has not expired stops authenticating once the
// organization withdraws its record of the identity. Nothing about the
// certificate changes between the two calls.
//
// The admitted call is the control. Both present the same certificate
// through a service over the same authority, and only the record
// differs, so what refuses the second is the withdrawal rather than
// anything the certificate carries — which is what a refusal for an
// expiry, a chain or a SAN would look like too.
func TestService_RefusesAnIdentityWhoseRecordWasWithdrawn(t *testing.T) {
	issuer := newIssuer(t)
	subject := operatorGRNOf("acme", "one")
	issuer.authority(t, "acme")
	certificate := issuer.sign(t, "acme", leaf{uris: []string{subject.String()}})

	identity, err := authenticate(t, issuer.service(), certificate)
	require.NoError(t, err)
	require.Equal(t, subject, identity.GRN())

	_, err = authenticate(t, issuer.serviceHolding(held().without(subject)), certificate)
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindUnauthenticated),
		"a withdrawn record is answered as a certificate that did not authenticate")
}

// TestService_WithdrawingOneRecordLeavesEveryOtherIdentity is
// constraint 3 of #1085, asserted rather than left to be discovered: a
// withdrawal reaches the identity it names and no other. The agent
// here belongs to the same organization and is signed by the same
// authority, so nothing but the GRN separates it from the operator
// that was refused.
func TestService_WithdrawingOneRecordLeavesEveryOtherIdentity(t *testing.T) {
	issuer := newIssuer(t)
	withdrawn := operatorGRNOf("acme", "one")
	issuer.authority(t, "acme")
	svc := issuer.serviceHolding(held().without(withdrawn))

	_, err := authenticate(t, svc, issuer.sign(t, "acme", leaf{uris: []string{withdrawn.String()}}))
	require.Error(t, err)

	for _, subject := range []grn.GRN{
		agentGRN("acme", "bot-1"),
		operatorGRNOf("acme", "two"),
	} {
		identity, err := authenticate(t, svc, issuer.sign(t, "acme", leaf{uris: []string{subject.String()}}))
		require.NoError(t, err, subject.String())
		assert.Equal(t, subject, identity.GRN())
	}
}
