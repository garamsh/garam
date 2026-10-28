package message

import (
	"time"

	"github.com/garamsh/garam/lib/grn"
)

// Consent is the relation a message crossing an organization boundary
// needs (ADR-0035): one row per pair of organizations, holding each
// side's half as the moment that side last granted. It stands while
// both halves do, and either organization withdraws its own half at any
// time. The pair is held ordered, so the two directions of one relation
// are one Consent.
//
// It is a data carrier, its fields exported (ADR-0069): orgA sorts
// before orgB, and each half is the stamp its side's last grant wrote.
type Consent struct {
	OrgA grn.GRN
	OrgB grn.GRN
	// AConsentedAt is when the orgA side last granted, or nil while that
	// half is withdrawn.
	AConsentedAt *time.Time
	// BConsentedAt is when the orgB side last granted, or nil while that
	// half is withdrawn.
	BConsentedAt *time.Time
}

// Stands reports whether both halves stand, which is the whole of what
// the send path asks: a message crosses between the two organizations
// while it does and is refused back to its sender when it does not.
func (c *Consent) Stands() bool { return c.AConsentedAt != nil && c.BConsentedAt != nil }
