package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/garam/internal/ca"
	capg "github.com/garamsh/garam/internal/ca/postgres"
	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/org"
)

// TestCertificateAuthority_RoundTrip issues against real Postgres: an
// organization's CA is created by the first certificate that needs it,
// the row holds the private key only in sealed form, and a certificate
// signed by one organization does not chain against another's.
func TestCertificateAuthority_RoundTrip(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	orgs := org.NewPostgresStore(p)
	acme := orgRootGRN("acme")
	other := orgRootGRN("other")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(acme, "acme")))
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(other, "other")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	store := capg.New(p)
	svc, err := ca.NewService(store, sealer, ca.Config{})
	require.NoError(t, err)

	_, err = store.Get(ctx, acme)
	assert.ErrorIs(t, err, ca.ErrAuthorityNotFound, "a CA exists only once something needs one")

	subject := scopedGRN("acme", "agent", "bot-1")
	cert, err := svc.Issue(ctx, subject)
	require.NoError(t, err)

	authority, err := store.Get(ctx, acme)
	require.NoError(t, err)
	assert.Equal(t, acme, authority.Organization)
	assert.Equal(t, subject.String(), leafOf(t, &cert.Certificate).URIs[0].String())
	require.NoError(t, chainsTo(t, &cert.Certificate, authority))

	// What a database dump yields is the sealed form: readable through
	// the sealer that produced it, and unusable otherwise — it is not a
	// key, and it does not open under a key that never sealed it.
	var sealed []byte
	require.NoError(t, p.QueryRow(ctx,
		`SELECT sealed_key FROM certificate_authorities WHERE org_grn = $1`, acme.String()).Scan(&sealed))
	opened, err := sealer.Open(ctx, []byte(acme.String()), sealed)
	require.NoError(t, err)
	_, err = x509.ParsePKCS8PrivateKey(opened)
	require.NoError(t, err)
	_, err = x509.ParsePKCS8PrivateKey(sealed)
	assert.Error(t, err, "the stored bytes parse as a private key")
	strangerKey, err := ca.NewAESGCMSealer(make([]byte, ca.KeySize))
	require.NoError(t, err)
	_, err = strangerKey.Open(ctx, []byte(acme.String()), sealed)
	assert.Error(t, err, "the stored bytes open under a key-encryption key that never sealed them")

	// The organization's second agent takes the CA the first created.
	sibling, err := svc.Issue(ctx, scopedGRN("acme", "agent", "bot-2"))
	require.NoError(t, err)
	assert.Equal(t, cert.IssuerPEM(), sibling.IssuerPEM())

	// Another organization gets a CA of its own, and neither trust root
	// accepts the other's certificate (ADR-0014).
	foreign, err := svc.Issue(ctx, scopedGRN("other", "agent", "bot-1"))
	require.NoError(t, err)
	foreignAuthority, err := store.Get(ctx, other)
	require.NoError(t, err)
	require.NoError(t, chainsTo(t, &foreign.Certificate, foreignAuthority))
	assert.Error(t, chainsTo(t, &cert.Certificate, foreignAuthority))
	assert.Error(t, chainsTo(t, &foreign.Certificate, authority))

	// A CA belongs to an organization that exists, and no CA is written
	// for one that does not.
	_, err = svc.Issue(ctx, scopedGRN("ghost", "agent", "bot-1"))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindNotFound))
	var authorities int
	require.NoError(t, p.QueryRow(ctx, `SELECT count(*) FROM certificate_authorities`).Scan(&authorities))
	assert.Equal(t, 2, authorities)

	// Write access to the database is not enough to move a trust root:
	// an authority row copied onto another organization carries a
	// sealed key that does not open there, so the copy signs nothing.
	copycat := orgRootGRN("copycat")
	require.NoError(t, orgs.CreateOrg(ctx, org.NewOrganization(copycat, "copycat")))
	_, err = p.Exec(ctx, `INSERT INTO certificate_authorities (org_grn, certificate, sealed_key, not_after, created_at)
		SELECT $1, certificate, sealed_key, not_after, created_at
		FROM certificate_authorities WHERE org_grn = $2`, copycat.String(), acme.String())
	require.NoError(t, err)

	_, err = svc.Issue(ctx, scopedGRN("copycat", "agent", "bot-1"))
	require.Error(t, err)
	assert.True(t, errorx.IsKind(err, errorx.KindDataLoss))

	assert.ErrorIs(t, store.Create(ctx, authority), ca.ErrAuthorityAlreadyExists)
}

// TestCertificateAuthority_SurrendersItsTimestamps holds that the two
// instants an Authority persists are reachable from the aggregate the
// store returns (ADR-0051). The table carries no updated_at, so there
// is no pair to play against itself; the certificate stands in for one.
// not_after is written in a column of its own and the same instant
// travels inside the certificate bytes beside it, so the field is read
// against a value the row carries independently of that column.
func TestCertificateAuthority_SurrendersItsTimestamps(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	acme := orgRootGRN("acme")
	require.NoError(t, org.NewPostgresStore(p).CreateOrg(ctx, org.NewOrganization(acme, "acme")))

	key := make([]byte, ca.KeySize)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := ca.NewAESGCMSealer(key)
	require.NoError(t, err)
	store := capg.New(p)
	svc, err := ca.NewService(store, sealer, ca.Config{})
	require.NoError(t, err)

	// The service stamps the authority from a `now` truncated to the
	// second, so the window's lower bound is truncated with it.
	before := time.Now().UTC().Truncate(time.Second)
	_, err = svc.Issue(ctx, scopedGRN("acme", "agent", "bot-1"))
	require.NoError(t, err)
	after := time.Now().UTC()

	authority, err := store.Get(ctx, acme)
	require.NoError(t, err)
	certificate, err := x509.ParseCertificate(authority.Certificate)
	require.NoError(t, err)

	assert.True(t, authority.NotAfter.Equal(certificate.NotAfter),
		"the stored not_after is the instant the CA certificate itself carries")
	assert.WithinRange(t, authority.CreatedAt, before, after)
	assert.True(t, authority.CreatedAt.Before(authority.NotAfter),
		"a CA is created before it expires")
}

// leafOf parses the issued certificate.
func leafOf(t *testing.T, cert *ca.Certificate) *x509.Certificate {
	t.Helper()
	block, _ := pem.Decode(cert.CertificatePEM())
	require.NotNil(t, block)
	leaf, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)
	require.Len(t, leaf.URIs, 1)
	return leaf
}

// chainsTo verifies the certificate against one organization's CA, the
// way a listener requiring client auth would.
func chainsTo(t *testing.T, cert *ca.Certificate, authority *ca.Authority) error {
	t.Helper()
	root, err := x509.ParseCertificate(authority.Certificate)
	require.NoError(t, err)
	roots := x509.NewCertPool()
	roots.AddCert(root)
	_, err = leafOf(t, cert).Verify(x509.VerifyOptions{
		Roots:     roots,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	return err
}
