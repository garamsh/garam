// Package ca owns the certificate authority an organization signs its
// machine identities with (ADR-0014).
//
// Each organization has its own CA, created on the first certificate it
// needs, so a certificate issued by one organization does not chain
// against another's: tenancy holds in the trust root rather than in a
// check a caller could forget. An issued certificate carries the
// subject's GRN in a SAN URI and nothing else — no roles, no claims.
//
// The CA private key never reaches a column in the clear. It is sealed
// through [Sealer] with a key-encryption key the process is given at
// boot, so a database dump alone signs nothing. A subject's own private
// key is generated per certificate and returned once; garam never
// stores it.
package ca

import (
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Domain sentinel errors.
var (
	ErrAuthorityNotFound      = errorx.New(errorx.KindNotFound, "certificate authority not found")
	ErrAuthorityAlreadyExists = errorx.New(errorx.KindAlreadyExists, "certificate authority already exists")
)

// Authority is an organization's certificate authority: the certificate
// it signs with, and the private key behind it in sealed form.
type Authority struct {
	orgGRN      grn.GRN
	certificate []byte
	sealedKey   []byte
	notAfter    time.Time
	createdAt   time.Time
}

// Organization returns the GRN of the organization the CA belongs to.
func (a *Authority) Organization() grn.GRN { return a.orgGRN }

// Certificate returns the CA certificate in DER form.
func (a *Authority) Certificate() []byte { return a.certificate }

// SealedKey returns the CA private key as [Sealer] sealed it. Reading
// it yields no key material.
func (a *Authority) SealedKey() []byte { return a.sealedKey }

// Certificate is an issued client certificate, the private key it was
// issued with, and the CA that signed it. The private key exists in
// this value alone — it is generated per certificate and never stored,
// so a caller that loses it reissues rather than recovers.
type Certificate struct {
	subject   grn.GRN
	certPEM   []byte
	keyPEM    []byte
	issuerPEM []byte
	notAfter  time.Time
}

// Subject returns the GRN the certificate identifies, which is also
// what its SAN URI holds.
func (c *Certificate) Subject() grn.GRN { return c.subject }

// CertificatePEM returns the certificate in PEM form.
func (c *Certificate) CertificatePEM() []byte { return c.certPEM }

// PrivateKeyPEM returns the private key in PKCS#8 PEM form.
func (c *Certificate) PrivateKeyPEM() []byte { return c.keyPEM }

// IssuerPEM returns the signing CA certificate in PEM form: the
// authority this certificate chains to. It is not what garam is
// verified against — the two directions rest on different roots, and
// the one a holder pins garam by travels beside this (ADR-0040).
func (c *Certificate) IssuerPEM() []byte { return c.issuerPEM }

// NotAfter returns when the certificate stops being valid. It carries
// jitter, so certificates issued together do not expire together.
func (c *Certificate) NotAfter() time.Time { return c.notAfter }
