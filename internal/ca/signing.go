package ca

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

const (
	// authorityLifetime outlives every certificate the CA signs by
	// enough that no organization meets a cliff: losing a CA invalidates
	// every agent under it (ADR-0014), and nothing rotates one yet.
	authorityLifetime = 10 * 365 * 24 * time.Hour
	// clockSkew backdates NotBefore, so a certificate is usable on a
	// peer whose clock trails the issuer's.
	clockSkew = time.Minute
	// jitterFraction is the share of a lifetime that jitter may remove.
	jitterFraction = 10
	// serialBits sizes the random serial number.
	serialBits = 128
)

// generateKey returns a fresh P-256 key. Both the CA and what it signs
// use one curve, so an agent needs no negotiation to read either.
func generateKey() (*ecdsa.PrivateKey, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: generate key", errorx.KindInternal)
	}
	return key, nil
}

// createAuthorityCertificate self-signs the CA certificate of orgGRN
// and returns it in DER form. The organization names the certificate;
// what the CA signs is identified by its SAN URI, never by a name.
func createAuthorityCertificate(orgGRN grn.GRN, key *ecdsa.PrivateKey, now time.Time) ([]byte, error) {
	serial, err := serialNumber()
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: orgGRN.String()},
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              now.Add(authorityLifetime),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: create authority certificate", errorx.KindInternal)
	}
	return der, nil
}

// createCertificate signs a client certificate for the subject named
// by uri with the issuer's key, and returns it in DER form.
func createCertificate(uri *url.URL, public *ecdsa.PublicKey, issuer *x509.Certificate, issuerKey *ecdsa.PrivateKey, now, notAfter time.Time) ([]byte, error) {
	serial, err := serialNumber()
	if err != nil {
		return nil, err
	}
	// An empty subject name makes Go mark the SAN critical, which is
	// what carries the identity here (ADR-0014).
	template := &x509.Certificate{
		SerialNumber:          serial,
		NotBefore:             now.Add(-clockSkew),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		URIs:                  []*url.URL{uri},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, issuer, public, issuerKey)
	if err != nil {
		return nil, errorx.Wrap(err, "ca: create certificate", errorx.KindInternal)
	}
	return der, nil
}

// subjectURI renders a GRN as the SAN URI that identifies it.
//
// A GRN is an opaque URI: it has no "//", so url.Parse leaves Path and
// Host as empty strings rather than failing. Whoever reads this back
// compares the whole URI string, or Opaque — an empty Path compares
// equal to any other empty Path, which is every identity at once.
//
// Those two readings agree only while the whole GRN is the opaque part,
// and a '?' or '#' in a segment breaks that quietly: url.Parse moves
// the tail into RawQuery or Fragment, so Opaque holds a truncated
// identity while the URI still renders back to the GRN. The check below
// refuses to issue such a certificate rather than leave a verifier's
// choice of reading deciding who the holder is.
func subjectURI(g grn.GRN) (*url.URL, error) {
	u, err := url.Parse(g.String())
	if err != nil {
		return nil, errorx.Wrap(err, "ca: subject uri", errorx.KindInvalidArgument)
	}
	if u.Scheme+":"+u.Opaque != g.String() {
		return nil, errorx.New(errorx.KindInvalidArgument, "ca: subject grn does not survive as a uri")
	}
	return u, nil
}

// expiry returns when a certificate issued at now stops being valid.
// Jitter only ever shortens the lifetime, so the configured value stays
// the ceiling it is meant to be, and agents provisioned together stop
// expiring — and so restarting — together (ADR-0015).
func expiry(now time.Time, lifetime time.Duration) (time.Time, error) {
	spread := int64(lifetime / jitterFraction)
	if spread <= 0 {
		return now.Add(lifetime).Truncate(time.Second), nil
	}
	jitter, err := rand.Int(rand.Reader, big.NewInt(spread))
	if err != nil {
		return time.Time{}, errorx.Wrap(err, "ca: lifetime jitter", errorx.KindInternal)
	}
	return now.Add(lifetime - time.Duration(jitter.Int64())).Truncate(time.Second), nil
}

// serialNumber draws a serial wide enough that two issuers never
// collide on one.
func serialNumber() (*big.Int, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), serialBits))
	if err != nil {
		return nil, errorx.Wrap(err, "ca: serial number", errorx.KindInternal)
	}
	return serial, nil
}

// encodePEM wraps DER bytes in the PEM block of the given type.
func encodePEM(blockType string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}
