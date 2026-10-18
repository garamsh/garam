package org

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/garamsh/garam/internal/errorx"
	"github.com/garamsh/garam/internal/grn"
)

// Invitation is a one-time bearer token that admits a user into an
// Organization with a fixed Role. Orgs grow by invitation and
// acceptance, not open signup. Every field is one the store writes and
// reads back, and the aggregate holds no invariant over any of them, so
// they are exported and whatever restores a row does it from outside
// this package (ADR-0069).
type Invitation struct {
	Token        string
	Organization grn.GRN
	Role         Role
	CreatedBy    grn.GRN
	CreatedAt    time.Time
	ExpiresAt    time.Time
	AcceptedBy   grn.GRN
	AcceptedAt   time.Time
}

// NewInvitationToken returns a random invitation token. The token is a
// 192-bit secret rendered as hex so it also satisfies the GRN
// identifier alphabet.
func NewInvitationToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errorx.Wrap(err, "invitation token")
	}
	return hex.EncodeToString(b[:]), nil
}

// NewInvitation constructs a pending Invitation, stamping CreatedAt.
func NewInvitation(token string, orgGRN grn.GRN, role Role, createdBy grn.GRN, expiresAt time.Time) *Invitation {
	return &Invitation{
		Token:        token,
		Organization: orgGRN,
		Role:         role,
		CreatedBy:    createdBy,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    expiresAt.UTC(),
	}
}

// Accepted reports whether the invitation has been used.
func (i *Invitation) Accepted() bool { return !i.AcceptedAt.IsZero() }

// Expired reports whether the invitation is past its expiry at now.
func (i *Invitation) Expired(now time.Time) bool { return !now.Before(i.ExpiresAt) }
