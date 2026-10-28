package org

import (
	"time"

	"github.com/garamsh/garam/platform/internal/grn"
)

// Group is a member container that belongs to a single Organization.
// Every field is one the store writes and reads back, and the aggregate
// holds no invariant over any of them, so they are exported and
// whatever restores a row does it from outside this package (ADR-0069).
type Group struct {
	GRN          grn.GRN
	Name         string
	Organization grn.GRN
	Roles        []string
	Policies     []string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// NewGroup constructs a Group tied to the given Organization GRN. The
// optional roles are the ones the Group grants its members. CreatedAt
// and UpdatedAt are stamped from one instant.
func NewGroup(g grn.GRN, name string, orgGRN grn.GRN, roles ...string) *Group {
	now := time.Now().UTC()
	return &Group{GRN: g, Name: name, Organization: orgGRN, Roles: roles, CreatedAt: now, UpdatedAt: now}
}
