package message

import (
	"time"

	"github.com/garamsh/garam/internal/grn"
)

// Consent is the relation a message crossing an organization boundary
// needs (ADR-0035): one row per pair of organizations, holding each
// side's half as the moment that side last granted. It stands while
// both halves do, and either organization withdraws its own half at any
// time. The pair is held ordered, so the two directions of one relation
// are one Consent.
type Consent struct {
	orgA         grn.GRN
	orgB         grn.GRN
	aConsentedAt *time.Time
	bConsentedAt *time.Time
}

// NewConsent builds a Consent from a stored pair: orgA before orgB,
// each half the stamp its side's last grant wrote, or none where that
// side does not stand. It is exported for the stores outside this
// package that rehydrate the relation, as internal/agent's
// constructors are.
func NewConsent(orgA, orgB grn.GRN, aConsentedAt, bConsentedAt *time.Time) *Consent {
	return &Consent{orgA: orgA, orgB: orgB, aConsentedAt: aConsentedAt, bConsentedAt: bConsentedAt}
}

func (c *Consent) OrgA() grn.GRN { return c.orgA }
func (c *Consent) OrgB() grn.GRN { return c.orgB }

// AConsentedAt is when the orgA side last granted, or nil while that
// half is withdrawn.
func (c *Consent) AConsentedAt() *time.Time { return c.aConsentedAt }

// BConsentedAt is when the orgB side last granted, or nil while that
// half is withdrawn.
func (c *Consent) BConsentedAt() *time.Time { return c.bConsentedAt }

// Stands reports whether both halves stand, which is the whole of what
// the send path asks: a message crosses between the two organizations
// while it does and is refused back to its sender when it does not.
func (c *Consent) Stands() bool { return c.aConsentedAt != nil && c.bConsentedAt != nil }
