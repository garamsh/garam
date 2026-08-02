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
// acceptance, not open signup.
type Invitation struct {
	token      string
	orgGRN     grn.GRN
	role       Role
	createdBy  grn.GRN
	createdAt  time.Time
	expiresAt  time.Time
	acceptedBy grn.GRN
	acceptedAt time.Time
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

// NewInvitation constructs a pending Invitation.
func NewInvitation(token string, orgGRN grn.GRN, role Role, createdBy grn.GRN, expiresAt time.Time) *Invitation {
	return &Invitation{
		token:     token,
		orgGRN:    orgGRN,
		role:      role,
		createdBy: createdBy,
		createdAt: time.Now().UTC(),
		expiresAt: expiresAt.UTC(),
	}
}

func (i *Invitation) Token() string         { return i.token }
func (i *Invitation) Organization() grn.GRN { return i.orgGRN }
func (i *Invitation) Role() Role            { return i.role }
func (i *Invitation) CreatedBy() grn.GRN    { return i.createdBy }
func (i *Invitation) CreatedAt() time.Time  { return i.createdAt }
func (i *Invitation) ExpiresAt() time.Time  { return i.expiresAt }
func (i *Invitation) AcceptedBy() grn.GRN   { return i.acceptedBy }
func (i *Invitation) AcceptedAt() time.Time { return i.acceptedAt }

// Accepted reports whether the invitation has been used.
func (i *Invitation) Accepted() bool { return !i.acceptedAt.IsZero() }

// Expired reports whether the invitation is past its expiry at now.
func (i *Invitation) Expired(now time.Time) bool { return !now.Before(i.expiresAt) }
